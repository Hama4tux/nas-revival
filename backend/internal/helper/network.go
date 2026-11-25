package helper

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
)

// ConfirmWindow is how long a network change waits for confirmation.
const ConfirmWindow = 180 * time.Second

// Pending describes a change that waits for confirmation.
type Pending struct {
	Deadline time.Time `json:"deadline"`
	Previous NetConfig `json:"previous"`
	Next     NetConfig `json:"next"`
}

type marker struct {
	Pending
	Iface   string `json:"iface"`
	HadFile bool   `json:"hadFile"`
}

// NetworkState is the result of network.get / apply / confirm.
type NetworkState struct {
	Interface string    `json:"interface"`
	Config    NetConfig `json:"config"`
	Current   Current   `json:"current"`
	Pending   *Pending  `json:"pending"`
}

// NetworkPaths are the files the network manager touches.
type NetworkPaths struct {
	NetworkFile string // /etc/systemd/network/20-wired.network
	StateDir    string // holds pending.json and the backup
	SysClassNet string // /sys/class/net
}

// Manager implements the safe-apply state machine for the network file.
type Manager struct {
	Paths NetworkPaths
	Iface string
	Run   Runner
	Now   func() time.Time
	// After schedules f after d and returns a stop function (time.AfterFunc in production).
	After  func(d time.Duration, f func()) (stop func() bool)
	Window time.Duration

	mu      sync.Mutex
	pending *marker
	stop    func() bool
	gen     int
}

// NewManager returns a Manager with production clock and timers.
func NewManager(p NetworkPaths, iface string, run Runner) *Manager {
	return &Manager{Paths: p, Iface: iface, Run: run, Now: time.Now, Window: ConfirmWindow,
		After: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }}
}

func (m *Manager) markerPath() string { return filepath.Join(m.Paths.StateDir, "pending.json") }
func (m *Manager) backupPath() string { return filepath.Join(m.Paths.StateDir, "20-wired.network.bak") }

func (m *Manager) checkIface(name string) error {
	if !ValidIfaceName(name) {
		return internalErr("Neplatný název rozhraní.")
	}
	if _, err := os.Stat(filepath.Join(m.Paths.SysClassNet, name)); err != nil {
		return internalErr("Síťové rozhraní neexistuje.")
	}
	return nil
}

func (m *Manager) readConfig() NetConfig {
	data, err := os.ReadFile(m.Paths.NetworkFile)
	if err != nil {
		return DefaultNetConfig()
	}
	return ParseNetwork(string(data))
}

// Get returns the stored config, live status and pending change.
func (m *Manager) Get(ctx context.Context) NetworkState {
	m.mu.Lock()
	var p *Pending
	if m.pending != nil {
		cp := m.pending.Pending
		p = &cp
	}
	m.mu.Unlock()
	return NetworkState{Interface: m.Iface, Config: m.readConfig(),
		Current: ReadCurrent(ctx, m.Run, m.Iface), Pending: p}
}

// HasPending reports a network change waiting for confirmation.
func (m *Manager) HasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending != nil
}

func (m *Manager) reload(ctx context.Context, iface string) error {
	if _, err := m.Run(ctx, "networkctl", "reload"); err != nil {
		return err
	}
	_, err := m.Run(ctx, "networkctl", "reconfigure", iface)
	return err
}

// Apply writes cfg and starts the confirmation timer.
func (m *Manager) Apply(ctx context.Context, cfg NetConfig) (NetworkState, error) {
	cfg, err := ValidateNetConfig(cfg)
	if err != nil {
		return NetworkState{}, err
	}
	return m.applyContent(ctx, cfg, []byte(RenderNetwork(cfg)), true)
}

// Restore writes an older version of the network file byte for byte (config
// rollback) with the same confirmation window as Apply. present=false
// removes the file.
func (m *Manager) Restore(ctx context.Context, data []byte, present bool) (NetworkState, error) {
	next := DefaultNetConfig()
	if present {
		cfg, err := ValidateNetConfig(ParseNetwork(string(data)))
		if err != nil {
			return NetworkState{}, invalid("", "Uložená verze nastavení sítě není platná.")
		}
		next = cfg
	}
	return m.applyContent(ctx, next, data, present)
}

func (m *Manager) applyContent(ctx context.Context, next NetConfig, content []byte, present bool) (NetworkState, error) {
	if err := m.checkIface(m.Iface); err != nil {
		return NetworkState{}, err
	}
	m.mu.Lock()
	if m.pending != nil {
		m.mu.Unlock()
		return NetworkState{}, &Error{Code: CodeChangePending, Message: "Jiná změna sítě čeká na potvrzení."}
	}
	old, rerr := os.ReadFile(m.Paths.NetworkFile)
	hadFile := rerr == nil
	if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		m.mu.Unlock()
		return NetworkState{}, internalErr("Nelze přečíst stávající nastavení sítě.")
	}
	prev := DefaultNetConfig()
	if hadFile {
		prev = ParseNetwork(string(old))
	}
	mk := &marker{Pending: Pending{Deadline: m.Now().Add(m.Window), Previous: prev, Next: next}, Iface: m.Iface, HadFile: hadFile}
	fail := func(msg string) (NetworkState, error) {
		m.mu.Unlock()
		return NetworkState{}, internalErr(msg)
	}
	if err := os.MkdirAll(m.Paths.StateDir, 0o700); err != nil {
		return fail("Nelze vytvořit stavový adresář.")
	}
	if hadFile {
		if err := atomicfile.Write(m.backupPath(), old, 0o600); err != nil {
			return fail("Nelze zálohovat nastavení sítě.")
		}
	}
	mdata, _ := json.Marshal(mk)
	if err := atomicfile.Write(m.markerPath(), mdata, 0o600); err != nil {
		return fail("Nelze zapsat stav čekající změny.")
	}
	m.pending = mk
	m.arm(m.Window)
	var werr error
	if present {
		werr = atomicfile.Write(m.Paths.NetworkFile, content, 0o644)
	} else if err := os.Remove(m.Paths.NetworkFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		werr = err
	}
	if werr != nil {
		m.rollbackLocked(ctx)
		return fail("Nelze zapsat nastavení sítě.")
	}
	if err := m.reload(ctx, m.Iface); err != nil {
		slog.Error("network reload failed", "err", err)
		m.rollbackLocked(ctx)
		return fail("Nastavení sítě se nepodařilo použít, vráceno původní.")
	}
	m.mu.Unlock()
	return m.Get(ctx), nil
}

// arm starts the timer; caller holds mu.
func (m *Manager) arm(d time.Duration) {
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
		slog.Warn("network change not confirmed, rolling back")
		m.rollbackLocked(context.Background())
	})
}

// Confirm accepts the pending change.
func (m *Manager) Confirm(ctx context.Context) (NetworkState, error) {
	m.mu.Lock()
	if m.pending == nil {
		m.mu.Unlock()
		return NetworkState{}, &Error{Code: CodeNothingPending, Message: "Není co potvrdit, změna už byla vrácena."}
	}
	if !m.Now().Before(m.pending.Deadline) {
		m.rollbackLocked(ctx)
		m.mu.Unlock()
		return NetworkState{}, &Error{Code: CodeNothingPending, Message: "Čas na potvrzení vypršel, změna byla vrácena."}
	}
	m.clearLocked()
	m.mu.Unlock()
	return m.Get(ctx), nil
}

func (m *Manager) clearLocked() {
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	m.gen++
	m.pending = nil
	os.Remove(m.markerPath())
	os.Remove(m.backupPath())
}

// rollbackLocked restores the backup and reapplies it; caller holds mu.
func (m *Manager) rollbackLocked(ctx context.Context) {
	mk := m.pending
	if mk == nil {
		return
	}
	m.rollbackFrom(ctx, mk.Iface, mk.HadFile)
	m.clearLocked()
}

func (m *Manager) rollbackFrom(ctx context.Context, iface string, hadFile bool) {
	if hadFile {
		data, err := os.ReadFile(m.backupPath())
		if err != nil {
			slog.Error("rollback: backup unreadable", "err", err)
			return
		}
		if err := atomicfile.Write(m.Paths.NetworkFile, data, 0o644); err != nil {
			slog.Error("rollback: restore failed", "err", err)
			return
		}
	} else if err := os.Remove(m.Paths.NetworkFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Error("rollback: remove failed", "err", err)
		return
	}
	if !ValidIfaceName(iface) {
		iface = m.Iface
	}
	if err := m.reload(ctx, iface); err != nil {
		slog.Error("rollback: reload failed", "err", err)
	}
}

// Recover handles a marker left by a previous run: expired → roll back,
// otherwise re-arm the timer for the remaining time.
func (m *Manager) Recover(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.markerPath())
	if err != nil {
		return
	}
	var mk marker
	if json.Unmarshal(data, &mk) != nil {
		slog.Error("pending marker corrupt, restoring backup if present")
		_, berr := os.Stat(m.backupPath())
		m.rollbackFrom(ctx, m.Iface, berr == nil)
		m.pending = nil
		m.clearLocked()
		return
	}
	m.pending = &mk
	remaining := mk.Deadline.Sub(m.Now())
	if remaining <= 0 {
		slog.Warn("expired network change found at start, rolling back")
		m.rollbackLocked(ctx)
		return
	}
	m.arm(remaining)
}
