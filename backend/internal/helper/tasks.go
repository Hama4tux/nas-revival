package helper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/tasks"
)

// TaskList is the result of task.get and task.set.
type TaskList struct {
	Tasks []tasks.Task `json:"tasks"`
}

// TaskManager owns tasks.json and the generated timer units (W3.3).
type TaskManager struct {
	Path    string // tasks.json
	UnitDir string // /etc/systemd/system
	Run     Runner
	// NewID returns a fresh id for a task saved without one.
	NewID func() string
	// SharesPath is shares.json; backup destinations are checked against it
	// (empty skips the check, tests only).
	SharesPath string
	mu         sync.Mutex
}

var generatedTimerRe = regexp.MustCompile(`^nas-revival-task-([a-z0-9]{1,16})\.timer$`)

func randomTaskID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func taskFieldErr(err error) error {
	var fe *tasks.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// ValidateTasks checks a task list (the web process runs it too).
func ValidateTasks(in []tasks.Task) ([]tasks.Task, error) {
	l, err := tasks.Validate(in)
	return l, taskFieldErr(err)
}

func (m *TaskManager) load() ([]tasks.Task, error) {
	l, err := tasks.Load(m.Path)
	if err != nil {
		slog.Error("helper: read tasks.json", "err", err)
		return nil, internalErr("Seznam úloh nelze načíst.")
	}
	return l, nil
}

// Get returns the saved list or the default one.
func (m *TaskManager) Get(context.Context) (TaskList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, err := m.load()
	return TaskList{Tasks: l}, err
}

// Set validates the list, assigns ids, writes tasks.json and reconciles the
// timers. If that fails, the previous tasks.json comes back.
func (m *TaskManager) Set(ctx context.Context, in []tasks.Task, actor string) (TaskList, error) {
	l, err := ValidateTasks(in)
	if err != nil {
		return TaskList{}, err
	}
	newID := m.NewID
	if newID == nil {
		newID = randomTaskID
	}
	used := map[string]bool{}
	for _, t := range l {
		used[t.ID] = true
	}
	for i := range l {
		for l[i].ID == "" {
			if id := newID(); !used[id] && tasks.ValidID(id) {
				l[i].ID, used[id] = id, true
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkDests(l); err != nil {
		return TaskList{}, err
	}
	old, oldErr := os.ReadFile(m.Path)
	if err := tasks.Save(m.Path, l); err != nil {
		slog.Error("helper: write tasks.json", "err", err)
		return TaskList{}, internalErr("Seznam úloh nelze uložit.")
	}
	if err := m.reconcile(ctx, l); err != nil {
		var rerr error
		if errors.Is(oldErr, fs.ErrNotExist) {
			rerr = os.Remove(m.Path)
		} else if oldErr == nil {
			rerr = atomicfile.Write(m.Path, old, 0o644)
		}
		if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			slog.Error("helper: tasks restore", "err", rerr)
		}
		if prev, lerr := tasks.Load(m.Path); lerr == nil {
			m.reconcile(ctx, prev)
		}
		return TaskList{}, err
	}
	slog.Info("helper: task.set", "count", len(l), "user", actor)
	return TaskList{Tasks: l}, nil
}

// checkDests refuses a backup task whose destination is not an existing
// backupTarget share on the backup volume. A task saved earlier without a
// destination may stay that way (it is migrated later), a new one needs it.
func (m *TaskManager) checkDests(l []tasks.Task) error {
	if m.SharesPath == "" {
		return nil
	}
	var cfg shares.Config
	loaded := false
	var prev []tasks.Task
	for i, t := range l {
		if t.Type != tasks.TypeBackup || t.Backup == nil {
			continue
		}
		field := fmt.Sprintf("tasks[%d].backup.dest.share", i)
		d := t.Backup.Dest
		if d.Share == "" {
			if prev == nil {
				prev, _ = tasks.Load(m.Path)
			}
			if _, known := tasks.Find(prev, t.ID); !known {
				return invalid(field, "Vyberte cíl záloh.")
			}
			continue
		}
		if !loaded {
			c, err := shares.Load(m.SharesPath)
			if err != nil {
				slog.Error("helper: read shares.json", "err", err)
				return internalErr("Konfiguraci sdílených složek nelze načíst.")
			}
			cfg, loaded = c, true
		}
		j := cfg.Find(d.Share)
		if j < 0 {
			return invalid(field, "Sdílená složka neexistuje.")
		}
		if s := cfg.Shares[j]; !s.BackupTarget || s.Volume != tasks.BackupVolume {
			return invalid(field, "Složka nemá volbu Cíl záloh (nebo není na datovém svazku).")
		}
	}
	return nil
}

// Reconcile makes the timer units match tasks.json (helper start).
func (m *TaskManager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, err := m.load()
	if err != nil {
		return err
	}
	return m.reconcile(ctx, l)
}

func (m *TaskManager) systemctl(ctx context.Context, args ...string) error {
	out, err := m.Run(ctx, "systemctl", args...)
	if err != nil {
		slog.Error("helper: systemctl", "args", args, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return err
}

// reconcile tries every step and returns the first error.
func (m *TaskManager) reconcile(ctx context.Context, l []tasks.Task) error {
	var first error
	note := func(err error) {
		if err != nil && first == nil {
			first = internalErr("Časovače úloh se nepodařilo nastavit.")
		}
	}
	want := map[string]bool{}
	for _, t := range l {
		want[t.ID] = true
		p := filepath.Join(m.UnitDir, tasks.TimerName(t.ID))
		text := tasks.TimerUnit(t)
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() {
			if b, err := os.ReadFile(p); err == nil && string(b) == text {
				continue
			}
		}
		// The rename replaces whatever stands there, a symlink included.
		if err := atomicfile.Write(p, []byte(text), 0o644); err != nil {
			slog.Error("helper: write timer", "id", t.ID, "err", err)
			note(err)
		}
	}
	entries, err := os.ReadDir(m.UnitDir)
	if err != nil {
		slog.Error("helper: list timers", "err", err)
		note(err)
	}
	for _, e := range entries {
		sub := generatedTimerRe.FindStringSubmatch(e.Name())
		if sub == nil || want[sub[1]] {
			continue
		}
		note(m.systemctl(ctx, "disable", "--now", "--", e.Name()))
		if err := os.Remove(filepath.Join(m.UnitDir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("helper: remove timer", "name", e.Name(), "err", err)
			note(err)
		}
		// The type of a removed task is unknown: stop both templates.
		note(m.systemctl(ctx, "stop", "--", tasks.ServiceName(sub[1])))
		note(m.systemctl(ctx, "stop", "--", tasks.BackupServiceName(sub[1])))
	}
	note(m.systemctl(ctx, "daemon-reload"))
	for _, t := range l {
		verb := "disable"
		if t.Enabled {
			verb = "enable"
		}
		note(m.systemctl(ctx, verb, "--now", "--", tasks.TimerName(t.ID)))
	}
	return first
}

func (m *TaskManager) find(id string) (tasks.Task, error) {
	if !tasks.ValidID(id) {
		return tasks.Task{}, notFound("Úloha neexistuje.")
	}
	l, err := m.load()
	if err != nil {
		return tasks.Task{}, err
	}
	t, ok := tasks.Find(l, id)
	if !ok {
		return tasks.Task{}, notFound("Úloha neexistuje.")
	}
	return t, nil
}

// Start runs the task now through its service unit (does not wait). A
// restart or shutdown task cannot be started this way: the immediate power
// actions keep their typed confirmation (CORE-12).
func (m *TaskManager) Start(ctx context.Context, id, actor string) error {
	return m.control(ctx, "start", id, actor)
}

// CodeRunNotAllowed: the task type has no "run now".
const CodeRunNotAllowed = "run_not_allowed"

// Stop stops a running task.
func (m *TaskManager) Stop(ctx context.Context, id, actor string) error {
	return m.control(ctx, "stop", id, actor)
}

func (m *TaskManager) control(ctx context.Context, verb, id, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.find(id)
	if err != nil {
		return err
	}
	if verb == "start" && tasks.IsPower(t.Type) {
		return &Error{Code: CodeRunNotAllowed, Message: "Okamžitý restart nebo vypnutí spusťte tlačítkem Napájení NASu v horní liště."}
	}
	if err := m.systemctl(ctx, verb, "--no-block", "--", tasks.ServiceNameFor(t)); err != nil {
		return internalErr("Úlohu se nepodařilo ovládat.")
	}
	slog.Info("helper: task."+map[string]string{"start": "run", "stop": "stop"}[verb], "id", id, "user", actor)
	return nil
}

// handleTasks runs the task operations (W3.3).
func (d *Dispatcher) handleTasks(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Tasks []tasks.Task `json:"tasks"`
		ID    string       `json:"id"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "task.get":
		return d.Tasks.Get(ctx)
	case "task.set":
		if a.Tasks == nil {
			return nil, invalid("tasks", "Chybí seznam úloh.")
		}
		return d.Tasks.Set(ctx, a.Tasks, user)
	case "task.run":
		return struct{}{}, d.Tasks.Start(ctx, a.ID, user)
	case "task.stop":
		return struct{}{}, d.Tasks.Stop(ctx, a.ID, user)
	}
	return nil, invalid("", "Neznámá operace.")
}
