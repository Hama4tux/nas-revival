package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakeBackend struct {
	calls    []string
	smartErr error
	est      *int
	files    int
	size     int64
	recErr   error
	onCall   func()
}

func (f *fakeBackend) SmartTest(_ context.Context, d, tt string) (*int, error) {
	f.calls = append(f.calls, "smart "+d+" "+tt)
	if f.onCall != nil {
		f.onCall()
	}
	return f.est, f.smartErr
}
func (f *fakeBackend) RecycleCleanup(context.Context) (int, int64, error) {
	f.calls = append(f.calls, "recycle")
	if f.onCall != nil {
		f.onCall()
	}
	return f.files, f.size, f.recErr
}

type fix struct {
	r   *Runner
	b   *fakeBackend
	now time.Time
	dir string
}

func newFix(t *testing.T, l []Task) *fix {
	t.Helper()
	dir := t.TempDir()
	if err := Save(filepath.Join(dir, "tasks.json"), l); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "heavy.lock")
	os.WriteFile(lock, nil, 0o660)
	f := &fix{b: &fakeBackend{}, now: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC), dir: dir}
	f.r = &Runner{TasksPath: filepath.Join(dir, "tasks.json"), HistoryPath: filepath.Join(dir, "task-history.json"),
		LockPath: lock, Wait: time.Hour, Retry: 30 * time.Second, Backend: f.b,
		Now:   func() time.Time { return f.now },
		Sleep: func(_ context.Context, d time.Duration) error { f.now = f.now.Add(d); return nil }}
	return f
}

func hist(t *testing.T, f *fix, id string) []Run {
	t.Helper()
	h, err := LoadHistory(f.r.HistoryPath)
	if err != nil {
		t.Fatal(err)
	}
	return h.Runs[id]
}

func TestRunSmartOK(t *testing.T) {
	f := newFix(t, []Task{okSmart()})
	est := 90
	f.b.est = &est
	run, err := f.r.Run(context.Background(), "a1b2c3d4")
	if err != nil || run.Result != ResultOK || run.Message != "Test spuštěn, odhad 90 min." {
		t.Fatalf("%+v %v", run, err)
	}
	if fmt.Sprint(f.b.calls) != "[smart ata-EXAMPLE_SERIAL long]" {
		t.Fatalf("%v", f.b.calls)
	}
	if h := hist(t, f, "a1b2c3d4"); len(h) != 1 || h[0].Result != ResultOK {
		t.Fatalf("%+v", h)
	}
}

func TestRunSmartRunningAndFailed(t *testing.T) {
	f := newFix(t, []Task{okSmart()})
	f.b.smartErr = ErrTestRunning
	run, _ := f.r.Run(context.Background(), "a1b2c3d4")
	if run.Result != ResultSkipped || run.Message != "Na disku už běží test SMART." {
		t.Fatalf("%+v", run)
	}
	f.b.smartErr = errors.New("Disk nebyl nalezen.")
	run, _ = f.r.Run(context.Background(), "a1b2c3d4")
	if run.Result != ResultFailed || run.Message != "Disk nebyl nalezen." {
		t.Fatalf("%+v", run)
	}
	if h := hist(t, f, "a1b2c3d4"); len(h) != 2 || h[0].Result != ResultFailed {
		t.Fatalf("newest first: %+v", h)
	}
}

func TestRunRecycleAndDisabled(t *testing.T) {
	d := Default()
	d[0].Enabled = false
	f := newFix(t, d)
	f.b.files, f.b.size = 12, 5*(1<<20)+(1<<19)
	run, err := f.r.Run(context.Background(), "recycle")
	if err != nil || run.Result != ResultOK || run.Message != "Odstraněno 12 souborů (5,5 MB)." {
		t.Fatalf("%+v %v", run, err)
	}
	f.b.recErr = errors.New("Vysypání koše bylo přerušeno.")
	if run, _ = f.r.Run(context.Background(), "recycle"); run.Result != ResultFailed {
		t.Fatalf("%+v", run)
	}
}

func TestRunUnknown(t *testing.T) {
	f := newFix(t, Default())
	if _, err := f.r.Run(context.Background(), "nope"); !errors.Is(err, ErrUnknownTask) {
		t.Fatal(err)
	}
	if len(f.b.calls) != 0 {
		t.Fatal("backend called")
	}
}

func holdLock(t *testing.T, path string) *os.File {
	t.Helper()
	h, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(h.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestRunLockTimeoutSkipped(t *testing.T) {
	f := newFix(t, Default())
	holdLock(t, f.r.LockPath)
	sleeps := 0
	f.r.Sleep = func(_ context.Context, d time.Duration) error { sleeps++; f.now = f.now.Add(d); return nil }
	run, err := f.r.Run(context.Background(), "recycle")
	if err != nil || run.Result != ResultSkipped || run.Message != "Běžela jiná náročná úloha." {
		t.Fatalf("%+v %v", run, err)
	}
	if sleeps != 120 || len(f.b.calls) != 0 {
		t.Fatalf("sleeps %d calls %v", sleeps, f.b.calls)
	}
}

func TestRunWaitsForLock(t *testing.T) {
	f := newFix(t, Default())
	h := holdLock(t, f.r.LockPath)
	n := 0
	f.r.Sleep = func(_ context.Context, d time.Duration) error {
		f.now = f.now.Add(d)
		if n++; n == 3 {
			h.Close()
		}
		return nil
	}
	run, _ := f.r.Run(context.Background(), "recycle")
	if run.Result != ResultOK || n != 3 {
		t.Fatalf("%+v n=%d", run, n)
	}
}

func TestRunLockMissing(t *testing.T) {
	f := newFix(t, Default())
	os.Remove(f.r.LockPath)
	run, _ := f.r.Run(context.Background(), "recycle")
	if run.Result != ResultFailed || run.Message != "Chybí soubor zámku náročných úloh (systemd-tmpfiles)." {
		t.Fatalf("%+v", run)
	}
	if _, err := os.Stat(f.r.LockPath); err == nil {
		t.Fatal("runner created the lock file")
	}
}

func TestRunStoppedWhileWaiting(t *testing.T) {
	f := newFix(t, Default())
	holdLock(t, f.r.LockPath)
	ctx, cancel := context.WithCancel(context.Background())
	f.r.Sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	run, _ := f.r.Run(ctx, "recycle")
	if run.Result != ResultStopped {
		t.Fatalf("%+v", run)
	}
	if h := hist(t, f, "recycle"); len(h) != 1 || h[0].Result != ResultStopped {
		t.Fatalf("%+v", h)
	}
}

func TestRunStoppedDuringWork(t *testing.T) {
	f := newFix(t, Default())
	ctx, cancel := context.WithCancel(context.Background())
	f.b.onCall = cancel
	f.b.recErr = context.Canceled
	if run, _ := f.r.Run(ctx, "recycle"); run.Result != ResultStopped {
		t.Fatalf("%+v", run)
	}
}

func TestRunReleasesLock(t *testing.T) {
	f := newFix(t, Default())
	f.r.Run(context.Background(), "recycle")
	h := holdLock(t, f.r.LockPath) // fails the test if the runner kept it
	h.Close()
}

func TestHistoryBoundAndDrop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "task-history.json")
	for i := 0; i < 35; i++ {
		run := Run{Start: time.Unix(int64(i), 0).UTC(), End: time.Unix(int64(i), 0).UTC(), Result: ResultOK, Message: fmt.Sprint(i)}
		if err := Record(p, []string{"a", "gone"}, "a", run); err != nil {
			t.Fatal(err)
		}
	}
	Record(p, []string{"a", "gone"}, "gone", Run{Result: ResultOK})
	h, _ := LoadHistory(p)
	if len(h.Runs["a"]) != MaxRuns || h.Runs["a"][0].Message != "34" || h.Runs["a"][29].Message != "5" {
		t.Fatalf("%d %+v", len(h.Runs["a"]), h.Runs["a"][0])
	}
	if len(h.Runs["gone"]) != 1 {
		t.Fatal("gone missing")
	}
	// "gone" is no longer in tasks.json: dropped by the next write.
	Record(p, []string{"a"}, "a", Run{Result: ResultOK})
	h, _ = LoadHistory(p)
	if _, ok := h.Runs["gone"]; ok {
		t.Fatal("removed id kept")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestHistoryMessageClipped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	long := make([]rune, 500)
	for i := range long {
		long[i] = 'č'
	}
	Record(p, []string{"a"}, "a", Run{Result: ResultFailed, Message: string(long)})
	h, _ := LoadHistory(p)
	if n := len([]rune(h.Runs["a"][0].Message)); n != MaxMessageLen {
		t.Fatalf("len %d", n)
	}
}
