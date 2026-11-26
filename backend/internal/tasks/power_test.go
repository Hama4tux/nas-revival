package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// powerBackend adds the power calls to fakeBackend.
type powerBackend struct {
	*fakeBackend
	blockers [][]string // one answer per preflight; the last one repeats
	pfErr    error
	powerErr []error // one answer per Power call; nil when exhausted
	actions  []string
}

func (p *powerBackend) PowerBlockers(context.Context) ([]string, error) {
	p.calls = append(p.calls, "preflight")
	if p.pfErr != nil {
		return nil, p.pfErr
	}
	if len(p.blockers) == 0 {
		return nil, nil
	}
	b := p.blockers[0]
	if len(p.blockers) > 1 {
		p.blockers = p.blockers[1:]
	}
	return b, nil
}

func (p *powerBackend) Power(_ context.Context, action, user string) error {
	p.actions = append(p.actions, action+" "+user)
	if len(p.powerErr) == 0 {
		return nil
	}
	e := p.powerErr[0]
	p.powerErr = p.powerErr[1:]
	return e
}

func powerTask(id, typ string) Task {
	return Task{ID: id, Name: "Noční " + typ, Type: typ, Enabled: true, Schedule: Schedule{Kind: KindDaily, Time: "01:00"}}
}

func newPowerFix(t *testing.T) (*fix, *powerBackend) {
	t.Helper()
	f := newFix(t, []Task{powerTask("rb", TypeReboot), powerTask("off", TypeShutdown)})
	pb := &powerBackend{fakeBackend: f.b}
	f.r.Backend = pb
	f.r.Uptime = func() (time.Duration, error) { return 2 * time.Hour, nil }
	f.r.MinUptime, f.r.PowerWait, f.r.PowerRetry = 15*time.Minute, time.Hour, time.Minute
	return f, pb
}

func TestPowerTaskRebootAndShutdown(t *testing.T) {
	f, pb := newPowerFix(t)
	run, err := f.r.Run(context.Background(), "rb")
	if err != nil || run.Result != ResultOK || run.Message != "Restart zahájen." {
		t.Fatalf("%+v %v", run, err)
	}
	run, _ = f.r.Run(context.Background(), "off")
	if run.Result != ResultOK || run.Message != "Vypnutí zahájeno." {
		t.Fatalf("%+v", run)
	}
	if strings.Join(pb.actions, "|") != "reboot task:rb|shutdown task:off" {
		t.Fatal(pb.actions)
	}
	if h := hist(t, f, "rb"); len(h) != 1 || h[0].Result != ResultOK {
		t.Fatalf("history %+v", h)
	}
}

func TestPowerTaskIgnoresHeavyLock(t *testing.T) {
	// The preflight reports a held lock as task_running; the runner itself
	// must neither wait for it nor hold it during the power action.
	f, pb := newPowerFix(t)
	holdLock(t, f.r.LockPath)
	if run, _ := f.r.Run(context.Background(), "rb"); run.Result != ResultOK || len(pb.actions) != 1 {
		t.Fatalf("%+v %v", run, pb.actions)
	}
}

func TestPowerTaskSkippedRightAfterBoot(t *testing.T) {
	f, pb := newPowerFix(t)
	f.r.Uptime = func() (time.Duration, error) { return 3 * time.Minute, nil }
	run, _ := f.r.Run(context.Background(), "off")
	if run.Result != ResultSkipped || run.Message != MsgJustBooted || len(pb.calls) != 0 || len(pb.actions) != 0 {
		t.Fatalf("%+v calls %v %v", run, pb.calls, pb.actions)
	}
	f.r.Uptime = func() (time.Duration, error) { return 0, errors.New("no proc") }
	if run, _ := f.r.Run(context.Background(), "off"); run.Result != ResultFailed || len(pb.actions) != 0 {
		t.Fatalf("%+v", run)
	}
}

func TestPowerTaskWaitsForBlockers(t *testing.T) {
	f, pb := newPowerFix(t)
	pb.blockers = [][]string{{"backup_running"}, {"backup_running"}, nil}
	sleeps := 0
	f.r.Sleep = func(_ context.Context, d time.Duration) error { sleeps++; f.now = f.now.Add(d); return nil }
	run, _ := f.r.Run(context.Background(), "rb")
	if run.Result != ResultOK || sleeps != 2 || len(pb.actions) != 1 {
		t.Fatalf("%+v sleeps %d %v", run, sleeps, pb.actions)
	}
}

func TestPowerTaskSkippedWhenBlockedTooLong(t *testing.T) {
	f, pb := newPowerFix(t)
	pb.blockers = [][]string{{"smart_test", "smart_test", "package_manager"}}
	sleeps := 0
	f.r.Sleep = func(_ context.Context, d time.Duration) error { sleeps++; f.now = f.now.Add(d); return nil }
	run, _ := f.r.Run(context.Background(), "off")
	if run.Result != ResultSkipped || run.Message != "Přeskočeno, běží test SMART, probíhá instalace balíčků." {
		t.Fatalf("%+v", run)
	}
	if sleeps != 60 || len(pb.actions) != 0 {
		t.Fatalf("sleeps %d actions %v", sleeps, pb.actions)
	}
}

func TestPowerTaskBlockedRaceRetries(t *testing.T) {
	f, pb := newPowerFix(t)
	pb.powerErr = []error{ErrPowerBlocked}
	run, _ := f.r.Run(context.Background(), "rb")
	if run.Result != ResultOK || len(pb.actions) != 2 {
		t.Fatalf("%+v %v", run, pb.actions)
	}
}

func TestPowerTaskFailuresAndStop(t *testing.T) {
	f, pb := newPowerFix(t)
	pb.powerErr = []error{errors.New("Restart nebo vypnutí už probíhá.")}
	if run, _ := f.r.Run(context.Background(), "rb"); run.Result != ResultFailed || run.Message != "Restart nebo vypnutí už probíhá." {
		t.Fatalf("%+v", run)
	}
	pb.pfErr = errors.New("Pomocná služba neodpovídá.")
	if run, _ := f.r.Run(context.Background(), "rb"); run.Result != ResultFailed {
		t.Fatalf("%+v", run)
	}
	pb.pfErr = nil
	pb.blockers = [][]string{{"copy_running"}}
	ctx, cancel := context.WithCancel(context.Background())
	f.r.Sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	if run, _ := f.r.Run(ctx, "rb"); run.Result != ResultStopped {
		t.Fatalf("%+v", run)
	}
}

func TestPowerTaskNeedsPowerBackend(t *testing.T) {
	f, _ := newPowerFix(t)
	f.r.Backend = f.b // without the power calls
	if run, _ := f.r.Run(context.Background(), "rb"); run.Result != ResultFailed {
		t.Fatalf("%+v", run)
	}
}

func TestPowerTypesValidateAndTimer(t *testing.T) {
	l, err := Validate([]Task{powerTask("rb", TypeReboot), powerTask("off", TypeShutdown)})
	if err != nil || len(l) != 2 {
		t.Fatalf("%v %v", l, err)
	}
	x := powerTask("rb", TypeReboot)
	x.Smart = &SmartParams{DiskID: "a", Test: "short"}
	if _, err := Validate([]Task{x}); err == nil {
		t.Fatal("power task with params accepted")
	}
	many := []Task{}
	for i := 0; i <= MaxPowerTasks; i++ {
		many = append(many, powerTask("", TypeShutdown))
	}
	if _, err := Validate(many); err == nil || fieldOf(t, err) != "tasks" {
		t.Fatalf("too many power tasks: %v", err)
	}
	if u := TimerUnit(powerTask("rb", TypeReboot)); !strings.Contains(u, "Persistent=false\n") {
		t.Fatalf("power timer is persistent:\n%s", u)
	}
	if u := TimerUnit(okSmart()); !strings.Contains(u, "Persistent=true\n") {
		t.Fatal("smart timer lost Persistent=true")
	}
}

func TestProcUptime(t *testing.T) {
	if d, err := ProcUptime(); err != nil || d <= 0 {
		t.Fatalf("%v %v", d, err)
	}
}
