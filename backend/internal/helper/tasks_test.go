package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/tasks"
)

type fakeSysctl struct {
	calls []string
	fail  string // a call prefix that fails
}

func (f *fakeSysctl) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		return nil, errors.New("exit 1")
	}
	return nil, nil
}

type taskFix struct {
	m    *TaskManager
	f    *fakeSysctl
	dir  string
	unit string
}

func newTaskFix(t *testing.T) *taskFix {
	t.Helper()
	dir := t.TempDir()
	unit := filepath.Join(dir, "system")
	os.MkdirAll(unit, 0o755)
	f := &fakeSysctl{}
	n := 0
	m := &TaskManager{Path: filepath.Join(dir, "tasks.json"), UnitDir: unit, Run: f.run,
		NewID: func() string { n++; return "0000000" + string(rune('0'+n)) }}
	return &taskFix{m: m, f: f, dir: dir, unit: unit}
}

func smartTask(id string, enabled bool) tasks.Task {
	return tasks.Task{ID: id, Name: "SMART", Type: tasks.TypeSmart, Enabled: enabled,
		Schedule: tasks.Schedule{Kind: tasks.KindWeekly, Time: "02:00", Weekdays: []string{"mon", "thu"}},
		Smart:    &tasks.SmartParams{DiskID: "ata-EXAMPLE_SERIAL", Test: "short"}}
}

func calls(f *fakeSysctl) string { return strings.Join(f.calls, "|") }

func TestTaskGetDefault(t *testing.T) {
	x := newTaskFix(t)
	l, err := x.m.Get(context.Background())
	if err != nil || len(l.Tasks) != 1 || l.Tasks[0].ID != "recycle" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestTaskSetWritesFilesAndReconciles(t *testing.T) {
	x := newTaskFix(t)
	in := []tasks.Task{smartTask("", true), smartTask("keep", false)}
	l, err := x.m.Set(context.Background(), in, "admin")
	if err != nil || l.Tasks[0].ID != "00000001" || l.Tasks[1].ID != "keep" {
		t.Fatalf("%+v %v", l, err)
	}
	b, err := os.ReadFile(filepath.Join(x.unit, "nas-revival-task-00000001.timer"))
	if err != nil || !strings.Contains(string(b), "OnCalendar=Mon,Thu *-*-* 02:00:00\n") || !strings.Contains(string(b), "Unit=nas-revival-task@00000001.service") {
		t.Fatalf("%s %v", b, err)
	}
	want := "systemctl daemon-reload|systemctl enable --now -- nas-revival-task-00000001.timer|systemctl disable --now -- nas-revival-task-keep.timer"
	if calls(x.f) != want {
		t.Fatalf("calls %s", calls(x.f))
	}
	got, _ := tasks.Load(x.m.Path)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if st, _ := os.Stat(x.m.Path); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestTaskSetRemovesStaleTimers(t *testing.T) {
	x := newTaskFix(t)
	ctx := context.Background()
	x.m.Set(ctx, []tasks.Task{smartTask("aa", true), smartTask("bb", true)}, "admin")
	other := filepath.Join(x.unit, "unrelated.timer")
	os.WriteFile(other, []byte("x"), 0o644)
	// A symlink with the generated name for an unknown id is removed, not followed.
	target := filepath.Join(x.dir, "target")
	os.WriteFile(target, []byte("keep"), 0o644)
	os.Symlink(target, filepath.Join(x.unit, "nas-revival-task-cc.timer"))
	x.f.calls = nil
	if _, err := x.m.Set(ctx, []tasks.Task{smartTask("aa", true)}, "admin"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bb", "cc"} {
		if _, err := os.Lstat(filepath.Join(x.unit, "nas-revival-task-"+id+".timer")); err == nil {
			t.Errorf("%s timer kept", id)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated unit removed")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Error("symlink target touched")
	}
	c := calls(x.f)
	for _, w := range []string{
		"systemctl disable --now -- nas-revival-task-bb.timer|", "systemctl stop -- nas-revival-task@bb.service",
		"systemctl disable --now -- nas-revival-task-cc.timer|", "systemctl stop -- nas-revival-task@cc.service",
		"systemctl daemon-reload|systemctl enable --now -- nas-revival-task-aa.timer",
	} {
		if !strings.Contains(c, w) {
			t.Errorf("missing %q in %s", w, c)
		}
	}
	if strings.Index(c, "disable --now -- nas-revival-task-bb.timer") > strings.Index(c, "stop -- nas-revival-task@bb.service") {
		t.Error("disable must come before stop")
	}
}

func TestTaskSetUnchangedTimerNotRewritten(t *testing.T) {
	x := newTaskFix(t)
	ctx := context.Background()
	x.m.Set(ctx, []tasks.Task{smartTask("aa", true)}, "admin")
	p := filepath.Join(x.unit, "nas-revival-task-aa.timer")
	old := mustStat(t, p)
	x.m.Set(ctx, []tasks.Task{smartTask("aa", true)}, "admin")
	if !os.SameFile(old, mustStat(t, p)) {
		t.Fatal("identical timer rewritten")
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestTaskSetRejectsInvalid(t *testing.T) {
	x := newTaskFix(t)
	bad := smartTask("aa", true)
	bad.Smart.Test = "x"
	_, err := x.m.Set(context.Background(), []tasks.Task{bad}, "admin")
	var he *Error
	if !errors.As(err, &he) || he.Code != CodeInvalid || he.Field != "tasks[0].smart.test" {
		t.Fatalf("%v", err)
	}
	if _, serr := os.Stat(x.m.Path); serr == nil || len(x.f.calls) != 0 {
		t.Fatal("changed something")
	}
}

func TestTaskSetRollsBackOnFailure(t *testing.T) {
	x := newTaskFix(t)
	ctx := context.Background()
	x.m.Set(ctx, []tasks.Task{smartTask("aa", true)}, "admin")
	before, _ := os.ReadFile(x.m.Path)
	x.f.fail = "systemctl enable --now -- nas-revival-task-bb.timer"
	_, err := x.m.Set(ctx, []tasks.Task{smartTask("aa", true), smartTask("bb", true)}, "admin")
	if err == nil {
		t.Fatal("no error")
	}
	after, _ := os.ReadFile(x.m.Path)
	if string(before) != string(after) {
		t.Fatalf("not rolled back:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(x.unit, "nas-revival-task-bb.timer")); err == nil {
		t.Fatal("stale timer of rolled back task")
	}
}

func TestTaskReconcileAtStart(t *testing.T) {
	x := newTaskFix(t)
	if err := x.m.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(x.unit, "nas-revival-task-recycle.timer")); err != nil {
		t.Fatal(err)
	}
	if calls(x.f) != "systemctl daemon-reload|systemctl enable --now -- nas-revival-task-recycle.timer" {
		t.Fatal(calls(x.f))
	}
}

func TestTaskRunStopArgv(t *testing.T) {
	x := newTaskFix(t)
	ctx := context.Background()
	if err := x.m.Start(ctx, "recycle", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := x.m.Stop(ctx, "recycle", "admin"); err != nil {
		t.Fatal(err)
	}
	if calls(x.f) != "systemctl start --no-block -- nas-revival-task@recycle.service|systemctl stop --no-block -- nas-revival-task@recycle.service" {
		t.Fatal(calls(x.f))
	}
	x.f.calls = nil
	for _, id := range []string{"nope", "../x", "--now", ""} {
		var he *Error
		if err := x.m.Start(ctx, id, "a"); !errors.As(err, &he) || he.Code != CodeNotFound {
			t.Errorf("%q: %v", id, err)
		}
	}
	if len(x.f.calls) != 0 {
		t.Fatalf("systemctl called: %v", x.f.calls)
	}
}

func TestTaskDispatch(t *testing.T) {
	x := newTaskFix(t)
	d := &Dispatcher{Tasks: x.m}
	ctx := context.Background()
	if _, err := d.Handle(ctx, request{Op: "task.get"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "task.set", Args: []byte(`{}`)}); err == nil {
		t.Fatal("task.set without tasks accepted")
	}
	if _, err := d.Handle(ctx, request{Op: "task.set", Args: []byte(`{"tasks":[],"x":1}`)}); err == nil {
		t.Fatal("unknown arg accepted")
	}
	if _, err := d.Handle(ctx, request{Op: "task.run", Args: []byte(`{"id":"recycle"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "task.run", Args: []byte(`{"id":"zz"}`)}); err == nil {
		t.Fatal("unknown id accepted")
	}
}

// Power preflight and the task runner must look at the same heavy lock.
func TestHeavyLockPathsAgree(t *testing.T) {
	if DefaultHeavyLock != tasks.HeavyLockPath {
		t.Fatalf("preflight lock %q != runner lock %q", DefaultHeavyLock, tasks.HeavyLockPath)
	}
}
