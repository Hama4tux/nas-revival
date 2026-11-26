package tasks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"
)

// HeavyLockPath serialises heavy jobs (shared with the pull backup job).
const HeavyLockPath = "/run/lock/nas-heavy.lock"

// Messages recorded in the history (Czech, no secrets, no user data paths).
const (
	MsgLockTimeout = "Běžela jiná náročná úloha."
	MsgTestRunning = "Na disku už běží test SMART."
	MsgLockMissing = "Chybí soubor zámku náročných úloh (systemd-tmpfiles)."
	MsgFailed      = "Úloha selhala."
	MsgStopped     = "Úloha byla zastavena."
)

// ErrTestRunning is what Backend.SmartTest returns when the disk already
// runs a self-test.
var ErrTestRunning = errors.New("self-test already running")

// ErrUnknownTask: the id is not in tasks.json.
var ErrUnknownTask = errors.New("unknown task")

// Backend does the actual work through the privileged helper. Errors that
// are not ErrTestRunning should carry a short Czech message fit for the
// history.
type Backend interface {
	// SmartTest starts a self-test; estimate is the expected duration in
	// minutes (nil when unknown).
	SmartTest(ctx context.Context, diskID, test string) (estimate *int, err error)
	RecycleCleanup(ctx context.Context) (files int, bytes int64, err error)
}

// Runner executes one task (the template unit's ExecStart).
type Runner struct {
	TasksPath   string
	HistoryPath string
	// LockPath is the heavy lock; Wait is how long to wait for it, Retry the
	// polling interval.
	LockPath string
	Wait     time.Duration
	Retry    time.Duration
	Backend  Backend
	Now      func() time.Time
	// Sleep waits d or returns the context error.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewRunner returns a Runner with the production defaults.
func NewRunner(tasksPath, historyPath string, b Backend) *Runner {
	return &Runner{TasksPath: tasksPath, HistoryPath: historyPath, LockPath: HeavyLockPath,
		Wait: 3 * time.Hour, Retry: 30 * time.Second, Backend: b, Now: time.Now, Sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run executes the task id (also when it is disabled) and records the
// outcome. The error is ErrUnknownTask or a problem loading the list; the
// run's own failure is in the returned Run (Result failed or stopped).
func (r *Runner) Run(ctx context.Context, id string) (Run, error) {
	l, err := Load(r.TasksPath)
	if err != nil {
		return Run{}, fmt.Errorf("tasks.json: %w", err)
	}
	t, ok := Find(l, id)
	if !ok {
		return Run{}, ErrUnknownTask
	}
	known := make([]string, 0, len(l))
	for _, x := range l {
		known = append(known, x.ID)
	}
	run := Run{Start: r.Now()}
	run.Result, run.Message = r.execute(ctx, t)
	run.End = r.Now()
	if err := Record(r.HistoryPath, known, id, run); err != nil {
		slog.Error("task history not written", "id", id, "err", err)
	}
	slog.Info("task finished", "id", id, "type", t.Type, "result", run.Result)
	return run, nil
}

func (r *Runner) execute(ctx context.Context, t Task) (string, string) {
	lock, res, msg := r.lock(ctx)
	if lock == nil {
		return res, msg
	}
	// The lock is held until the runner exits; a SMART test keeps running on
	// the drive itself after that.
	defer lock.Close()
	switch t.Type {
	case TypeSmart:
		est, err := r.Backend.SmartTest(ctx, t.Smart.DiskID, t.Smart.Test)
		switch {
		case ctx.Err() != nil:
			return ResultStopped, MsgStopped
		case errors.Is(err, ErrTestRunning):
			return ResultSkipped, MsgTestRunning
		case err != nil:
			return ResultFailed, failMsg(err)
		case est != nil:
			return ResultOK, fmt.Sprintf("Test spuštěn, odhad %d min.", *est)
		}
		return ResultOK, "Test spuštěn."
	case TypeRecycle:
		files, size, err := r.Backend.RecycleCleanup(ctx)
		switch {
		case ctx.Err() != nil:
			return ResultStopped, MsgStopped
		case err != nil:
			return ResultFailed, failMsg(err)
		}
		mb := strings.Replace(fmt.Sprintf("%.1f", float64(size)/(1<<20)), ".", ",", 1)
		return ResultOK, fmt.Sprintf("Odstraněno %d souborů (%s MB).", files, mb)
	}
	return ResultFailed, MsgFailed
}

func failMsg(err error) string {
	if m := strings.TrimSpace(err.Error()); m != "" {
		return m
	}
	return MsgFailed
}

// lock takes the heavy lock, polling every Retry for up to Wait. A nil file
// means no lock: result and message say why.
func (r *Runner) lock(ctx context.Context) (*os.File, string, string) {
	// No O_CREATE: the file belongs to root:nas-heavy (tmpfiles) and
	// fs.protected_regular in the sticky /run/lock forbids opening a file
	// of another owner that the runner would have created itself.
	f, err := os.OpenFile(r.LockPath, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ResultFailed, MsgLockMissing
	}
	if err != nil {
		slog.Error("heavy lock not opened", "err", err)
		return nil, ResultFailed, "Zámek náročných úloh nelze otevřít."
	}
	deadline := r.Now().Add(r.Wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, "", ""
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			slog.Error("heavy lock failed", "err", err)
			return nil, ResultFailed, "Zámek náročných úloh nelze získat."
		}
		if !r.Now().Before(deadline) {
			f.Close()
			return nil, ResultSkipped, MsgLockTimeout
		}
		if err := r.Sleep(ctx, r.Retry); err != nil {
			f.Close()
			return nil, ResultStopped, MsgStopped
		}
	}
}
