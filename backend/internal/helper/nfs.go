package helper

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
)

const nfsUnit = "nfs-server.service"

// NfsShare is a shared folder the NFS tab can export.
type NfsShare struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
	// GroupWrite: nas-users has read/write. Squashed NFS clients write with
	// that group, so without it every NFS write is refused (ACL volumes).
	GroupWrite bool `json:"groupWrite"`
}

func groupWrite(s shares.Share) bool {
	for _, p := range s.Permissions {
		if p.Kind == shares.KindGroup && p.Name == accounts.UsersGroup && p.Access == shares.AccessRW {
			return true
		}
	}
	return false
}

// NfsService is the result of nfs.get and nfs.set (NFS-01).
type NfsService struct {
	Installed bool         `json:"installed"`
	Settings  nfs.Settings `json:"settings"`
	// Running is what systemd reports for nfs-server.
	Running bool `json:"running"`
	// Managed: the exports file is the one rendered from Settings.
	Managed bool `json:"managed"`
	// Missing lists exported shares that were deleted meanwhile.
	Missing []string   `json:"missing"`
	Shares  []NfsShare `json:"shares"`
}

// NfsStatus is the result of nfs.status: NFSv4 clients with a lease.
type NfsStatus struct {
	Clients []nfs.Connected `json:"clients"`
}

// NfsManager owns nfs.json and the exports file and switches nfs-server.
type NfsManager struct {
	Path       string // nfs.json
	Exports    string // exports file
	Shares     string // shares.json, only read
	Accounts   accounts.Paths
	ClientsDir string // /proc/fs/nfsd/clients
	// Filesystems is /proc/filesystems: nfsd appears there once the kernel has it loaded.
	Filesystems string
	Run         Runner
	LookPath    func(string) (string, error)
	mu          sync.Mutex
}

func nfsFieldErr(err error) error {
	var fe *nfs.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// ValidateNfs checks NFS settings (the web process runs it too).
func ValidateNfs(s nfs.Settings) (nfs.Settings, error) {
	n, err := nfs.Validate(s)
	return n, nfsFieldErr(err)
}

func (m *NfsManager) installed() bool {
	_, err := m.LookPath("exportfs")
	return err == nil
}

func (m *NfsManager) running(ctx context.Context) bool {
	_, err := m.Run(ctx, "systemctl", "is-active", "--quiet", nfsUnit)
	return err == nil
}

func (m *NfsManager) loadShares() (shares.Config, error) {
	c, err := shares.Load(m.Shares)
	if err != nil {
		slog.Error("helper: read shares.json", "err", err)
		return c, internalErr("Konfiguraci sdílených složek nelze načíst.")
	}
	return c, nil
}

// anonGID is the group squashed clients write with: nas-users, which every
// share folder grants read/write.
func (m *NfsManager) anonGID() (int, error) {
	db, err := accounts.Load(m.Accounts)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return 0, internalErr("Nelze načíst uživatelské účty.")
	}
	g, ok := db.Group(accounts.UsersGroup)
	if !ok {
		return 0, internalErr("Na NASu chybí skupina " + accounts.UsersGroup + ".")
	}
	return g.GID, nil
}

func (m *NfsManager) render(s nfs.Settings, cfg shares.Config) (string, []string, error) {
	gid, err := m.anonGID()
	if err != nil {
		return "", nil, err
	}
	ok, missing := nfs.Resolve(s, cfg)
	return nfs.Render(s.Enabled, ok, gid), missing, nil
}

func (m *NfsManager) state(ctx context.Context) (NfsService, error) {
	s, err := nfs.Load(m.Path)
	if err != nil {
		slog.Error("helper: read nfs.json", "err", err)
		return NfsService{}, internalErr("Nastavení NFS nelze načíst.")
	}
	cfg, err := m.loadShares()
	if err != nil {
		return NfsService{}, err
	}
	st := NfsService{Installed: m.installed(), Settings: s, Missing: []string{}, Shares: []NfsShare{}}
	for _, sh := range cfg.Shares {
		st.Shares = append(st.Shares, NfsShare{Name: sh.Name, Path: sh.Path(), ReadOnly: sh.ReadOnly, GroupWrite: groupWrite(sh)})
	}
	text, missing, err := m.render(s, cfg)
	if err != nil {
		return NfsService{}, err
	}
	if missing != nil {
		st.Missing = missing
	}
	if !st.Installed {
		return st, nil
	}
	st.Running = m.running(ctx)
	if b, err := os.ReadFile(m.Exports); err == nil && string(b) == text {
		st.Managed = true
	}
	return st, nil
}

func (m *NfsManager) Get(ctx context.Context) (NfsService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state(ctx)
}

// Set validates, writes nfs.json and the exports file and switches
// nfs-server. If the server refuses the exports, the previous files and
// state come back and nothing changes.
func (m *NfsManager) Set(ctx context.Context, in nfs.Settings, actor string) (NfsService, error) {
	s, err := ValidateNfs(in)
	if err != nil {
		return NfsService{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.installed() {
		return NfsService{}, invalid("", "NFS server není na NASu nainstalovaný.")
	}
	cfg, err := m.loadShares()
	if err != nil {
		return NfsService{}, err
	}
	text, missing, err := m.render(s, cfg)
	if err != nil {
		return NfsService{}, err
	}
	if len(missing) > 0 {
		return NfsService{}, invalid("exports", "Sdílená složka "+missing[0]+" neexistuje.")
	}
	oldJSON, oldJSONErr := os.ReadFile(m.Path)
	oldExports, oldExportsErr := os.ReadFile(m.Exports)
	wasRunning := m.running(ctx)
	if err := m.write(s, text); err != nil {
		return NfsService{}, err
	}
	if err := m.apply(ctx, s.Enabled); err != nil {
		m.restoreFile(m.Path, oldJSON, oldJSONErr)
		m.restoreFile(m.Exports, oldExports, oldExportsErr)
		if wasRunning {
			m.apply(ctx, true)
		} else {
			m.systemctl(ctx, "disable", "--now", nfsUnit)
		}
		return NfsService{}, err
	}
	slog.Info("helper: nfs.set", "enabled", s.Enabled, "exports", len(s.Exports), "user", actor)
	return m.state(ctx)
}

func (m *NfsManager) write(s nfs.Settings, text string) error {
	if err := nfs.Save(m.Path, s); err != nil {
		slog.Error("helper: write nfs.json", "err", err)
		return internalErr("Nastavení NFS nelze uložit.")
	}
	return m.writeExports(text)
}

func (m *NfsManager) writeExports(text string) error {
	if err := os.MkdirAll(dirOf(m.Exports), 0o755); err != nil {
		return internalErr("Exporty NFS nelze zapsat.")
	}
	if err := atomicfile.Write(m.Exports, []byte(text), 0o644); err != nil {
		slog.Error("helper: write exports", "err", err)
		return internalErr("Exporty NFS nelze zapsat.")
	}
	return nil
}

// restoreFile puts back what ReadFile returned before a change.
func (m *NfsManager) restoreFile(p string, b []byte, readErr error) {
	var err error
	if errors.Is(readErr, fs.ErrNotExist) {
		err = os.Remove(p)
	} else if readErr == nil {
		err = atomicfile.Write(p, b, 0o644)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Error("helper: nfs restore", "file", p, "err", err)
	}
}

// apply starts or stops nfs-server and makes the kernel use the exports file.
func (m *NfsManager) apply(ctx context.Context, enabled bool) error {
	if !enabled {
		if err := m.systemctl(ctx, "disable", "--now", nfsUnit); err != nil {
			return internalErr("NFS server se nepodařilo vypnout; nic se nezměnilo.")
		}
		return nil
	}
	if err := m.systemctl(ctx, "enable", "--now", nfsUnit); err != nil {
		if !m.kernelHasNfsd() {
			return internalErr("Jádro NASu neumí NFS server (chybí NFSD); je potřeba nové jádro. Nic se nezměnilo.")
		}
		return internalErr("NFS server se nepodařilo spustit; nic se nezměnilo.")
	}
	// -r syncs the kernel with the files: new entries appear, removed ones go.
	if out, err := m.Run(ctx, "exportfs", "-ra"); err != nil {
		slog.Error("helper: exportfs -ra", "err", err, "out", strings.TrimSpace(string(out)))
		return internalErr("NFS server exporty odmítl; nic se nezměnilo.")
	}
	return nil
}

func (m *NfsManager) kernelHasNfsd() bool {
	b, err := os.ReadFile(m.Filesystems)
	return err == nil && bytes.Contains(b, []byte("\tnfsd\n"))
}

func (m *NfsManager) systemctl(ctx context.Context, args ...string) error {
	out, err := m.Run(ctx, "systemctl", args...)
	if err != nil {
		slog.Error("helper: systemctl", "args", args, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return err
}

// Status lists the connected NFSv4 clients (admins only in the API).
func (m *NfsManager) Status(ctx context.Context) (NfsStatus, error) {
	if !m.installed() || !m.running(ctx) {
		return NfsStatus{Clients: []nfs.Connected{}}, nil
	}
	l, err := nfs.ReadClients(m.ClientsDir)
	if err != nil {
		slog.Error("helper: nfsd clients", "err", err)
		return NfsStatus{Clients: []nfs.Connected{}}, internalErr("Připojené klienty NFS nelze zjistit.")
	}
	return NfsStatus{Clients: l}, nil
}

// Exported reports whether a share is listed in nfs.json; such a share
// cannot be deleted until its export is removed.
func (m *NfsManager) Exported(name string) bool {
	s, err := nfs.Load(m.Path)
	if err != nil {
		return false
	}
	for _, e := range s.Exports {
		if e.Share == name {
			return true
		}
	}
	return false
}

// Refresh re-renders the exports after a share changed (read-only flag,
// config rollback) and re-exports when the server runs. It never fails the
// share change; problems are logged.
func (m *NfsManager) Refresh(ctx context.Context, cfg shares.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !fileExists(m.Path) {
		return
	}
	s, err := nfs.Load(m.Path)
	if err != nil {
		slog.Error("helper: nfs refresh", "err", err)
		return
	}
	text, missing, err := m.render(s, cfg)
	if err != nil {
		return
	}
	if len(missing) > 0 {
		slog.Warn("helper: nfs exports of missing shares skipped", "shares", missing)
	}
	if b, err := os.ReadFile(m.Exports); err == nil && string(b) == text {
		return
	}
	if err := m.writeExports(text); err != nil {
		return
	}
	if m.installed() && m.running(ctx) {
		if out, err := m.Run(ctx, "exportfs", "-ra"); err != nil {
			slog.Error("helper: nfs refresh exportfs", "err", err, "out", strings.TrimSpace(string(out)))
		}
	}
}

// Restore writes an older nfs.json back (config rollback) and applies it; a
// version without nfs.json means NFS off. Exports of shares that no longer
// exist are skipped.
func (m *NfsManager) Restore(ctx context.Context, data []byte, present bool) error {
	s := nfs.Default()
	if present {
		p, err := nfs.Parse(data)
		if err != nil {
			return invalid("", "Uložená verze nastavení NFS není platná.")
		}
		s = p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.loadShares()
	if err != nil {
		return err
	}
	text, _, err := m.render(s, cfg)
	if err != nil {
		return err
	}
	if present {
		if err := m.write(s, text); err != nil {
			return err
		}
	} else {
		if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return internalErr("Nastavení NFS nelze odstranit.")
		}
		if err := m.writeExports(text); err != nil {
			return err
		}
	}
	if !m.installed() {
		return nil
	}
	return m.apply(ctx, s.Enabled)
}
