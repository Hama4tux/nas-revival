package helper

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/updates"
)

// Update Manager, Debian part (UPD-01, ADR 0016): updates.get, updates.check,
// updates.upgrade. The helper only reads the job's state.json and starts
// nas-revival-updates@check|upgrade.service; apt-get runs in that unit.

// Error codes of the update operations.
const (
	CodeUpdatesRunning = "updates_running" // a check or upgrade job runs
	CodeUpdatesStale   = "updates_stale"   // the list changed since the user loaded it
	CodeUpdatesSpace   = "updates_space"   // not enough free space or memory
)

// Upgrade preconditions: room for the downloaded .debs, their unpacked files
// and dpkg's backups, and memory for apt's dependency solver (~60 MB on the NAS).
const (
	upgradeMinFree   = 300 << 20
	upgradeFreeRatio = 3
	upgradeMinMemory = 128 << 20
)

// UpdatesStatus is the answer of the update operations.
type UpdatesStatus struct {
	updates.State
	// Running is the job running now: "", "check" or "upgrade".
	Running string `json:"running"`
	// RestartRecommended: packages used by running processes were upgraded
	// since the last boot, or a package asked for a reboot (/run/reboot-required).
	RestartRecommended bool `json:"restartRecommended"`
	// FreeBytes is the free space of the root file system.
	FreeBytes uint64 `json:"freeBytes"`
}

// UpdateManager serves the updates.* operations.
type UpdateManager struct {
	Dir string // updates.DefaultStateDir
	Run Runner
	// Free returns the space available to root on the root file system.
	Free func() (uint64, error)
	// Memory returns MemAvailable + SwapFree.
	Memory   func() (uint64, error)
	BootTime func() (time.Time, error)
	// RebootRequired reports whether /run/reboot-required exists.
	RebootRequired func() bool

	mu sync.Mutex // one start at a time
}

func (m *UpdateManager) running(ctx context.Context) string {
	for _, job := range []string{updates.JobUpgrade, updates.JobCheck} {
		out, _ := m.Run(ctx, "systemctl", "is-active", updates.ServiceName(job))
		switch strings.TrimSpace(string(out)) {
		case "active", "activating", "deactivating", "reloading":
			return job
		}
	}
	return ""
}

// Get returns the state of the last check and upgrade.
func (m *UpdateManager) Get(ctx context.Context) (UpdatesStatus, error) {
	st, err := updates.Load(m.Dir)
	if err != nil {
		slog.Warn("helper: updates state unreadable", "err", err)
		return UpdatesStatus{}, internalErr("Stav aktualizací nelze přečíst.")
	}
	out := UpdatesStatus{State: st, Running: m.running(ctx)}
	if u := st.Upgrade; u != nil && u.Result == updates.ResultRunning && out.Running != updates.JobUpgrade {
		// Power loss or a killed job: dpkg may be half done; the next
		// upgrade starts with dpkg --configure -a.
		c := *u
		c.Result, c.Message = updates.ResultFailed, "Instalace byla přerušena. Spusťte ji znovu, dokončí rozpracované balíčky."
		out.Upgrade = &c
	}
	if boot, err := m.BootTime(); err != nil || st.RestartSince == nil || !st.RestartSince.Equal(boot) {
		out.RestartPackages = []string{}
	}
	out.RestartRecommended = len(out.RestartPackages) > 0 || (m.RebootRequired != nil && m.RebootRequired())
	if free, err := m.Free(); err == nil {
		out.FreeBytes = free
	}
	return out, nil
}

func (m *UpdateManager) start(ctx context.Context, job string) error {
	if _, err := m.Run(ctx, "systemctl", "start", "--no-block", "--", updates.ServiceName(job)); err != nil {
		slog.Warn("helper: update job not started", "job", job, "err", err)
		return internalErr("Úlohu aktualizací se nepodařilo spustit.")
	}
	return nil
}

// Check starts a refresh of the package lists.
func (m *UpdateManager) Check(ctx context.Context) (UpdatesStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.running(ctx); job != "" {
		return UpdatesStatus{}, &Error{Code: CodeUpdatesRunning, Message: runningMsg(job)}
	}
	if err := m.start(ctx, updates.JobCheck); err != nil {
		return UpdatesStatus{}, err
	}
	return m.Get(ctx)
}

func runningMsg(job string) string {
	if job == updates.JobUpgrade {
		return "Aktualizace se právě instalují."
	}
	return "Kontrola aktualizací právě běží."
}

// Upgrade starts the installation of the list computed at listedAt
// (RFC 3339 as returned by Get).
func (m *UpdateManager) Upgrade(ctx context.Context, listedAt string) (UpdatesStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.running(ctx); job != "" {
		return UpdatesStatus{}, &Error{Code: CodeUpdatesRunning, Message: runningMsg(job)}
	}
	st, err := updates.Load(m.Dir)
	if err != nil {
		return UpdatesStatus{}, internalErr("Stav aktualizací nelze přečíst.")
	}
	want, err := time.Parse(time.RFC3339Nano, listedAt)
	if err != nil {
		return UpdatesStatus{}, invalid("listedAt", "Chybí čas seznamu aktualizací.")
	}
	if st.ListedAt == nil || !st.ListedAt.Equal(want) {
		return UpdatesStatus{}, &Error{Code: CodeUpdatesStale, Message: "Seznam aktualizací se mezitím změnil. Prohlédněte si ho znovu."}
	}
	if len(st.Packages) == 0 {
		return UpdatesStatus{}, invalid("", "Není co instalovat.")
	}
	need := uint64(upgradeMinFree) + upgradeFreeRatio*uint64(st.DownloadBytes)
	if free, err := m.Free(); err != nil || free < need {
		return UpdatesStatus{}, &Error{Code: CodeUpdatesSpace,
			Message: "Na systémovém disku není dost místa (potřeba " + mib(need) + ")."}
	}
	if mem, err := m.Memory(); err != nil || mem < upgradeMinMemory {
		return UpdatesStatus{}, &Error{Code: CodeUpdatesSpace,
			Message: "Málo volné paměti pro instalaci (potřeba " + mib(upgradeMinMemory) + "). Zastavte náročné úlohy a zkuste to znovu."}
	}
	if err := m.start(ctx, updates.JobUpgrade); err != nil {
		return UpdatesStatus{}, err
	}
	return m.Get(ctx)
}

func mib(b uint64) string { return strconv.FormatUint((b+(1<<19))>>20, 10) + " MiB" }
