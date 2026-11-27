package helper

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/firewall"
)

// FirewallPending is a rule set that is loaded into the kernel but not saved
// yet; it waits for confirmation (ADR 0014).
type FirewallPending struct {
	Deadline time.Time         `json:"deadline"`
	Next     firewall.Settings `json:"next"`
}

// FirewallState is the result of firewall.get / apply / confirm.
type FirewallState struct {
	// Supported: the kernel has nftables (`nft list tables` works).
	Supported bool `json:"supported"`
	// Active: table ip nas_revival exists in the kernel.
	Active   bool              `json:"active"`
	Settings firewall.Settings `json:"settings"`
	Pending  *FirewallPending  `json:"pending"`
}

// FirewallPaths are the files the firewall manager touches.
type FirewallPaths struct {
	ConfigFile string // /etc/nas-revival/firewall.json
	BootFile   string // /etc/nftables.d/nas-revival.nft
	StateDir   string // holds the pending marker and the script being loaded
}

// FirewallManager implements confirm-or-rollback for the nft rule set: a new
// rule set is loaded into the kernel only, the boot file and firewall.json
// are written on Confirm, a rollback re-loads the boot file.
type FirewallManager struct {
	Paths FirewallPaths
	Run   Runner
	Now   func() time.Time
	// After schedules f after d and returns a stop function (time.AfterFunc in production).
	After  func(d time.Duration, f func()) (stop func() bool)
	Window time.Duration

	mu      sync.Mutex
	pending *FirewallPending
	stop    func() bool
	gen     int
}

// NewFirewallManager returns a manager with production clock and timers.
func NewFirewallManager(p FirewallPaths, run Runner) *FirewallManager {
	return &FirewallManager{Paths: p, Run: run, Now: time.Now, Window: ConfirmWindow,
		After: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }}
}

func (m *FirewallManager) markerPath() string {
	return filepath.Join(m.Paths.StateDir, "firewall-pending.json")
}
func (m *FirewallManager) scriptPath() string { return filepath.Join(m.Paths.StateDir, "firewall.nft") }

func fwFieldErr(err error) error {
	var fe *firewall.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// ValidateFirewall checks firewall settings against the client's address
// (the web process runs it too).
func ValidateFirewall(s firewall.Settings, clientAddr string) (firewall.Settings, error) {
	n, err := firewall.Validate(s, clientAddr)
	return n, fwFieldErr(err)
}

// tables runs `nft list tables`; an error means no nftables support.
func (m *FirewallManager) tables(ctx context.Context) (supported, active bool) {
	out, err := m.Run(ctx, "nft", "list", "tables")
	if err != nil {
		return false, false
	}
	return true, strings.Contains(string(out), "table ip "+firewall.Table)
}

func (m *FirewallManager) stateLocked(ctx context.Context) (FirewallState, error) {
	s, err := firewall.Load(m.Paths.ConfigFile)
	if err != nil {
		slog.Error("helper: read firewall.json", "err", err)
		return FirewallState{}, internalErr("Nastavení firewallu nelze načíst.")
	}
	st := FirewallState{Settings: s}
	st.Supported, st.Active = m.tables(ctx)
	if m.pending != nil {
		cp := *m.pending
		st.Pending = &cp
	}
	return st, nil
}

// Get returns the saved settings, kernel state and pending change.
func (m *FirewallManager) Get(ctx context.Context) (FirewallState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked(ctx)
}

// HasPending reports a change waiting for confirmation.
func (m *FirewallManager) HasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending != nil
}

// load writes script to the state directory and loads it; check=true only
// verifies it (`nft -c`).
func (m *FirewallManager) load(ctx context.Context, script string, check bool) error {
	if err := os.MkdirAll(m.Paths.StateDir, 0o700); err != nil {
		return err
	}
	if err := atomicfile.Write(m.scriptPath(), []byte(script), 0o600); err != nil {
		return err
	}
	args := []string{"-f", m.scriptPath()}
	if check {
		args = append([]string{"-c"}, args...)
	}
	out, err := m.Run(ctx, "nft", args...)
	if err != nil {
		slog.Error("helper: nft failed", "check", check, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return err
}

// Apply validates s (again: the web process is not trusted), checks the rule
// set and loads it into the kernel, then starts the confirmation timer.
func (m *FirewallManager) Apply(ctx context.Context, in firewall.Settings, clientAddr string) (FirewallState, error) {
	if clientAddr == "" {
		return FirewallState{}, invalid("", "Chybí adresa klienta.")
	}
	s, err := ValidateFirewall(in, clientAddr)
	if err != nil {
		return FirewallState{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		return FirewallState{}, &Error{Code: CodeChangePending, Message: "Jiná změna firewallu čeká na potvrzení."}
	}
	if supported, _ := m.tables(ctx); !supported {
		return FirewallState{}, &Error{Code: CodeNotInstalled, Message: "Jádro NASu nepodporuje firewall (nftables)."}
	}
	script := firewall.Render(s)
	if err := m.load(ctx, script, true); err != nil {
		os.Remove(m.scriptPath())
		return FirewallState{}, internalErr("Pravidla firewallu jsou neplatná, nic se nezměnilo.")
	}
	p := &FirewallPending{Deadline: m.Now().Add(m.Window), Next: s}
	mdata, _ := json.Marshal(p)
	if err := os.MkdirAll(m.Paths.StateDir, 0o700); err != nil {
		return FirewallState{}, internalErr("Nelze vytvořit stavový adresář.")
	}
	if err := atomicfile.Write(m.markerPath(), mdata, 0o600); err != nil {
		return FirewallState{}, internalErr("Nelze zapsat stav čekající změny.")
	}
	m.pending = p
	m.arm(m.Window)
	if _, err := m.Run(ctx, "nft", "-f", m.scriptPath()); err != nil {
		slog.Error("helper: nft load failed", "err", err)
		m.rollbackLocked(ctx)
		return FirewallState{}, internalErr("Firewall se nepodařilo použít, vráceno původní nastavení.")
	}
	os.Remove(m.scriptPath())
	return m.stateLocked(ctx)
}

// arm starts the timer; caller holds mu.
func (m *FirewallManager) arm(d time.Duration) {
	if m.stop != nil {
		m.stop()
	}
	m.gen++
	gen := m.gen
	m.stop = m.After(d, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending == nil || m.gen != gen {
			return
		}
		slog.Warn("firewall change not confirmed, rolling back")
		m.rollbackLocked(context.Background())
	})
}

// Confirm saves the pending rule set (boot file, firewall.json).
func (m *FirewallManager) Confirm(ctx context.Context) (FirewallState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return FirewallState{}, &Error{Code: CodeNothingPending, Message: "Není co potvrdit, změna už byla vrácena."}
	}
	if !m.Now().Before(m.pending.Deadline) {
		m.rollbackLocked(ctx)
		return FirewallState{}, &Error{Code: CodeNothingPending, Message: "Čas na potvrzení vypršel, změna byla vrácena."}
	}
	next := m.pending.Next
	if err := m.persist(next); err != nil {
		slog.Error("helper: save firewall", "err", err)
		m.rollbackLocked(ctx)
		return FirewallState{}, internalErr("Firewall se nepodařilo uložit, vráceno původní nastavení.")
	}
	m.clearLocked()
	return m.stateLocked(ctx)
}

// persist writes the boot file (removed when the firewall is off) and
// firewall.json.
func (m *FirewallManager) persist(s firewall.Settings) error {
	if s.Enabled {
		if err := mkdirMode(filepath.Dir(m.Paths.BootFile), 0o755); err != nil {
			return err
		}
		if err := atomicfile.Write(m.Paths.BootFile, []byte(firewall.Render(s)), 0o644); err != nil {
			return err
		}
	} else if err := os.Remove(m.Paths.BootFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return firewall.Save(m.Paths.ConfigFile, s)
}

func (m *FirewallManager) clearLocked() {
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	m.gen++
	m.pending = nil
	os.Remove(m.markerPath())
	os.Remove(m.scriptPath())
}

func (m *FirewallManager) rollbackLocked(ctx context.Context) {
	m.restore(ctx)
	m.clearLocked()
}

// restore re-loads the boot file, or removes the table when there is none.
func (m *FirewallManager) restore(ctx context.Context) {
	if _, err := os.Stat(m.Paths.BootFile); err == nil {
		if out, err := m.Run(ctx, "nft", "-f", m.Paths.BootFile); err != nil {
			slog.Error("firewall rollback: boot file not loaded", "err", err, "out", strings.TrimSpace(string(out)))
		}
		return
	}
	if err := m.load(ctx, firewall.Render(firewall.Default()), false); err != nil {
		slog.Error("firewall rollback: table not removed", "err", err)
	}
}

// Recover handles a marker left by a previous run: the rules were never
// confirmed, so they are rolled back at once instead of re-arming the timer.
func (m *FirewallManager) Recover(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(m.markerPath()); err != nil {
		return
	}
	slog.Warn("unconfirmed firewall change found at start, rolling back")
	m.restore(ctx)
	m.clearLocked()
}
