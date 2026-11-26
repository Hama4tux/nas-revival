package helper

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"nas-revival/backend/internal/diskpower"
)

// Error codes of the power operations.
const (
	// CodeBlocked: preflight found work that a restart or shutdown would cut off.
	CodeBlocked = "blocked"
	// CodePowerPending: a restart or shutdown is already on its way.
	CodePowerPending = "power_pending"
)

// Blocker codes reported by power.preflight (CORE-13).
const (
	BlockSmartTest      = "smart_test"      // SMART self-test running on DiskID
	BlockCopy           = "copy_running"    // COPY button backup (nsa310-copy) running
	BlockBackup         = "backup_running"  // backup pull job running
	BlockTask           = "task_running"    // a scheduled heavy task holds the heavy-task lock
	BlockPackageManager = "package_manager" // apt/dpkg running
	BlockNetworkPending = "network_pending" // network change waits for confirmation
	BlockCheckFailed    = "check_failed"    // a check could not run; DiskID when it was a disk
)

// copyUnit is the COPY button job (deploy/debian/nsa310-copy.service).
const copyUnit = "nsa310-copy.service"

// backupUnit is the interim backup pull job installed outside NAS Revival;
// backup jobs of W4.3 replace it (ADR 0008).
const backupUnit = "nas-pull.service"

// DefaultHeavyLock is held (flock) for the whole run by scheduled tasks
// (W3.3) and the backup pull job; tmpfiles creates it root-owned.
const DefaultHeavyLock = "/run/lock/nas-heavy.lock"

// packageManagers are process names (/proc/<pid>/comm, max 15 chars) whose
// interruption can leave dpkg half-configured.
var packageManagers = map[string]bool{
	"apt": true, "apt-get": true, "aptitude": true, "dpkg": true, "unattended-upgr": true,
}

// PowerBlocker is one reason not to restart or shut down now.
type PowerBlocker struct {
	Code   string `json:"code"`
	DiskID string `json:"diskId,omitempty"`
}

type PowerPreflight struct {
	Blockers []PowerBlocker `json:"blockers"`
}

type PowerActionResult struct {
	Action   string         `json:"action"`
	Forced   bool           `json:"forced"`
	Blockers []PowerBlocker `json:"blockers"`
}

// SystemPowerManager restarts and shuts down the NAS (ops power.preflight,
// power.reboot, power.shutdown; CORE-12, CORE-13).
type SystemPowerManager struct {
	Disks      func() ([]PowerDisk, error)
	Run        Runner
	ProcRoot   string      // /proc
	HeavyLock  string      // heavy-task lock file; empty = not checked
	NetPending func() bool // network change waiting for confirmation
	Sync       func()      // flushes file-system buffers (syscall.Sync)
	// After runs f after Delay; the delay lets the HTTP answer reach the
	// browser before the web service is stopped.
	After func(d time.Duration, f func())
	Delay time.Duration

	mu        sync.Mutex
	scheduled string
}

// Preflight lists what a restart or shutdown would interrupt. It never wakes
// a sleeping disk: a disk in standby runs no self-test.
func (m *SystemPowerManager) Preflight(ctx context.Context) PowerPreflight {
	b := []PowerBlocker{}
	b = append(b, m.smartBlockers(ctx)...)
	if m.unitRunning(ctx, copyUnit) {
		b = append(b, PowerBlocker{Code: BlockCopy})
	}
	if m.unitRunning(ctx, backupUnit) {
		// The pull job holds the heavy lock too; one reason is enough.
		b = append(b, PowerBlocker{Code: BlockBackup})
	} else if m.heavyLockHeld() {
		b = append(b, PowerBlocker{Code: BlockTask})
	}
	if m.packageManagerRunning() {
		b = append(b, PowerBlocker{Code: BlockPackageManager})
	}
	if m.NetPending != nil && m.NetPending() {
		b = append(b, PowerBlocker{Code: BlockNetworkPending})
	}
	return PowerPreflight{Blockers: b}
}

func (m *SystemPowerManager) smartBlockers(ctx context.Context) []PowerBlocker {
	disks, err := m.Disks()
	if err != nil {
		slog.Warn("helper: power preflight: disks not listed", "err", err)
		return []PowerBlocker{{Code: BlockCheckFailed}}
	}
	var out []PowerBlocker
	for _, d := range disks {
		if !d.Capable {
			continue
		}
		dev := "/dev/" + d.Name
		st, err := m.Run(ctx, "hdparm", "-C", dev)
		if err != nil {
			slog.Warn("helper: power preflight: hdparm -C failed", "disk", d.ID, "err", err)
			out = append(out, PowerBlocker{Code: BlockCheckFailed, DiskID: d.ID})
			continue
		}
		if s := diskpower.ParseState(string(st)); s == diskpower.StateStandby || s == diskpower.StateSleep {
			continue
		}
		caps, err := (&SmartManager{Run: m.Run}).smartctl(ctx, "-j", "-c", dev)
		if err != nil {
			slog.Warn("helper: power preflight: smartctl -c failed", "disk", d.ID, "err", err)
			out = append(out, PowerBlocker{Code: BlockCheckFailed, DiskID: d.ID})
			continue
		}
		if a := caps.ATAData; a != nil && a.SelfTest != nil && a.SelfTest.Status != nil && a.SelfTest.Status.Value>>4 == 0xF {
			out = append(out, PowerBlocker{Code: BlockSmartTest, DiskID: d.ID})
		}
	}
	return out
}

// unitRunning: systemctl is-active exits non-zero for an inactive unit (and
// for a oneshot that is still "activating"), so only the printed state counts.
func (m *SystemPowerManager) unitRunning(ctx context.Context, unit string) bool {
	out, _ := m.Run(ctx, "systemctl", "is-active", unit)
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "deactivating", "reloading":
		return true
	}
	return false
}

// heavyLockHeld opens the lock without O_CREAT (a missing file means no task
// ever ran since boot) and probes it with a non-blocking shared lock.
func (m *SystemPowerManager) heavyLockHeld() bool {
	if m.HeavyLock == "" {
		return false
	}
	f, err := os.Open(m.HeavyLock)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("helper: power preflight: heavy lock not opened", "err", err)
		}
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func (m *SystemPowerManager) packageManagerRunning() bool {
	comms, _ := filepath.Glob(filepath.Join(m.ProcRoot, "[0-9]*", "comm"))
	for _, p := range comms {
		b, err := os.ReadFile(p)
		if err == nil && packageManagers[strings.TrimSpace(string(b))] {
			return true
		}
	}
	return false
}

// systemctlVerb maps the API action to the systemd verb.
var systemctlVerb = map[string]string{"reboot": "reboot", "shutdown": "poweroff"}

// Do restarts or shuts down the NAS after a fresh preflight. Blockers stop
// it unless force is set (the second confirmation in the UI). user is only
// recorded in the log.
func (m *SystemPowerManager) Do(ctx context.Context, action string, force bool, user string) (PowerActionResult, error) {
	verb, ok := systemctlVerb[action]
	if !ok {
		return PowerActionResult{}, invalid("action", "Neznámá akce.")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scheduled != "" {
		return PowerActionResult{}, &Error{Code: CodePowerPending, Message: "Restart nebo vypnutí už probíhá."}
	}
	pf := m.Preflight(ctx)
	if len(pf.Blockers) > 0 && !force {
		return PowerActionResult{}, &Error{Code: CodeBlocked, Message: "Probíhající činnost by se přerušila."}
	}
	m.Sync()
	m.scheduled = action
	codes := make([]string, 0, len(pf.Blockers))
	for _, b := range pf.Blockers {
		codes = append(codes, b.Code)
	}
	slog.Info("helper: power."+action, "user", user, "forced", len(pf.Blockers) > 0, "blockers", codes)
	m.After(m.Delay, func() {
		// The request context is gone by now.
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := m.Run(c, "systemctl", verb); err != nil {
			slog.Error("helper: systemctl "+verb+" failed", "err", err)
			m.mu.Lock()
			m.scheduled = ""
			m.mu.Unlock()
		}
	})
	return PowerActionResult{Action: action, Forced: len(pf.Blockers) > 0, Blockers: pf.Blockers}, nil
}
