package helper

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/services"
	"nas-revival/backend/internal/sshd"
)

// SshService is the result of ssh.get and ssh.set (SSH-01).
type SshService struct {
	Settings sshd.Settings `json:"settings"`
	// Managed: the drop-in is the one rendered from Settings.
	Managed   bool           `json:"managed"`
	Running   bool           `json:"running"`
	Effective sshd.Effective `json:"effective"`
	// Groups are the NAS groups SFTP can be granted to.
	Groups []string `json:"groups"`
	// ChrootOK: sshd accepts ChrootDir (root-owned, not writable by others).
	ChrootOK bool `json:"chrootOk"`
}

// SshManager owns ssh.json and the sshd drop-in and reloads sshd.
type SshManager struct {
	Path     string // ssh.json
	DropIn   string // sshd_config.d/10-nas-revival.conf
	KeysDir  string
	Chroot   string
	Accounts accounts.Paths
	Run      Runner
	mu       sync.Mutex
}

func sshFieldErr(err error) error {
	var fe *sshd.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// SshFieldErr turns a sshd.FieldError into the helper's invalid error.
func SshFieldErr(err error) error { return sshFieldErr(err) }

// ValidateSsh checks SSH settings (the web process runs it too).
func ValidateSsh(s sshd.Settings) (sshd.Settings, error) {
	n, err := sshd.Validate(s)
	return n, sshFieldErr(err)
}

// sftpGroups lists the groups SFTP may be granted to: nas-users (every NAS
// account) and the editable groups, never system groups.
func sftpGroups(db accounts.DB) []string {
	out := []string{}
	for _, g := range db.Groups {
		if g.Name == accounts.UsersGroup || db.IsEditableGroup(g.Name) {
			out = append(out, g.Name)
		}
	}
	slices.Sort(out)
	return out
}

// chrootOK applies sshd's rule to the chroot and every parent: owned by
// root and not writable by group or others.
func chrootOK(dir string) bool {
	for p := dir; ; {
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
			return false
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 {
			return false
		}
		if p == "/" {
			return true
		}
		p = parentDir(p)
	}
}

func parentDir(p string) string {
	i := strings.LastIndexByte(strings.TrimSuffix(p, "/"), '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func (m *SshManager) state(ctx context.Context) (SshService, error) {
	s, err := sshd.Load(m.Path)
	if err != nil {
		slog.Error("helper: read ssh.json", "err", err)
		return SshService{}, internalErr("Nastavení SSH nelze načíst.")
	}
	st := SshService{Settings: s, Groups: []string{}, ChrootOK: chrootOK(m.Chroot)}
	if b, err := os.ReadFile(m.DropIn); err == nil && string(b) == sshd.Render(s) {
		st.Managed = true
	}
	_, err = m.Run(ctx, "systemctl", "is-active", "--quiet", "ssh.service")
	st.Running = err == nil
	if out, err := m.Run(ctx, "sshd", "-T"); err == nil {
		st.Effective = sshd.ParseEffective(out)
	} else {
		slog.Warn("helper: sshd -T failed", "err", err)
		st.Effective = sshd.Effective{Ports: []int{}}
	}
	if db, err := accounts.Load(m.Accounts); err == nil {
		st.Groups = sftpGroups(db)
	}
	return st, nil
}

func (m *SshManager) Get(ctx context.Context) (SshService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state(ctx)
}

// portBusy reports a TCP listener on port that is not sshd's own.
func (m *SshManager) portBusy(ctx context.Context, port int, own []int) bool {
	if slices.Contains(own, port) {
		return false
	}
	raw, err := m.Run(ctx, "ss", "-H", "-l", "-t", "-n")
	if err != nil {
		slog.Warn("helper: ss failed", "err", err)
		return false
	}
	for _, l := range services.ParseSS(raw) {
		if l.Port.Port == port {
			return true
		}
	}
	return false
}

// Set validates, writes the drop-in, checks it with `sshd -t` (restoring the
// old one when sshd rejects it) and reloads sshd. Open sessions stay.
func (m *SshManager) Set(ctx context.Context, in sshd.Settings, actor string) (SshService, error) {
	s, err := ValidateSsh(in)
	if err != nil {
		return SshService{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := accounts.Load(m.Accounts)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return SshService{}, internalErr("Seznam skupin nelze načíst.")
	}
	allowed := sftpGroups(db)
	for _, g := range s.SftpGroups {
		if !slices.Contains(allowed, g) {
			return SshService{}, invalid("sftpGroups", "Skupině "+quoteShort(g)+" nelze povolit SFTP.")
		}
	}
	if len(s.SftpGroups) > 0 && !chrootOK(m.Chroot) {
		return SshService{}, invalid("sftpGroups", "Složka "+m.Chroot+" musí patřit rootovi a nesmí být zapisovatelná pro ostatní; SFTP nelze zapnout.")
	}
	var own []int
	if out, err := m.Run(ctx, "sshd", "-T"); err == nil {
		own = sshd.ParseEffective(out).Ports
	}
	if m.portBusy(ctx, s.Port, own) {
		return SshService{}, invalid("port", "Port "+strconv.Itoa(s.Port)+" už na NASu používá jiná služba.")
	}
	if err := m.apply(ctx, s); err != nil {
		return SshService{}, err
	}
	slog.Info("helper: ssh.set", "port", s.Port, "passwordAuth", s.PasswordAuth, "sftpGroups", s.SftpGroups, "user", actor)
	return m.state(ctx)
}

// apply writes and tests the drop-in, saves ssh.json and reloads sshd. A
// drop-in sshd rejects is replaced by the previous one before returning.
func (m *SshManager) apply(ctx context.Context, s sshd.Settings) error {
	// sshd reads authorized_keys with the user's uid, so the directory must be
	// world-readable despite the helper's UMask=0077.
	if err := mkdirMode(m.KeysDir, 0o755); err != nil {
		slog.Error("helper: create ssh keys dir", "err", err)
		return internalErr("Složku pro klíče SSH nelze vytvořit.")
	}
	old, oldErr := os.ReadFile(m.DropIn)
	revert := func() {
		var err error
		if oldErr == nil {
			err = atomicfile.Write(m.DropIn, old, 0o644)
		} else {
			err = os.Remove(m.DropIn)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("helper: restore sshd drop-in", "err", err)
		}
	}
	if err := atomicfile.Write(m.DropIn, []byte(sshd.Render(s)), 0o644); err != nil {
		slog.Error("helper: write sshd drop-in", "err", err)
		return internalErr("Konfiguraci SSH nelze zapsat.")
	}
	if out, err := m.Run(ctx, "sshd", "-t"); err != nil {
		slog.Error("helper: sshd -t rejected the drop-in", "err", err, "out", strings.TrimSpace(string(out)))
		revert()
		return internalErr("Konfigurace SSH je neplatná; nic se nezměnilo.")
	}
	if err := sshd.Save(m.Path, s); err != nil {
		slog.Error("helper: write ssh.json", "err", err)
		revert()
		return internalErr("Nastavení SSH nelze uložit.")
	}
	if out, err := m.Run(ctx, "systemctl", "reload", "ssh.service"); err != nil {
		slog.Error("helper: reload ssh", "err", err, "out", strings.TrimSpace(string(out)))
		return internalErr("Nastavení je uložené, ale SSH se nepodařilo znovu načíst.")
	}
	return nil
}

// Restore writes an older ssh.json back (config rollback) and applies it; a
// version without ssh.json means the Debian defaults.
func (m *SshManager) Restore(ctx context.Context, data []byte, present bool) error {
	s := sshd.Default()
	if present {
		p, err := sshd.Parse(data)
		if err != nil {
			return invalid("", "Uložená verze nastavení SSH není platná.")
		}
		s = p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(s.SftpGroups) > 0 && !chrootOK(m.Chroot) {
		return invalid("", "Složka "+m.Chroot+" nesplňuje podmínky pro SFTP; verzi nastavení SSH nelze obnovit.")
	}
	if err := m.apply(ctx, s); err != nil {
		return err
	}
	if !present {
		if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return internalErr("Nastavení SSH nelze odstranit.")
		}
	}
	return nil
}

func mkdirMode(dir string, mode os.FileMode) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return err
	}
	return os.Chmod(dir, mode)
}
