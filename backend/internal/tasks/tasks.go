// Package tasks holds the scheduled tasks (W3.3): the validated list in
// tasks.json, the systemd timer rendering, the run history and the runner the
// task units start. The task type is a fixed enum; there is deliberately no
// free-form command (TASK-02).
package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"nas-revival/backend/internal/atomicfile"
)

// DefaultConfigPath is the task list (0644, root, written by the helper).
const DefaultConfigPath = "/etc/nas-revival/tasks.json"

// Task types.
const (
	TypeSmart   = "smart"
	TypeRecycle = "recycle-cleanup"
	// TypeBackup pulls a backup from a server (W4.3, ADR 0008).
	TypeBackup = "backup-pull"
)

// Schedule kinds.
const (
	KindDaily   = "daily"
	KindWeekly  = "weekly"
	KindMonthly = "monthly"
)

// Limits.
const (
	MaxTasks   = 32
	MaxNameLen = 40
	// MaxBackupTasks limits the backup-pull tasks.
	MaxBackupTasks = 8
)

// RecycleID is the id of the default task that replaces the old recycle loop.
const RecycleID = "recycle"

var (
	idRe     = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
	diskIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	timeRe   = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// Weekdays in canonical order.
var weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// ValidID reports whether s is a well-formed task id.
func ValidID(s string) bool { return idRe.MatchString(s) }

// Schedule says when a task runs (NAS local time).
type Schedule struct {
	Kind     string   `json:"kind"`
	Time     string   `json:"time"`
	Weekdays []string `json:"weekdays,omitempty"`
	Day      int      `json:"day,omitempty"`
}

// SmartParams are the parameters of a smart task.
type SmartParams struct {
	DiskID string `json:"diskId"`
	Test   string `json:"test"`
}

// Task is one scheduled task. Later types (backup-pull) add their own
// parameter object here the same way as Smart.
type Task struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	Enabled  bool          `json:"enabled"`
	Schedule Schedule      `json:"schedule"`
	Smart    *SmartParams  `json:"smart,omitempty"`
	Backup   *BackupParams `json:"backup,omitempty"`
}

// File is tasks.json.
type File struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// FieldError names the invalid field.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func bad(field, msg string) error { return &FieldError{field, msg} }

// Default is the list of an unconfigured NAS: the daily recycle bin cleanup.
func Default() []Task {
	return []Task{{ID: RecycleID, Name: "Vysypání košů", Type: TypeRecycle, Enabled: true,
		Schedule: Schedule{Kind: KindDaily, Time: "03:30"}}}
}

// Validate checks a task list and returns the normalised copy (trimmed names,
// weekdays in canonical order). Empty ids are allowed here; the helper
// assigns them before it saves. Field names are "tasks[i].field".
func Validate(in []Task) ([]Task, error) {
	if len(in) > MaxTasks {
		return nil, bad("tasks", fmt.Sprintf("Nejvýše %d úloh.", MaxTasks))
	}
	backups := 0
	out := make([]Task, 0, len(in))
	seen := map[string]bool{}
	for i, t := range in {
		p := fmt.Sprintf("tasks[%d].", i)
		if t.ID != "" {
			if !idRe.MatchString(t.ID) {
				return nil, bad(p+"id", "Neplatné ID úlohy.")
			}
			if seen[t.ID] {
				return nil, bad(p+"id", "ID úlohy se opakuje.")
			}
			seen[t.ID] = true
		}
		t.Name = strings.TrimSpace(t.Name)
		if n := utf8.RuneCountInString(t.Name); n < 1 || n > MaxNameLen {
			return nil, bad(p+"name", fmt.Sprintf("Název musí mít 1 až %d znaků.", MaxNameLen))
		}
		for _, r := range t.Name {
			if unicode.IsControl(r) {
				return nil, bad(p+"name", "Název obsahuje nepovolený znak.")
			}
		}
		switch t.Type {
		case TypeSmart:
			if t.Smart == nil {
				return nil, bad(p+"smart", "Chybí parametry testu SMART.")
			}
			if !diskIDRe.MatchString(t.Smart.DiskID) {
				return nil, bad(p+"smart.diskId", "Neplatné ID disku.")
			}
			if t.Smart.Test != "short" && t.Smart.Test != "long" {
				return nil, bad(p+"smart.test", "Typ testu musí být short nebo long.")
			}
			if t.Backup != nil {
				return nil, bad(p+"backup", "Tento typ úlohy nemá parametry zálohy.")
			}
		case TypeRecycle:
			if t.Smart != nil || t.Backup != nil {
				return nil, bad(p+"smart", "Tento typ úlohy nemá parametry.")
			}
		case TypeBackup:
			if t.Smart != nil {
				return nil, bad(p+"smart", "Tento typ úlohy nemá parametry SMART.")
			}
			backups++
			if backups > MaxBackupTasks {
				return nil, bad("tasks", fmt.Sprintf("Nejvýše %d záloh.", MaxBackupTasks))
			}
			b, err := validateBackup(p+"backup.", t.Backup)
			if err != nil {
				return nil, err
			}
			t.Backup = b
		default:
			return nil, bad(p+"type", "Neznámý typ úlohy.")
		}
		s, err := validateSchedule(p+"schedule.", t.Schedule)
		if err != nil {
			return nil, err
		}
		t.Schedule = s
		out = append(out, t)
	}
	return out, nil
}

func validateSchedule(p string, s Schedule) (Schedule, error) {
	if !timeRe.MatchString(s.Time) {
		return s, bad(p+"time", "Čas musí být ve tvaru HH:MM.")
	}
	switch s.Kind {
	case KindDaily:
		if len(s.Weekdays) > 0 || s.Day != 0 {
			return s, bad(p+"kind", "Denní plán nemá dny ani den v měsíci.")
		}
	case KindWeekly:
		if s.Day != 0 {
			return s, bad(p+"day", "Týdenní plán nemá den v měsíci.")
		}
		if len(s.Weekdays) == 0 {
			return s, bad(p+"weekdays", "Vyberte alespoň jeden den v týdnu.")
		}
		have := map[string]bool{}
		for _, d := range s.Weekdays {
			if indexOf(weekdays, d) < 0 {
				return s, bad(p+"weekdays", "Neplatný den v týdnu.")
			}
			if have[d] {
				return s, bad(p+"weekdays", "Den v týdnu se opakuje.")
			}
			have[d] = true
		}
		ordered := make([]string, 0, len(have))
		for _, d := range weekdays {
			if have[d] {
				ordered = append(ordered, d)
			}
		}
		s.Weekdays = ordered
	case KindMonthly:
		if len(s.Weekdays) > 0 {
			return s, bad(p+"weekdays", "Měsíční plán nemá dny v týdnu.")
		}
		if s.Day < 1 || s.Day > 28 {
			return s, bad(p+"day", "Den v měsíci musí být 1 až 28.")
		}
	default:
		return s, bad(p+"kind", "Neznámý druh plánu.")
	}
	return s, nil
}

func indexOf(l []string, s string) int {
	for i, v := range l {
		if v == s {
			return i
		}
	}
	return -1
}

// OnCalendar renders a valid schedule as a systemd calendar expression.
func OnCalendar(s Schedule) string {
	switch s.Kind {
	case KindWeekly:
		days := make([]string, 0, len(s.Weekdays))
		for _, d := range s.Weekdays {
			days = append(days, strings.ToUpper(d[:1])+d[1:])
		}
		return fmt.Sprintf("%s *-*-* %s:00", strings.Join(days, ","), s.Time)
	case KindMonthly:
		return fmt.Sprintf("*-*-%02d %s:00", s.Day, s.Time)
	}
	return fmt.Sprintf("*-*-* %s:00", s.Time)
}

// TimerName is the generated timer unit of a task.
func TimerName(id string) string { return "nas-revival-task-" + id + ".timer" }

// ServiceName is the instance of the template service that runs a task
// (not a backup-pull task, see ServiceNameFor).
func ServiceName(id string) string { return "nas-revival-task@" + id + ".service" }

// BackupServiceName is the instance of the backup template service.
func BackupServiceName(id string) string { return "nas-revival-backup@" + id + ".service" }

// ServiceNameFor picks the template service by the task type.
func ServiceNameFor(t Task) string {
	if t.Type == TypeBackup {
		return BackupServiceName(t.ID)
	}
	return ServiceName(t.ID)
}

// TimerUnit renders the timer unit file. The task name is not part of it.
func TimerUnit(t Task) string {
	return "# Generated by NAS Revival from /etc/nas-revival/tasks.json – do not edit.\n" +
		"[Unit]\nDescription=NAS Revival task " + t.ID + "\n" +
		"[Timer]\nOnCalendar=" + OnCalendar(t.Schedule) + "\n" +
		"Persistent=true\nAccuracySec=1min\n" +
		"Unit=" + ServiceNameFor(t) + "\n" +
		"[Install]\nWantedBy=timers.target\n"
}

// Parse decodes tasks.json strictly and validates it.
func Parse(data []byte) ([]Task, error) {
	var f File
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if f.Version != 1 {
		return nil, errors.New("unsupported tasks.json version")
	}
	if f.Tasks == nil {
		f.Tasks = []Task{}
	}
	l, err := Validate(f.Tasks)
	if err != nil {
		return nil, err
	}
	for _, t := range l {
		if t.ID == "" {
			return nil, errors.New("task without id")
		}
	}
	return l, nil
}

// Load reads tasks.json; a missing file yields the default list.
func Load(path string) ([]Task, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Marshal renders the file content.
func Marshal(l []Task) ([]byte, error) {
	if l == nil {
		l = []Task{}
	}
	b, err := json.MarshalIndent(File{Version: 1, Tasks: l}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Save writes tasks.json (0644) atomically.
func Save(path string, l []Task) error {
	b, err := Marshal(l)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o644)
}

// Find returns the task with the id.
func Find(l []Task, id string) (Task, bool) {
	for _, t := range l {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
