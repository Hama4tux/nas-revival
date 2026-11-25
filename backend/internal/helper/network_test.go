package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const origNet = "[Match]\nName=e*\n\n[Network]\nDHCP=yes\n"

type fixture struct {
	m     *Manager
	now   time.Time
	calls []string
	fire  func() // fires the armed timer
	armed []time.Duration
	fail  string // command prefix that fails
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sys", "enp1s0"), 0o755)
	f := &fixture{now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	nf := filepath.Join(dir, "20-wired.network")
	os.WriteFile(nf, []byte(origNet), 0o644)
	f.m = NewManager(NetworkPaths{NetworkFile: nf, StateDir: filepath.Join(dir, "state"), SysClassNet: filepath.Join(dir, "sys")}, "enp1s0",
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, line)
			if f.fail != "" && strings.HasPrefix(line, f.fail) {
				return nil, os.ErrInvalid
			}
			switch name {
			case "ip":
				if args[1] == "addr" {
					return []byte(`[{"ifname":"enp1s0","addr_info":[{"family":"inet","local":"192.0.2.51","prefixlen":24,"scope":"global"},{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}]}]`), nil
				}
				return []byte(`[{"dst":"default","gateway":"192.0.2.1","dev":"enp1s0"}]`), nil
			case "resolvectl":
				return []byte("Link 2 (enp1s0): 192.0.2.1\n"), nil
			}
			return nil, nil
		})
	f.m.Now = func() time.Time { return f.now }
	f.m.After = func(d time.Duration, fn func()) func() bool {
		f.armed = append(f.armed, d)
		f.fire = fn
		return func() bool { return true }
	}
	return f
}

func (f *fixture) file(t *testing.T) string {
	b, err := os.ReadFile(f.m.Paths.NetworkFile)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

var newCfg = static("192.0.2.60", 24, "192.0.2.1", "192.0.2.1")

func TestGetStatus(t *testing.T) {
	f := newFixture(t)
	s := f.m.Get(context.Background())
	if s.Interface != "enp1s0" || s.Config.Mode != ModeDHCP || s.Pending != nil ||
		len(s.Current.Addresses) != 1 || s.Current.Addresses[0] != "192.0.2.51/24" ||
		*s.Current.Gateway != "192.0.2.1" || s.Current.DNS[0] != "192.0.2.1" {
		t.Fatalf("%+v", s)
	}
	if b, _ := json.Marshal(s); !strings.Contains(string(b), `"pending":null`) {
		t.Fatal(string(b))
	}
}

func TestApplyConfirm(t *testing.T) {
	f := newFixture(t)
	s, err := f.m.Apply(context.Background(), newCfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pending == nil || !s.Pending.Deadline.Equal(f.now.Add(180*time.Second)) || s.Pending.Previous.Mode != ModeDHCP {
		t.Fatalf("%+v", s.Pending)
	}
	if !strings.Contains(f.file(t), "Address=192.0.2.60/24") {
		t.Fatal(f.file(t))
	}
	want := []string{"networkctl reload", "networkctl reconfigure enp1s0"}
	if f.calls[0] != want[0] || f.calls[1] != want[1] {
		t.Fatal(f.calls)
	}
	if _, err := f.m.Apply(context.Background(), newCfg); err == nil || err.(*Error).Code != CodeChangePending {
		t.Fatalf("%v", err)
	}
	f.now = f.now.Add(60 * time.Second)
	s, err = f.m.Confirm(context.Background())
	if err != nil || s.Pending != nil {
		t.Fatalf("%+v %v", s, err)
	}
	for _, p := range []string{f.m.markerPath(), f.m.backupPath()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left", p)
		}
	}
	if !strings.Contains(f.file(t), "Address=") {
		t.Fatal("config lost")
	}
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatalf("%v", err)
	}
	f.fire() // stale timer after confirm must be a no-op
	if !strings.Contains(f.file(t), "Address=") {
		t.Fatal("stale timer rolled back")
	}
}

func TestRollbackOnTimeout(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Apply(context.Background(), newCfg); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	f.fire()
	if f.file(t) != origNet {
		t.Fatalf("not restored: %q", f.file(t))
	}
	if len(f.calls) != 2 || f.calls[0] != "networkctl reload" {
		t.Fatal(f.calls)
	}
	if s := f.m.Get(context.Background()); s.Pending != nil {
		t.Fatal("still pending")
	}
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatal(err)
	}
}

func TestRollbackWhenNoOriginalFile(t *testing.T) {
	f := newFixture(t)
	os.Remove(f.m.Paths.NetworkFile)
	if _, err := f.m.Apply(context.Background(), newCfg); err != nil {
		t.Fatal(err)
	}
	f.fire()
	if f.file(t) != "<missing>" {
		t.Fatal(f.file(t))
	}
}

func TestApplyFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	f.fail = "networkctl reconfigure"
	_, err := f.m.Apply(context.Background(), newCfg)
	if e, ok := err.(*Error); !ok || e.Code != CodeInternal {
		t.Fatalf("%v", err)
	}
	if f.file(t) != origNet || f.m.pending != nil {
		t.Fatal("not rolled back")
	}
}

func TestApplyRejectsInvalidAndUnknownIface(t *testing.T) {
	f := newFixture(t)
	bad := newCfg
	bad.Gateway = ptr("10.0.0.1")
	if _, err := f.m.Apply(context.Background(), bad); err == nil || err.(*Error).Field != "gateway" {
		t.Fatal(err)
	}
	f.m.Iface = "nope0"
	if _, err := f.m.Apply(context.Background(), newCfg); err == nil {
		t.Fatal("unknown iface accepted")
	}
	f.m.Iface = "en0;ls"
	if _, err := f.m.Apply(context.Background(), newCfg); err == nil {
		t.Fatal("bad iface accepted")
	}
	if len(f.calls) != 0 || f.file(t) != origNet {
		t.Fatal("side effects")
	}
}

func TestConfirmAfterDeadline(t *testing.T) {
	f := newFixture(t)
	f.m.Apply(context.Background(), newCfg)
	f.now = f.now.Add(181 * time.Second)
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatal(err)
	}
	if f.file(t) != origNet {
		t.Fatal("not rolled back")
	}
}

// restart simulates a new helper process sharing the same files.
func (f *fixture) restart(t *testing.T) *fixture {
	g := newFixture(t)
	g.now = f.now
	g.m.Paths = f.m.Paths
	return g
}

func TestRecoverExpiredMarker(t *testing.T) {
	f := newFixture(t)
	f.m.Apply(context.Background(), newCfg)
	g := f.restart(t)
	g.now = f.now.Add(10 * time.Minute)
	g.m.Recover(context.Background())
	if g.file(t) != origNet || g.m.pending != nil {
		t.Fatalf("not rolled back: %q", g.file(t))
	}
	if len(g.calls) != 2 {
		t.Fatal(g.calls)
	}
}

func TestRecoverActiveMarker(t *testing.T) {
	f := newFixture(t)
	f.m.Apply(context.Background(), newCfg)
	g := f.restart(t)
	g.now = f.now.Add(100 * time.Second)
	g.m.Recover(context.Background())
	if g.m.pending == nil || len(g.armed) != 1 || g.armed[0] != 80*time.Second {
		t.Fatalf("armed %v", g.armed)
	}
	if !strings.Contains(g.file(t), "Address=") {
		t.Fatal("changed")
	}
	g.fire()
	if g.file(t) != origNet {
		t.Fatal("timer did not roll back")
	}
}

func TestRecoverNoMarkerAndCorrupt(t *testing.T) {
	f := newFixture(t)
	f.m.Recover(context.Background())
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
	f.m.Apply(context.Background(), newCfg)
	os.WriteFile(f.m.markerPath(), []byte("{nope"), 0o600)
	g := f.restart(t)
	g.m.Recover(context.Background())
	if g.file(t) != origNet {
		t.Fatalf("corrupt marker: %q", g.file(t))
	}
}
