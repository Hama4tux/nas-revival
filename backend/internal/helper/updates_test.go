package helper

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/updates"
)

type fakeUpd struct {
	active map[string]string // unit → state
	calls  []string
}

func (f *fakeUpd) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if len(args) == 2 && args[0] == "is-active" {
		if s, ok := f.active[args[1]]; ok {
			return []byte(s + "\n"), nil
		}
		return []byte("inactive\n"), errors.New("exit 3")
	}
	return nil, nil
}

var updBoot = time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)

func newUpd(t *testing.T, st updates.State) (*UpdateManager, *fakeUpd) {
	t.Helper()
	dir := t.TempDir()
	if err := updates.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	f := &fakeUpd{active: map[string]string{}}
	return &UpdateManager{Dir: dir, Run: f.run,
		Free:     func() (uint64, error) { return 1 << 30, nil },
		Memory:   func() (uint64, error) { return 512 << 20, nil },
		BootTime: func() (time.Time, error) { return updBoot, nil }}, f
}

func listed() updates.State {
	at := time.Date(2026, 11, 27, 12, 0, 0, 123456789, time.UTC)
	return updates.State{ListedAt: &at, CheckedAt: &at, DownloadBytes: 10 << 20,
		Packages: []updates.Package{{Name: "openssl", From: "1", To: "2", Security: true}}}
}

const listedAt = "2026-11-27T12:00:00.123456789Z"

func TestUpdatesUpgradeStartsJob(t *testing.T) {
	m, f := newUpd(t, listed())
	if _, err := m.Upgrade(context.Background(), listedAt); err != nil {
		t.Fatal(err)
	}
	if !contains(f.calls, "systemctl start --no-block -- nas-revival-updates@upgrade.service") {
		t.Errorf("calls %v", f.calls)
	}
}

func TestUpdatesUpgradeRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *UpdateManager, f *fakeUpd)
		at    string
		code  string
	}{
		{"stale list", nil, "2026-11-27T12:00:00Z", CodeUpdatesStale},
		{"bad time", nil, "yesterday", CodeInvalid},
		{"check running", func(_ *UpdateManager, f *fakeUpd) { f.active["nas-revival-updates@check.service"] = "activating" }, listedAt, CodeUpdatesRunning},
		{"upgrade running", func(_ *UpdateManager, f *fakeUpd) { f.active["nas-revival-updates@upgrade.service"] = "activating" }, listedAt, CodeUpdatesRunning},
		// 300 MiB + 3 × 10 MiB are needed.
		{"disk full", func(m *UpdateManager, _ *fakeUpd) { m.Free = func() (uint64, error) { return 329 << 20, nil } }, listedAt, CodeUpdatesSpace},
		{"low memory", func(m *UpdateManager, _ *fakeUpd) { m.Memory = func() (uint64, error) { return 100 << 20, nil } }, listedAt, CodeUpdatesSpace},
	}
	for _, c := range cases {
		m, f := newUpd(t, listed())
		if c.setup != nil {
			c.setup(m, f)
		}
		_, err := m.Upgrade(context.Background(), c.at)
		if code(err) != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
		for _, call := range f.calls {
			if strings.HasPrefix(call, "systemctl start") {
				t.Errorf("%s: job started", c.name)
			}
		}
	}
	empty := listed()
	empty.Packages = nil
	m, _ := newUpd(t, empty)
	if _, err := m.Upgrade(context.Background(), listedAt); code(err) != CodeInvalid {
		t.Errorf("nothing to install: %v", err)
	}
}

func TestUpdatesCheck(t *testing.T) {
	m, f := newUpd(t, updates.State{})
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !contains(f.calls, "systemctl start --no-block -- nas-revival-updates@check.service") {
		t.Errorf("calls %v", f.calls)
	}
	f.active["nas-revival-updates@upgrade.service"] = "active"
	if _, err := m.Check(context.Background()); code(err) != CodeUpdatesRunning {
		t.Errorf("during upgrade: %v", err)
	}
}

func TestUpdatesGet(t *testing.T) {
	st := listed()
	st.Upgrade = &updates.Upgrade{Result: updates.ResultRunning, Total: 1}
	boot := updBoot
	st.RestartSince, st.RestartPackages = &boot, []string{"libc6"}
	m, f := newUpd(t, st)
	got, err := m.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The job is not running any more: the run was interrupted.
	if got.Running != "" || got.Upgrade.Result != updates.ResultFailed || !strings.Contains(got.Upgrade.Message, "přerušena") {
		t.Errorf("interrupted: %+v %+v", got, got.Upgrade)
	}
	if !got.RestartRecommended || got.FreeBytes != 1<<30 {
		t.Errorf("restart/free: %+v", got)
	}
	f.active["nas-revival-updates@upgrade.service"] = "activating"
	if got, _ := m.Get(context.Background()); got.Running != updates.JobUpgrade || got.Upgrade.Result != updates.ResultRunning {
		t.Errorf("running: %+v", got)
	}
	// After a reboot the old restart reasons no longer apply.
	m.BootTime = func() (time.Time, error) { return updBoot.Add(time.Hour), nil }
	if got, _ := m.Get(context.Background()); got.RestartRecommended || len(got.RestartPackages) != 0 {
		t.Errorf("after reboot: %+v", got)
	}
	m.RebootRequired = func() bool { return true }
	if got, _ := m.Get(context.Background()); !got.RestartRecommended {
		t.Error("reboot-required ignored")
	}
}
