package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/firewall"
)

type fwFixture struct {
	m       *FirewallManager
	now     time.Time
	calls   []string
	scripts []string // content of the script file at every `nft -f <state script>`
	fire    func()
	fail    string // command prefix that fails
	noNft   bool
	ss      string
	ssErr   error
}

func newFwFixture(t *testing.T) *fwFixture {
	t.Helper()
	dir := t.TempDir()
	f := &fwFixture{now: time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)}
	f.m = NewFirewallManager(FirewallPaths{ConfigFile: filepath.Join(dir, "firewall.json"),
		BootFile: filepath.Join(dir, "nftables.d", "nas-revival.nft"), StateDir: filepath.Join(dir, "state")},
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, line)
			if f.noNft && strings.HasPrefix(line, "nft list") {
				return nil, os.ErrInvalid
			}
			if f.fail != "" && strings.HasPrefix(line, f.fail) {
				return nil, os.ErrInvalid
			}
			if len(args) > 1 && args[len(args)-2] == "-f" && args[len(args)-1] == f.m.scriptPath() {
				b, _ := os.ReadFile(f.m.scriptPath())
				f.scripts = append(f.scripts, string(b))
			}
			if strings.HasPrefix(line, "ss ") {
				return []byte(f.ss), f.ssErr
			}
			if line == "nft list tables" {
				return []byte("table ip nas_revival\n"), nil
			}
			return nil, nil
		})
	f.m.Now = func() time.Time { return f.now }
	f.m.After = func(d time.Duration, fn func()) func() bool {
		f.fire = fn
		return func() bool { return true }
	}
	return f
}

func fwSettings() firewall.Settings {
	lan := []string{"192.0.2.0/24"}
	return firewall.Settings{Enabled: true, Allow: map[string][]string{"web": lan, "ssh": lan}}
}

const fwClient = "192.0.2.5"

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestFirewallApplyKernelOnly(t *testing.T) {
	f := newFwFixture(t)
	st, err := f.m.Apply(context.Background(), fwSettings(), fwClient)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Supported || !st.Active || st.Pending == nil || !st.Pending.Deadline.Equal(f.now.Add(180*time.Second)) || !st.Pending.Next.Enabled {
		t.Fatalf("%+v", st)
	}
	if st.Settings.Enabled {
		t.Fatal("settings saved before confirm")
	}
	if exists(f.m.Paths.BootFile) || exists(f.m.Paths.ConfigFile) {
		t.Fatal("boot file or json written before confirm")
	}
	if !exists(f.m.markerPath()) {
		t.Fatal("marker missing")
	}
	if len(f.calls) < 3 || f.calls[1] != "nft -c -f "+f.m.scriptPath() || f.calls[2] != "nft -f "+f.m.scriptPath() {
		t.Fatal(f.calls)
	}
	want := firewall.Render(st.Pending.Next)
	if len(f.scripts) != 2 || f.scripts[1] != want || !strings.Contains(want, "tcp dport 8443") {
		t.Fatalf("%q", f.scripts)
	}
	if _, err := f.m.Apply(context.Background(), fwSettings(), fwClient); err == nil || err.(*Error).Code != CodeChangePending {
		t.Fatalf("%v", err)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"deadline"`) {
		t.Fatal(string(b))
	}
}

func TestFirewallConfirmWritesFiles(t *testing.T) {
	f := newFwFixture(t)
	if _, err := f.m.Apply(context.Background(), fwSettings(), fwClient); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(60 * time.Second)
	st, err := f.m.Confirm(context.Background())
	if err != nil || st.Pending != nil || !st.Settings.Enabled {
		t.Fatalf("%+v %v", st, err)
	}
	boot, err := os.ReadFile(f.m.Paths.BootFile)
	if err != nil || string(boot) != firewall.Render(st.Settings) {
		t.Fatalf("boot: %q %v", boot, err)
	}
	if fi, _ := os.Stat(f.m.Paths.BootFile); fi.Mode().Perm() != 0o644 {
		t.Fatal(fi.Mode())
	}
	if s, err := firewall.Load(f.m.Paths.ConfigFile); err != nil || !s.Enabled {
		t.Fatalf("%+v %v", s, err)
	}
	if exists(f.m.markerPath()) {
		t.Fatal("marker left")
	}
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatalf("%v", err)
	}
	f.calls = nil
	f.fire() // stale timer must not roll back
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestFirewallConfirmDisabledRemovesBootFile(t *testing.T) {
	f := newFwFixture(t)
	f.m.Apply(context.Background(), fwSettings(), fwClient)
	f.m.Confirm(context.Background())
	off := firewall.Default()
	if _, err := f.m.Apply(context.Background(), off, fwClient); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Confirm(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists(f.m.Paths.BootFile) {
		t.Fatal("boot file kept for disabled firewall")
	}
}

func TestFirewallRollbackReloadsBootFile(t *testing.T) {
	f := newFwFixture(t)
	f.m.Apply(context.Background(), fwSettings(), fwClient)
	f.m.Confirm(context.Background())
	next := fwSettings()
	next.Allow["smb"] = []string{"192.0.2.0/24"}
	if _, err := f.m.Apply(context.Background(), next, fwClient); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	f.fire()
	if len(f.calls) != 1 || f.calls[0] != "nft -f "+f.m.Paths.BootFile {
		t.Fatal(f.calls)
	}
	if f.m.HasPending() || exists(f.m.markerPath()) {
		t.Fatal("still pending")
	}
	if s, _ := firewall.Load(f.m.Paths.ConfigFile); len(s.Allow["smb"]) != 0 {
		t.Fatal("unconfirmed change saved")
	}
}

func TestFirewallRollbackWithoutBootFileDeletesTable(t *testing.T) {
	f := newFwFixture(t)
	f.m.Apply(context.Background(), fwSettings(), fwClient)
	f.scripts = nil
	f.fire()
	if len(f.scripts) != 1 || f.scripts[0] != "table ip nas_revival {}\ndelete table ip nas_revival\n" {
		t.Fatalf("%q", f.scripts)
	}
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatalf("%v", err)
	}
}

func TestFirewallConfirmAfterDeadlineRollsBack(t *testing.T) {
	f := newFwFixture(t)
	f.m.Apply(context.Background(), fwSettings(), fwClient)
	f.now = f.now.Add(181 * time.Second)
	if _, err := f.m.Confirm(context.Background()); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatalf("%v", err)
	}
	if exists(f.m.Paths.BootFile) || f.m.HasPending() {
		t.Fatal("late confirm accepted")
	}
}

func TestFirewallCheckFailureLeavesNoState(t *testing.T) {
	f := newFwFixture(t)
	f.fail = "nft -c"
	_, err := f.m.Apply(context.Background(), fwSettings(), fwClient)
	if err == nil || err.(*Error).Code != CodeInternal {
		t.Fatalf("%v", err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "nft -f") {
			t.Fatal("loaded after failed check")
		}
	}
	if f.m.HasPending() || exists(f.m.markerPath()) || exists(f.m.scriptPath()) {
		t.Fatal("state left")
	}
}

func TestFirewallLoadFailureRollsBack(t *testing.T) {
	f := newFwFixture(t)
	f.fail = "nft -f " + f.m.scriptPath()
	if _, err := f.m.Apply(context.Background(), fwSettings(), fwClient); err == nil {
		t.Fatal("expected error")
	}
	if f.m.HasPending() || exists(f.m.markerPath()) {
		t.Fatal("state left")
	}
}

func TestFirewallRecoverRollsBackImmediately(t *testing.T) {
	f := newFwFixture(t)
	f.m.Apply(context.Background(), fwSettings(), fwClient)
	// A new manager over the same files = helper restart inside the window.
	g := newFwFixture(t)
	g.m.Paths = f.m.Paths
	g.m.Recover(context.Background())
	if g.fire != nil {
		t.Fatal("timer re-armed")
	}
	if len(g.scripts) != 1 || !strings.Contains(g.scripts[0], "delete table ip nas_revival") {
		t.Fatalf("%q %v", g.scripts, g.calls)
	}
	if exists(g.m.markerPath()) || g.m.HasPending() {
		t.Fatal("marker left")
	}
	g.calls = nil
	g.m.Recover(context.Background()) // no marker: nothing to do
	if len(g.calls) != 0 {
		t.Fatal(g.calls)
	}
}

func TestFirewallUnsupported(t *testing.T) {
	f := newFwFixture(t)
	f.noNft = true
	st, err := f.m.Get(context.Background())
	if err != nil || st.Supported || st.Active {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := f.m.Apply(context.Background(), fwSettings(), fwClient); err == nil || err.(*Error).Code != CodeNotInstalled {
		t.Fatalf("%v", err)
	}
}

func TestFirewallApplyValidatesAgain(t *testing.T) {
	f := newFwFixture(t)
	if _, err := f.m.Apply(context.Background(), fwSettings(), "203.0.113.9"); err == nil || err.(*Error).Code != CodeInvalid || err.(*Error).Field != "allow.web" {
		t.Fatalf("%v", err)
	}
	if _, err := f.m.Apply(context.Background(), fwSettings(), ""); err == nil || err.(*Error).Code != CodeInvalid {
		t.Fatalf("%v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("nft called for invalid input", f.calls)
	}
}

func TestFirewallDispatch(t *testing.T) {
	f := newFwFixture(t)
	d := &Dispatcher{Firewall: f.m}
	args, _ := json.Marshal(map[string]any{"settings": fwSettings(), "clientAddr": fwClient})
	res, err := d.Handle(context.Background(), request{Op: "firewall.apply", Args: args})
	if err != nil || res.(FirewallState).Pending == nil {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "firewall.confirm"}); err != nil {
		t.Fatal(err)
	}
	res, err = d.Handle(context.Background(), request{Op: "firewall.get"})
	if err != nil || !res.(FirewallState).Settings.Enabled {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "firewall.apply", Args: json.RawMessage(`{"bogus":1}`)}); err == nil {
		t.Fatal("unknown arg accepted")
	}
}

func TestFirewallListening(t *testing.T) {
	f := newFwFixture(t)
	f.ss = `tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=220,fd=6))
tcp LISTEN 0 128 [::]:22 [::]:*
tcp LISTEN 0 50 *:445 *:* users:(("smbd",pid=5,fd=3))
tcp LISTEN 0 50 127.0.0.1:631 0.0.0.0:*
`
	ctx := context.Background()
	st, err := f.m.Get(ctx)
	if err != nil || len(st.Listening) != 2 || st.Listening[0].Process != "sshd" || st.Listening[0].Allowed || st.Listening[1].Port != 445 {
		t.Fatalf("%+v %v", st.Listening, err)
	}
	// A pending change decides the verdict.
	in := fwSettings()
	in.Rules = []firewall.Rule{{Source: "192.0.2.0/24", Ports: "445", Proto: "tcp", Action: "accept"}}
	st, err = f.m.Apply(ctx, in, fwClient)
	if err != nil || st.Pending == nil || !st.Listening[0].Allowed || !st.Listening[1].Allowed {
		t.Fatalf("%+v %v", st.Listening, err)
	}
	// A failing ss never fails the whole get.
	f.ssErr = os.ErrInvalid
	st, err = f.m.Get(ctx)
	if err != nil || st.Listening == nil || len(st.Listening) != 0 {
		t.Fatalf("%#v %v", st.Listening, err)
	}
}
