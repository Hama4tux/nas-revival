package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	stateActive    = "\n/dev/sda:\n drive state is:  active/idle\n"
	stateStandby   = "\n/dev/sda:\n drive state is:  standby\n"
	sysCapsIdle    = `{"smartctl":{"exit_status":0},"ata_smart_data":{"self_test":{"status":{"value":0}},"capabilities":{"self_tests_supported":true}}}`
	sysCapsTesting = `{"smartctl":{"exit_status":0},"ata_smart_data":{"self_test":{"status":{"value":249}},"capabilities":{"self_tests_supported":true}}}`
)

type fakeSys struct {
	state, caps, copyState, backupState string
	failSystemctl                       bool
	calls                               []string
}

func (f *fakeSys) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case call == "hdparm -C /dev/sda":
		return []byte(f.state), nil
	case name == "smartctl":
		return []byte(f.caps), nil
	case call == "systemctl is-active "+copyUnit:
		if f.copyState != "active" {
			return []byte(f.copyState + "\n"), errors.New("exit 3")
		}
		return []byte("active\n"), nil
	case call == "systemctl is-active "+backupUnit:
		if f.backupState != "activating" {
			return []byte("inactive\n"), errors.New("exit 3")
		}
		return []byte("activating\n"), errors.New("exit 3")
	case name == "systemctl" && f.failSystemctl:
		return nil, errors.New("exit 1")
	}
	return nil, nil
}

type sysFixture struct {
	m       *SystemPowerManager
	f       *fakeSys
	synced  int
	delayed []func()
	pending bool
}

func newSysPower(t *testing.T, f *fakeSys, procs ...string) *sysFixture {
	t.Helper()
	proc := t.TempDir()
	for i, c := range procs {
		dir := filepath.Join(proc, strings.Repeat("1", i+1))
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "comm"), []byte(c+"\n"), 0o644)
	}
	fx := &sysFixture{f: f}
	fx.m = &SystemPowerManager{
		Disks: func() ([]PowerDisk, error) {
			return []PowerDisk{{ID: "wwn-0x1", Name: "sda", Capable: true}, {ID: "usb-stick", Name: "sdb"}}, nil
		},
		Run:        f.run,
		ProcRoot:   proc,
		NetPending: func() bool { return fx.pending },
		Sync:       func() { fx.synced++ },
		After:      func(_ time.Duration, fn func()) { fx.delayed = append(fx.delayed, fn) },
	}
	return fx
}

func codes(b []PowerBlocker) string {
	var s []string
	for _, x := range b {
		s = append(s, x.Code+":"+x.DiskID)
	}
	return strings.Join(s, ",")
}

func TestPreflightClean(t *testing.T) {
	fx := newSysPower(t, &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "inactive"}, "bash", "sshd")
	if got := fx.m.Preflight(context.Background()); len(got.Blockers) != 0 {
		t.Fatalf("blockers %v", codes(got.Blockers))
	}
}

func TestPreflightFindsEveryBlocker(t *testing.T) {
	fx := newSysPower(t, &fakeSys{state: stateActive, caps: sysCapsTesting, copyState: "active", backupState: "activating"}, "bash", "dpkg")
	fx.pending = true
	got := codes(fx.m.Preflight(context.Background()).Blockers)
	want := "smart_test:wwn-0x1,copy_running:,backup_running:,package_manager:,network_pending:"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestPreflightDoesNotWakeSleepingDisk(t *testing.T) {
	f := &fakeSys{state: stateStandby, caps: sysCapsTesting, copyState: "inactive"}
	fx := newSysPower(t, f)
	if got := fx.m.Preflight(context.Background()); len(got.Blockers) != 0 {
		t.Fatalf("blockers %v", codes(got.Blockers))
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "smartctl") {
			t.Fatalf("smartctl called on a sleeping disk: %q", f.calls)
		}
	}
}

func TestPreflightReportsFailedCheck(t *testing.T) {
	fx := newSysPower(t, &fakeSys{state: stateActive, caps: "not json", copyState: "inactive"})
	if got := codes(fx.m.Preflight(context.Background()).Blockers); got != "check_failed:wwn-0x1" {
		t.Fatalf("got %s", got)
	}
}

func TestDoRefusesWhenBlockedWithoutForce(t *testing.T) {
	fx := newSysPower(t, &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "active"})
	_, err := fx.m.Do(context.Background(), "reboot", false, "admin")
	if e, ok := AsError(err); !ok || e.Code != CodeBlocked {
		t.Fatalf("err %v", err)
	}
	if fx.synced != 0 || len(fx.delayed) != 0 {
		t.Fatal("must not sync or schedule when blocked")
	}
}

func TestDoForcedRebootSyncsThenSchedules(t *testing.T) {
	f := &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "active"}
	fx := newSysPower(t, f)
	r, err := fx.m.Do(context.Background(), "reboot", true, "admin")
	if err != nil || r.Action != "reboot" || !r.Forced || codes(r.Blockers) != "copy_running:" {
		t.Fatalf("%+v %v", r, err)
	}
	if fx.synced != 1 || len(fx.delayed) != 1 {
		t.Fatalf("synced %d delayed %d", fx.synced, len(fx.delayed))
	}
	fx.delayed[0]()
	if last := f.calls[len(f.calls)-1]; last != "systemctl reboot" {
		t.Fatalf("last call %q", last)
	}
	if _, err := fx.m.Do(context.Background(), "shutdown", true, "admin"); err == nil {
		t.Fatal("second power action accepted")
	} else if e, _ := AsError(err); e.Code != CodePowerPending {
		t.Fatalf("err %v", err)
	}
}

func TestDoShutdownUsesPoweroff(t *testing.T) {
	f := &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "inactive"}
	fx := newSysPower(t, f)
	r, err := fx.m.Do(context.Background(), "shutdown", false, "admin")
	if err != nil || r.Forced {
		t.Fatalf("%+v %v", r, err)
	}
	fx.delayed[0]()
	if last := f.calls[len(f.calls)-1]; last != "systemctl poweroff" {
		t.Fatalf("last call %q", last)
	}
}

func TestDoFailedSystemctlAllowsRetry(t *testing.T) {
	f := &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "inactive", failSystemctl: true}
	fx := newSysPower(t, f)
	if _, err := fx.m.Do(context.Background(), "reboot", false, "admin"); err != nil {
		t.Fatal(err)
	}
	fx.delayed[0]()
	if _, err := fx.m.Do(context.Background(), "reboot", false, "admin"); err != nil {
		t.Fatalf("retry refused: %v", err)
	}
}

func TestDoRejectsUnknownAction(t *testing.T) {
	fx := newSysPower(t, &fakeSys{})
	if _, err := fx.m.Do(context.Background(), "halt", true, "admin"); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestDispatcherPowerOps(t *testing.T) {
	f := &fakeSys{state: stateActive, caps: sysCapsIdle, copyState: "inactive"}
	fx := newSysPower(t, f)
	d := &Dispatcher{SysPower: fx.m}
	if _, err := d.Handle(context.Background(), request{Op: "power.preflight"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "power.reboot", Args: []byte(`{"force":false,"path":"/x"}`)}); err == nil {
		t.Fatal("unknown argument accepted")
	}
	res, err := d.Handle(context.Background(), request{Op: "power.shutdown", Args: []byte(`{"force":false,"user":"admin"}`)})
	if err != nil || res.(PowerActionResult).Action != "shutdown" {
		t.Fatalf("%v %v", res, err)
	}
	if _, err := (&Dispatcher{}).Handle(context.Background(), request{Op: "power.reboot"}); err == nil {
		t.Fatal("power op without manager accepted")
	}
}

func TestPreflightHeavyLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "nas-heavy.lock")
	fx := newSysPower(t, &fakeSys{state: stateStandby, copyState: "inactive"})
	fx.m.HeavyLock = lock
	if got := codes(fx.m.Preflight(context.Background()).Blockers); got != "" {
		t.Fatalf("missing lock file must not block, got %s", got)
	}
	os.WriteFile(lock, nil, 0o660)
	if got := codes(fx.m.Preflight(context.Background()).Blockers); got != "" {
		t.Fatalf("free lock must not block, got %s", got)
	}
	f, _ := os.Open(lock)
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if got := codes(fx.m.Preflight(context.Background()).Blockers); got != "task_running:" {
		t.Fatalf("held lock: got %s", got)
	}
}

func TestPreflightBackupUnitHidesLockBlocker(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "nas-heavy.lock")
	os.WriteFile(lock, nil, 0o660)
	f, _ := os.Open(lock)
	defer f.Close()
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	fx := newSysPower(t, &fakeSys{state: stateStandby, copyState: "inactive", backupState: "activating"})
	fx.m.HeavyLock = lock
	if got := codes(fx.m.Preflight(context.Background()).Blockers); got != "backup_running:" {
		t.Fatalf("got %s", got)
	}
}
