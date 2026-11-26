package helper

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"syscall"

	"nas-revival/backend/internal/procs"
)

// Error codes of process.kill.
const (
	CodeProcessChanged   = "process_changed"
	CodeProcessProtected = "process_protected"
)

// ProcessKillResult is the result of process.kill.
type ProcessKillResult struct {
	PID    int    `json:"pid"`
	Name   string `json:"name"`
	Signal string `json:"signal"`
}

// ProcessManager stops processes for the Resource Monitor (MON-06). It reads
// /proc/PID/stat again right before the signal and applies procs.Protection,
// whatever the web process claims.
type ProcessManager struct {
	Root string // "/" on the NAS
	// Kill sends the signal; nil = syscall.Kill.
	Kill func(pid int, sig syscall.Signal) error
	Self int // own PID; 0 = os.Getpid()
}

// Stop sends SIGTERM (or SIGKILL with force) to pid, if it still is the
// process that started at start (clock ticks after boot) and is not protected.
func (m *ProcessManager) Stop(_ context.Context, pid int, start uint64, force bool, user string) (ProcessKillResult, error) {
	// start 0 is valid: PID 1 and the first kernel threads start at tick 0.
	if pid < 1 || pid > 1<<22 {
		return ProcessKillResult{}, invalid("pid", "Neplatný proces.")
	}
	s, err := procs.ReadStat(m.Root, pid)
	if errors.Is(err, procs.ErrNotFound) {
		return ProcessKillResult{}, notFound("Proces už neběží.")
	}
	if err != nil {
		slog.Error("process stat failed", "pid", pid, "err", err)
		return ProcessKillResult{}, internalErr("Proces se nepodařilo načíst.")
	}
	if s.Start != start {
		return ProcessKillResult{}, &Error{Code: CodeProcessChanged, Message: "Proces s tímto číslem mezitím skončil a běží jiný. Obnovte seznam."}
	}
	self := m.Self
	if self == 0 {
		self = os.Getpid()
	}
	if reason := procs.Protection(s, self); reason != "" {
		slog.Warn("process kill refused: protected", "pid", pid, "name", s.Name, "reason", reason, "by", user)
		return ProcessKillResult{}, &Error{Code: CodeProcessProtected, Message: "Tento proces je chráněný, NAS ho potřebuje k chodu."}
	}
	sig, name := syscall.SIGTERM, "TERM"
	if force {
		sig, name = syscall.SIGKILL, "KILL"
	}
	kill := m.Kill
	if kill == nil {
		kill = syscall.Kill
	}
	if err := kill(pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return ProcessKillResult{}, notFound("Proces už neběží.")
		}
		slog.Error("process kill failed", "pid", pid, "err", err)
		return ProcessKillResult{}, internalErr("Signál se nepodařilo odeslat.")
	}
	slog.Info("process signalled", "pid", pid, "name", s.Name, "signal", name, "by", user)
	return ProcessKillResult{PID: pid, Name: s.Name, Signal: name}, nil
}
