package helper

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/sshd"
)

// SshKeyUser is one NAS account with its public keys (SSH-02).
type SshKeyUser struct {
	Name string     `json:"name"`
	Keys []sshd.Key `json:"keys"`
	// Sftp: the account is in a group that may log in with SFTP, so its keys
	// are used. Keys of other accounts are kept but sshd refuses the login.
	Sftp bool `json:"sftp"`
	// Invalid counts lines of the keys file that are not a valid key (edited
	// by hand); the next change of the user's keys drops them.
	Invalid int `json:"invalid,omitempty"`
}

// SshKeys is the result of the ssh.keys.* operations.
type SshKeys struct {
	Users []SshKeyUser `json:"users"`
}

// keysPath is the authorized_keys file of a NAS user; name is a valid
// account name, so it cannot leave the directory.
func (m *SshManager) keysPath(name string) string { return filepath.Join(m.KeysDir, name) }

func (m *SshManager) readKeys(name string) ([]sshd.Key, []string, int, error) {
	b, err := os.ReadFile(m.keysPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return []sshd.Key{}, nil, 0, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	keys, lines, invalid := sshd.ParseKeysFile(b)
	if keys == nil {
		keys = []sshd.Key{}
	}
	return keys, lines, invalid, nil
}

// inSftpGroup: nas-users is every NAS account's primary group, the others
// list their members.
func inSftpGroup(db accounts.DB, sftpGroups []string, name string) bool {
	for _, g := range sftpGroups {
		if g == accounts.UsersGroup {
			return true
		}
		if e, ok := db.Group(g); ok && slices.Contains(e.Members, name) {
			return true
		}
	}
	return false
}

func (m *SshManager) keysState() (SshKeys, error) {
	db, err := accounts.Load(m.Accounts)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return SshKeys{}, internalErr("Seznam uživatelů nelze načíst.")
	}
	s, err := sshd.Load(m.Path)
	if err != nil {
		slog.Error("helper: read ssh.json", "err", err)
		return SshKeys{}, internalErr("Nastavení SSH nelze načíst.")
	}
	out := SshKeys{Users: []SshKeyUser{}}
	for _, u := range db.NASUsers() {
		keys, _, invalid, err := m.readKeys(u.Name)
		if err != nil {
			slog.Error("helper: read ssh keys", "account", u.Name, "err", err)
			return SshKeys{}, internalErr("Klíče SSH nelze načíst.")
		}
		out.Users = append(out.Users, SshKeyUser{Name: u.Name, Keys: keys, Sftp: inSftpGroup(db, s.SftpGroups, u.Name), Invalid: invalid})
	}
	return out, nil
}

func (m *SshManager) KeysList(context.Context) (SshKeys, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keysState()
}

// nasUserKeys loads the keys of an existing NAS account (never root or a
// system account: their keys are managed outside the web UI).
func (m *SshManager) nasUserKeys(name string) ([]sshd.Key, []string, error) {
	db, err := accounts.Load(m.Accounts)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return nil, nil, internalErr("Seznam uživatelů nelze načíst.")
	}
	if !ValidName(name) || !db.IsNASUser(name) {
		return nil, nil, notFound("Uživatel NASu " + quoteShort(name) + " neexistuje.")
	}
	keys, lines, _, err := m.readKeys(name)
	if err != nil {
		slog.Error("helper: read ssh keys", "account", name, "err", err)
		return nil, nil, internalErr("Klíče SSH nelze načíst.")
	}
	return keys, lines, nil
}

// writeKeys replaces the user's keys file: root-owned and 0644, because
// sshd opens it with the user's uid (StrictModes accepts root as owner).
func (m *SshManager) writeKeys(name string, lines []string) error {
	if err := mkdirMode(m.KeysDir, 0o755); err != nil {
		slog.Error("helper: create ssh keys dir", "err", err)
		return internalErr("Složku pro klíče SSH nelze vytvořit.")
	}
	var err error
	if len(lines) == 0 {
		err = os.Remove(m.keysPath(name))
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	} else {
		err = atomicfile.Write(m.keysPath(name), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	if err != nil {
		slog.Error("helper: write ssh keys", "account", name, "err", err)
		return internalErr("Klíče SSH nelze uložit.")
	}
	return nil
}

// KeyAdd adds one public key to a NAS user. sshd reads the file at every
// login, so no reload is needed.
func (m *SshManager) KeyAdd(_ context.Context, name, text, actor string) (SshKeys, error) {
	k, line, err := sshd.ParseKey(text)
	if err != nil {
		return SshKeys{}, sshFieldErr(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, lines, err := m.nasUserKeys(name)
	if err != nil {
		return SshKeys{}, err
	}
	if slices.ContainsFunc(keys, func(x sshd.Key) bool { return x.Fingerprint == k.Fingerprint }) {
		return SshKeys{}, invalid("key", "Tento klíč už uživatel má.")
	}
	if len(keys) >= sshd.MaxKeysPerUser {
		return SshKeys{}, invalid("key", "Uživatel může mít nejvýše 10 klíčů.")
	}
	if err := m.writeKeys(name, append(lines, line)); err != nil {
		return SshKeys{}, err
	}
	slog.Info("helper: ssh.keys.add", "account", name, "type", k.Type, "fingerprint", k.Fingerprint, "user", actor)
	return m.keysState()
}

// KeyRemove removes the key with the given SHA256 fingerprint.
func (m *SshManager) KeyRemove(_ context.Context, name, fingerprint, actor string) (SshKeys, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, lines, err := m.nasUserKeys(name)
	if err != nil {
		return SshKeys{}, err
	}
	i := slices.IndexFunc(keys, func(x sshd.Key) bool { return x.Fingerprint == fingerprint })
	if i < 0 {
		return SshKeys{}, notFound("Klíč neexistuje.")
	}
	if err := m.writeKeys(name, slices.Delete(lines, i, i+1)); err != nil {
		return SshKeys{}, err
	}
	slog.Info("helper: ssh.keys.remove", "account", name, "fingerprint", fingerprint, "user", actor)
	return m.keysState()
}

// RemoveUserKeys deletes the keys file of an account that was deleted or is
// being created, so a new account never inherits the keys of an old one with
// the same name.
func RemoveUserKeys(dir, name string) error {
	if dir == "" || !ValidName(name) {
		return nil
	}
	err := os.Remove(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
