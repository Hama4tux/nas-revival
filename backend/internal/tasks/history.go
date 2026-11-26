package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"
	"unicode/utf8"

	"nas-revival/backend/internal/atomicfile"
)

// Run results.
const (
	ResultOK      = "ok"
	ResultFailed  = "failed"
	ResultSkipped = "skipped"
	ResultStopped = "stopped"
)

// Limits of the history.
const (
	MaxRuns       = 30
	MaxMessageLen = 200
)

// DefaultHistoryPath is the run history in the web process' state directory.
const DefaultHistoryPath = "/var/lib/nas-revival/task-history.json"

// Run is one recorded run of a task.
type Run struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Result  string    `json:"result"`
	Message string    `json:"message"`
}

// History is task-history.json: runs per task id, newest first.
type History struct {
	Version int              `json:"version"`
	Runs    map[string][]Run `json:"runs"`
}

// LoadHistory reads the history; a missing file is an empty history.
func LoadHistory(path string) (History, error) {
	h := History{Version: 1, Runs: map[string][]Run{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return History{Version: 1, Runs: map[string][]Run{}}, err
	}
	if h.Runs == nil {
		h.Runs = map[string][]Run{}
	}
	return h, nil
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= MaxMessageLen {
		return s
	}
	r := []rune(s)
	return string(r[:MaxMessageLen-1]) + "…"
}

// Record puts run first in the history of id, keeps MaxRuns per task and
// drops the runs of ids that are not in known. The file is written under an
// exclusive lock on path+".lock" (0600).
func Record(path string, known []string, id string, run Run) error {
	lf, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	h, err := LoadHistory(path)
	if err != nil {
		// A damaged history must not stop the recording; start over.
		h = History{Version: 1, Runs: map[string][]Run{}}
	}
	run.Message = clip(run.Message)
	keep := map[string]bool{id: true}
	for _, k := range known {
		keep[k] = true
	}
	for k := range h.Runs {
		if !keep[k] {
			delete(h.Runs, k)
		}
	}
	l := append([]Run{run}, h.Runs[id]...)
	if len(l) > MaxRuns {
		l = l[:MaxRuns]
	}
	h.Runs[id] = l
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o600)
}
