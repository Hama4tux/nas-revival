package helper

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smb"
)

// SmbService is the result of smb.get and smb.set (SMB-01).
type SmbService struct {
	Installed bool         `json:"installed"`
	Settings  smb.Settings `json:"settings"`
	// Running and NetbiosRunning are what systemd reports, which can differ
	// from Settings when someone used the Services app or the console.
	Running        bool `json:"running"`
	NetbiosRunning bool `json:"netbiosRunning"`
	// Managed: smb.conf is the one rendered from Settings.
	Managed bool `json:"managed"`
}

// SmbManager owns smb.json and smb.conf and switches smbd/nmbd.
type SmbManager struct {
	Path     string // smb.json
	Conf     string // smb.conf
	Fragment string // shares include file
	Run      Runner
	LookPath func(string) (string, error)
	// SharePaths maps share folders to names for the open file list.
	SharePaths func() map[string]string
	mu         sync.Mutex
}

func smbFieldErr(err error) error {
	var fe *smb.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// ValidateSmb checks SMB settings (the web process runs it too).
func ValidateSmb(s smb.Settings) (smb.Settings, error) {
	n, err := smb.Validate(s)
	return n, smbFieldErr(err)
}

func (m *SmbManager) installed() bool {
	_, err := m.LookPath("smbd")
	return err == nil
}

func (m *SmbManager) active(ctx context.Context, unit string) bool {
	_, err := m.Run(ctx, "systemctl", "is-active", "--quiet", unit)
	return err == nil
}

func (m *SmbManager) state(ctx context.Context) (SmbService, error) {
	s, err := smb.Load(m.Path)
	if err != nil {
		slog.Error("helper: read smb.json", "err", err)
		return SmbService{}, internalErr("Nastavení SMB nelze načíst.")
	}
	st := SmbService{Installed: m.installed(), Settings: s}
	if !st.Installed {
		return st, nil
	}
	st.Running = m.active(ctx, "smbd.service")
	st.NetbiosRunning = m.active(ctx, "nmbd.service")
	if b, err := os.ReadFile(m.Conf); err == nil && string(b) == smb.Render(s) {
		st.Managed = true
	}
	return st, nil
}

func (m *SmbManager) Get(ctx context.Context) (SmbService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state(ctx)
}

// Set validates, checks the rendered smb.conf with testparm, saves both files
// and switches the daemons. A rejected config changes nothing.
func (m *SmbManager) Set(ctx context.Context, in smb.Settings, actor string) (SmbService, error) {
	s, err := ValidateSmb(in)
	if err != nil {
		return SmbService{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.installed() {
		return SmbService{}, invalid("", "Samba není na NASu nainstalovaná.")
	}
	old, err := smb.Load(m.Path)
	if err != nil {
		return SmbService{}, internalErr("Nastavení SMB nelze načíst.")
	}
	oldPresent := fileExists(m.Path)
	if err := m.write(ctx, s); err != nil {
		return SmbService{}, err
	}
	if err := m.apply(ctx, old, s, !oldPresent); err != nil {
		return SmbService{}, err
	}
	slog.Info("helper: smb.set", "enabled", s.Enabled, "workgroup", s.Workgroup, "minProtocol", s.MinProtocol,
		"netbios", s.NetBIOS, "user", actor)
	return m.state(ctx)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// write checks the config with testparm and writes smb.json and smb.conf.
func (m *SmbManager) write(ctx context.Context, s smb.Settings) error {
	text := smb.Render(s)
	// testparm follows the include; the fragment must exist (empty before
	// the first share).
	if !fileExists(m.Fragment) {
		if err := atomicfile.Write(m.Fragment, []byte(shares.Render(nil, nil)), 0o644); err != nil {
			slog.Error("helper: write samba fragment", "err", err)
			return internalErr("Konfiguraci Samby nelze zapsat.")
		}
	}
	tmp, err := os.CreateTemp("", "nas-revival-smbconf-*.conf")
	if err != nil {
		return internalErr("Konfiguraci Samby nelze ověřit.")
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(text)
	tmp.Close()
	if werr != nil {
		return internalErr("Konfiguraci Samby nelze ověřit.")
	}
	if out, err := m.Run(ctx, "testparm", "-s", tmp.Name()); err != nil {
		slog.Error("helper: testparm rejected smb.conf", "err", err, "out", strings.TrimSpace(string(out)))
		return internalErr("Konfigurace Samby je neplatná; nic se nezměnilo.")
	}
	if err := smb.Save(m.Path, s); err != nil {
		slog.Error("helper: write smb.json", "err", err)
		return internalErr("Nastavení SMB nelze uložit.")
	}
	if err := atomicfile.Write(m.Conf, []byte(text), 0o644); err != nil {
		slog.Error("helper: write smb.conf", "err", err)
		return internalErr("Konfiguraci Samby nelze zapsat.")
	}
	return nil
}

// apply switches smbd and nmbd to match s. A running smbd reloads its
// config; changing NetBIOS changes the listening ports and needs a restart.
func (m *SmbManager) apply(ctx context.Context, old, s smb.Settings, first bool) error {
	if !s.Enabled {
		if err := m.systemctl(ctx, "disable", "--now", "smbd.service", "nmbd.service"); err != nil {
			return internalErr("Nastavení je uložené, ale Sambu se nepodařilo vypnout.")
		}
		return nil
	}
	running := m.active(ctx, "smbd.service")
	if err := m.systemctl(ctx, "enable", "--now", "smbd.service"); err != nil {
		return internalErr("Nastavení je uložené, ale Sambu se nepodařilo spustit.")
	}
	if running {
		if first || old.NetBIOS != s.NetBIOS {
			if err := m.systemctl(ctx, "restart", "smbd.service"); err != nil {
				return internalErr("Nastavení je uložené, ale Sambu se nepodařilo restartovat.")
			}
		} else if out, err := m.Run(ctx, "smbcontrol", "smbd", "reload-config"); err != nil {
			slog.Warn("helper: smbcontrol reload-config failed", "err", err, "out", strings.TrimSpace(string(out)))
		}
	}
	if s.NetBIOS {
		if err := m.systemctl(ctx, "enable", "--now", "nmbd.service"); err != nil {
			return internalErr("Nastavení je uložené, ale NetBIOS (nmbd) se nepodařilo spustit.")
		}
		if old.NetBIOS {
			m.systemctl(ctx, "restart", "nmbd.service")
		}
	} else if err := m.systemctl(ctx, "disable", "--now", "nmbd.service"); err != nil {
		return internalErr("Nastavení je uložené, ale NetBIOS (nmbd) se nepodařilo vypnout.")
	}
	return nil
}

func (m *SmbManager) systemctl(ctx context.Context, args ...string) error {
	out, err := m.Run(ctx, "systemctl", args...)
	if err != nil {
		slog.Error("helper: systemctl", "args", args, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return err
}

// Status lists sessions and open files (SMB-02); smbstatus only reads.
func (m *SmbManager) Status(ctx context.Context) (smb.Status, error) {
	empty := smb.Status{Sessions: []smb.Session{}, OpenFiles: []smb.OpenFile{}}
	if !m.installed() || !m.active(ctx, "smbd.service") {
		return empty, nil
	}
	out, err := m.Run(ctx, "smbstatus", "--json")
	if err != nil {
		slog.Error("helper: smbstatus", "err", err)
		return empty, internalErr("Stav Samby nelze zjistit.")
	}
	var paths map[string]string
	if m.SharePaths != nil {
		paths = m.SharePaths()
	}
	st, err := smb.ParseStatus(jsonPart(out), paths)
	if err != nil {
		slog.Error("helper: parse smbstatus", "err", err)
		return empty, internalErr("Stav Samby nelze přečíst.")
	}
	return st, nil
}

// jsonPart skips the notes smbstatus prints before the JSON on a fresh
// install ("locking.tdb not initialised").
func jsonPart(b []byte) []byte {
	if i := strings.IndexByte(string(b), '{'); i > 0 {
		return b[i:]
	}
	return b
}

// Restore writes an older smb.json back (config rollback) and applies it; a
// version without smb.json means SMB off.
func (m *SmbManager) Restore(ctx context.Context, data []byte, present bool) error {
	s := smb.Default()
	if present {
		p, err := smb.Parse(data)
		if err != nil {
			return invalid("", "Uložená verze nastavení SMB není platná.")
		}
		s = p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, _ := smb.Load(m.Path)
	if m.installed() {
		if err := m.write(ctx, s); err != nil {
			return err
		}
	} else if present {
		// Without Samba only the settings file goes back.
		if err := smb.Save(m.Path, s); err != nil {
			return internalErr("Nastavení SMB nelze uložit.")
		}
	}
	if !present {
		if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return internalErr("Nastavení SMB nelze odstranit.")
		}
	}
	if !m.installed() {
		return nil
	}
	return m.apply(ctx, old, s, false)
}
