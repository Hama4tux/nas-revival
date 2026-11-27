package helper

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/tasks"
)

// BackupConfigPath is backup.json (root, 0644), {"version":1}. It only
// carries the version since the pseudo share "zalohy" was dropped; an older
// file with "smbShare" still loads and the field is ignored.
const (
	BackupConfigPath = "/etc/nas-revival/backup.json"
	defaultRemoteDir = "/var/backups"
	shellCheckMarker = "nr-shell-check"
)

// BackupConfig is backup.json.
type BackupConfig struct {
	Version int `json:"version"`
}

// BackupKey is the result of backup.key.get.
type BackupKey struct {
	PublicKey          string `json:"publicKey"`
	Fingerprint        string `json:"fingerprint"`
	AuthorizedKeysHint string `json:"authorizedKeysHint"`
}

// HostKey is one scanned server key.
type HostKey struct {
	Type        string `json:"type"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

// HostScan is the result of backup.host.scan.
type HostScan struct {
	Keys    []HostKey `json:"keys"`
	Known   bool      `json:"known"`
	Changed bool      `json:"changed"`
}

// BackupTestResult is the result of backup.test.
type BackupTestResult struct {
	OK         bool     `json:"ok"`
	Entries    []string `json:"entries"`
	Message    string   `json:"message"`
	FullAccess bool     `json:"fullAccess"`
}

// LoadBackupConfig reads backup.json; missing means everything off.
func LoadBackupConfig(path string) (BackupConfig, error) {
	c := BackupConfig{Version: 1}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	// Unknown fields (the old smbShare) are ignored.
	if err := json.Unmarshal(b, &c); err != nil || c.Version != 1 {
		return BackupConfig{Version: 1}, errors.New("invalid backup.json")
	}
	return c, nil
}

// BackupManager does the root-side parts of the backup pull jobs.
type BackupManager struct {
	Run        Runner
	Iface      string
	KeyPath    string
	KnownHosts string
	ConfigPath string
	// Volume is where the data volume is mounted (default tasks.BackupVolume).
	Volume string
	// TasksPath and SharesPath resolve a task's job directory (defaults: the
	// production files); JobDir replaces that in tests.
	TasksPath, SharesPath string
	JobDir                func(id string) (dir string, ok bool, err error)
	// Lookup resolves the nas-backup account.
	Lookup   func(name string) (uid uint32, gid int, err error)
	Chown    func(path string, uid, gid int) error
	Hostname func() (string, error)
	TempDir  string
	mu       sync.Mutex
}

// NewBackupManager returns a manager with the production paths.
func NewBackupManager(iface string, run Runner) *BackupManager {
	return &BackupManager{Run: run, Iface: iface, KeyPath: tasks.BackupKeyPath, KnownHosts: tasks.BackupKnownHosts,
		ConfigPath: BackupConfigPath, Lookup: LookupUser, Chown: os.Lchown, Hostname: os.Hostname}
}

func (m *BackupManager) account() (uint32, int, error) {
	uid, gid, err := m.Lookup(tasks.BackupUser)
	if err != nil {
		return 0, 0, &Error{Code: CodeNotInstalled, Message: "Zálohování není nainstalované (chybí účet nas-backup)."}
	}
	return uid, gid, nil
}

func safeHostname(h string) string {
	var b strings.Builder
	for _, r := range h {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 || b.Len() > 63 {
		return "nas"
	}
	return b.String()
}

// fingerprint runs ssh-keygen -l on a key file.
func (m *BackupManager) fingerprint(ctx context.Context, file string) (string, error) {
	out, err := m.Run(ctx, "ssh-keygen", "-l", "-E", "sha256", "-f", file)
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || !strings.HasPrefix(f[1], "SHA256:") {
		return "", errors.New("unexpected ssh-keygen output")
	}
	return f[1], nil
}

func (m *BackupManager) nasIPv4(ctx context.Context) string {
	out, err := m.Run(ctx, "ip", "-j", "addr", "show", "dev", m.Iface)
	if err != nil {
		return ""
	}
	for _, a := range parseIPAddr(out) {
		if ip, _, ok := strings.Cut(a, "/"); ok && !strings.Contains(ip, ":") {
			return ip
		}
	}
	return ""
}

// KeyGet returns the public key of the NAS, generating the key pair first
// if it does not exist.
func (m *BackupManager) KeyGet(ctx context.Context) (BackupKey, error) {
	uid, gid, err := m.account()
	if err != nil {
		return BackupKey{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dir := filepath.Dir(m.KeyPath)
	fail := func(what string, err error) (BackupKey, error) {
		slog.Error("helper: backup key", "step", what, "err", err)
		return BackupKey{}, internalErr("Klíč NASu se nepodařilo připravit.")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("mkdir", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fail("chmod dir", err)
	}
	if err := m.Chown(dir, int(uid), gid); err != nil {
		return fail("chown dir", err)
	}
	if _, err := os.Stat(m.KeyPath); errors.Is(err, fs.ErrNotExist) {
		h := "nas"
		if m.Hostname != nil {
			if n, err := m.Hostname(); err == nil {
				h = safeHostname(n)
			}
		}
		if _, err := m.Run(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", tasks.BackupUser+"@"+h, "-f", m.KeyPath); err != nil {
			return fail("ssh-keygen", err)
		}
	} else if err != nil {
		return fail("stat", err)
	}
	pub := m.KeyPath + ".pub"
	for p, mode := range map[string]os.FileMode{m.KeyPath: 0o600, pub: 0o644} {
		if err := os.Chmod(p, mode); err != nil {
			return fail("chmod", err)
		}
		if err := m.Chown(p, int(uid), gid); err != nil {
			return fail("chown", err)
		}
	}
	raw, err := os.ReadFile(pub)
	if err != nil {
		return fail("read pub", err)
	}
	line := strings.TrimSpace(string(raw))
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "ssh-ed25519" || strings.ContainsAny(line, "\"\n\r") {
		return fail("pub format", errors.New("unexpected public key"))
	}
	fp, err := m.fingerprint(ctx, pub)
	if err != nil {
		return fail("fingerprint", err)
	}
	opts := "restrict,command=\"rrsync -ro " + defaultRemoteDir + "\""
	if ip := m.nasIPv4(ctx); ip != "" {
		opts = "from=\"" + ip + "\"," + opts
	}
	return BackupKey{PublicKey: line, Fingerprint: fp, AuthorizedKeysHint: opts + " " + line}, nil
}

func checkTarget(host string, port int) error {
	if !tasks.ValidHost(host) {
		return invalid("host", "Neplatný název nebo adresa serveru.")
	}
	if port == 0 {
		port = tasks.DefaultSSHPort
	}
	if !tasks.ValidPort(port) {
		return invalid("port", "Port musí být 1 až 65535.")
	}
	return nil
}

func hostSpec(host string, port int) string {
	host = strings.ToLower(host)
	if port == tasks.DefaultSSHPort {
		return host
	}
	return "[" + host + "]:" + strconv.Itoa(port)
}

type knownEntry struct{ typ, key string }

func (m *BackupManager) storedKeys(spec string) []knownEntry {
	f, err := os.Open(m.KnownHosts)
	if err != nil {
		return nil
	}
	defer f.Close()
	var res []knownEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 3 || strings.HasPrefix(fl[0], "#") {
			continue
		}
		for _, h := range strings.Split(fl[0], ",") {
			if h == spec {
				res = append(res, knownEntry{fl[1], fl[2]})
			}
		}
	}
	return res
}

// HostScan fetches the host keys the server offers.
func (m *BackupManager) HostScan(ctx context.Context, host string, port int) (HostScan, error) {
	if err := checkTarget(host, port); err != nil {
		return HostScan{}, err
	}
	if port == 0 {
		port = tasks.DefaultSSHPort
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	out, err := m.Run(ctx, "ssh-keyscan", "-T", "5", "-p", strconv.Itoa(port), "-t", "ed25519,ecdsa,rsa", "--", host)
	res := HostScan{Keys: []HostKey{}}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || strings.HasPrefix(f[0], "#") || !validKeyType(f[1]) || seen[f[1]] || !validKeyBlob(f[1], f[2]) {
			continue
		}
		seen[f[1]] = true
		fp, ferr := m.keyFingerprint(ctx, f[1], f[2])
		if ferr != nil {
			continue
		}
		res.Keys = append(res.Keys, HostKey{Type: f[1], Key: f[2], Fingerprint: fp})
	}
	if len(res.Keys) == 0 {
		if err != nil {
			slog.Warn("helper: ssh-keyscan", "err", err)
		}
		return HostScan{}, invalid("host", "Server neodpověděl nebo nenabízí podporovaný klíč.")
	}
	stored := m.storedKeys(hostSpec(host, port))
	for _, k := range res.Keys {
		for _, s := range stored {
			if s.typ == k.Type {
				if s.key == k.Key {
					res.Known = true
				} else {
					res.Changed = true
				}
			}
		}
	}
	return res, nil
}

func (m *BackupManager) keyFingerprint(ctx context.Context, typ, key string) (string, error) {
	f, err := os.CreateTemp(m.TempDir, "nr-hostkey-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, werr := f.WriteString(typ + " " + key + "\n")
	f.Close()
	if werr != nil {
		return "", werr
	}
	return m.fingerprint(ctx, f.Name())
}

func validKeyType(t string) bool {
	return t == "ssh-ed25519" || t == "ecdsa-sha2-nistp256" || t == "ssh-rsa"
}

// validKeyBlob checks base64, size and that the blob starts with its type.
func validKeyBlob(typ, key string) bool {
	if len(key) == 0 || len(key) > 1024 {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(b) < 4 {
		return false
	}
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	return n == len(typ) && len(b) >= 4+n && string(b[4:4+n]) == typ
}

// HostTrust stores the key for host:port, replacing the old entries of it.
func (m *BackupManager) HostTrust(ctx context.Context, host string, port int, typ, key string) error {
	if err := checkTarget(host, port); err != nil {
		return err
	}
	if port == 0 {
		port = tasks.DefaultSSHPort
	}
	if !validKeyType(typ) {
		return invalid("type", "Nepodporovaný typ klíče.")
	}
	if !validKeyBlob(typ, key) {
		return invalid("key", "Neplatný klíč serveru.")
	}
	spec := hostSpec(host, port)
	m.mu.Lock()
	defer m.mu.Unlock()
	var keep []string
	if b, err := os.ReadFile(m.KnownHosts); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if line == "" {
				continue
			}
			f := strings.Fields(line)
			drop := false
			if len(f) >= 3 && !strings.HasPrefix(f[0], "#") {
				for _, h := range strings.Split(f[0], ",") {
					drop = drop || h == spec
				}
			}
			if !drop {
				keep = append(keep, line)
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Error("helper: read known hosts", "err", err)
		return internalErr("Seznam důvěryhodných serverů nelze načíst.")
	}
	keep = append(keep, spec+" "+typ+" "+key)
	if err := atomicfile.Write(m.KnownHosts, []byte(strings.Join(keep, "\n")+"\n"), 0o644); err != nil {
		slog.Error("helper: write known hosts", "err", err)
		return internalErr("Seznam důvěryhodných serverů nelze uložit.")
	}
	slog.Info("helper: backup.host.trust", "host", spec, "type", typ)
	return nil
}

// Test lists the path as nas-backup and checks whether the key can run a
// shell command on the server.
func (m *BackupManager) Test(ctx context.Context, host string, port int, user, path string) (BackupTestResult, error) {
	if err := checkTarget(host, port); err != nil {
		return BackupTestResult{}, err
	}
	if port == 0 {
		port = tasks.DefaultSSHPort
	}
	if !tasks.ValidRemoteUser(user) {
		return BackupTestResult{}, invalid("user", "Neplatné jméno uživatele na serveru.")
	}
	if !tasks.ValidRelPath(path) {
		return BackupTestResult{}, invalid("path", "Neplatná cesta ke složce.")
	}
	if _, _, err := m.account(); err != nil {
		return BackupTestResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	res := BackupTestResult{Entries: []string{}}
	out, err := m.Run(ctx, "runuser", "-u", tasks.BackupUser, "--", "rsync", "--list-only", "--timeout=20",
		"-e", tasks.SSHCommand(m.KeyPath, port, m.KnownHosts), "--", tasks.RemoteSpec(user, host, path))
	if err != nil {
		res.Message = "Připojení se nezdařilo."
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			se := string(ee.Stderr)
			switch {
			case strings.Contains(se, "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(se, "Host key verification failed"):
				res.Message = tasks.MsgHostKey
			case strings.Contains(se, "Permission denied"):
				res.Message = "Server klíč NASu nepřijal."
			}
		}
		return res, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || strings.Join(f[4:], " ") == "." {
			continue
		}
		if len(res.Entries) < 20 {
			res.Entries = append(res.Entries, strings.Join(f[4:], " "))
		}
	}
	res.OK, res.Message = true, "Připojení funguje."
	args := append([]string{"-u", tasks.BackupUser, "--", "ssh"}, tasks.SSHOptions(m.KeyPath, port, m.KnownHosts)...)
	args = append(args, user+"@"+host, "--", "echo", shellCheckMarker)
	if so, err := m.Run(ctx, "runuser", args...); err == nil && strings.TrimSpace(string(so)) == shellCheckMarker {
		res.FullAccess = true
	}
	return res, nil
}

// handleBackup runs the backup operations (W4.3).
func (d *Dispatcher) handleBackup(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		User    string `json:"user"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		Key     string `json:"key"`
		Enabled *bool  `json:"enabled"`
		ID      string `json:"id"`
		Date    string `json:"date"`
		Offset  int    `json:"offset"`
		Share   string `json:"share"`
		Dir     string `json:"dir"`
		Name    string `json:"name"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "backup.key.get":
		return d.Backup.KeyGet(ctx)
	case "backup.host.scan":
		return d.Backup.HostScan(ctx, a.Host, a.Port)
	case "backup.host.trust":
		return struct{}{}, d.Backup.HostTrust(ctx, a.Host, a.Port, a.Type, a.Key)
	case "backup.test":
		return d.Backup.Test(ctx, a.Host, a.Port, a.User, a.Path)
	case "backup.versions":
		return d.Backup.Versions(ctx, a.ID)
	case "backup.browse":
		return d.Backup.Browse(ctx, a.ID, a.Date, a.Path, a.Offset)
	case "backup.open":
		return d.Backup.Open(ctx, a.ID, a.Date, a.Path)
	}
	if d.Restore != nil {
		switch req.Op {
		case "backup.restore.start":
			return d.Restore.Start(ctx, RestoreRequest{ID: a.ID, Date: a.Date, Path: a.Path, Share: a.Share, Dir: a.Dir, Name: a.Name}, user)
		case "backup.restore.status":
			return d.Restore.Status(), nil
		case "backup.restore.cancel":
			return d.Restore.Cancel(), nil
		}
	}
	return nil, invalid("", "Neznámá operace.")
}
