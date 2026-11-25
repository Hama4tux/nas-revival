package helper

import (
	"context"
	"log/slog"
	"strconv"
	"sync"

	"nas-revival/backend/internal/diskactivity"
)

// CodeActivityRunning: one measurement at a time.
const CodeActivityRunning = "activity_running"

// Measurer runs one measurement (diskactivity.Reader in production).
type Measurer interface {
	Measure(ctx context.Context, seconds int) (diskactivity.Result, error)
}

// ActivityManager serves op disk.activity (W1.9). It needs root to read the
// open files of every process; it only reads /proc, /sys and file metadata.
type ActivityManager struct {
	Measure Measurer
	mu      sync.Mutex
}

func (m *ActivityManager) Run(ctx context.Context, seconds int) (diskactivity.Result, error) {
	if seconds < diskactivity.MinSeconds || seconds > diskactivity.MaxSeconds {
		return diskactivity.Result{}, invalid("seconds", "Délka měření musí být "+
			strconv.Itoa(diskactivity.MinSeconds)+" až "+strconv.Itoa(diskactivity.MaxSeconds)+" sekund.")
	}
	if !m.mu.TryLock() {
		return diskactivity.Result{}, &Error{Code: CodeActivityRunning, Message: "Měření aktivity disků už běží. Počkejte na jeho konec."}
	}
	defer m.mu.Unlock()
	res, err := m.Measure.Measure(ctx, seconds)
	if err != nil {
		slog.Error("helper: disk.activity failed", "err", err)
		return diskactivity.Result{}, internalErr("Aktivitu disků nelze změřit.")
	}
	return res, nil
}
