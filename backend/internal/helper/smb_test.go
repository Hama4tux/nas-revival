package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/smb"
)

type fakeSmbd struct {
	active       map[string]bool
	rejectParm   bool
	status       string
	calls        []string
	parmSawInclu bool
}

func (f *fakeSmbd) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	switch {
	case name == "systemctl" && args[0] == "is-active":
		if f.active[args[2]] {
			return nil, nil
		}
		return nil, errors.New("exit 3")
	case name == "testparm":
		b, _ := os.ReadFile(args[1])
		f.parmSawInclu = strings.Contains(string(b), "include = ")
		f.calls = append(f.calls, "testparm")
		if f.rejectParm {
			return []byte("bad"), errors.New("exit 1")
		}
		return nil, nil
	case name == "smbstatus":
		f.calls = append(f.calls, call)
		return []byte(f.status), nil
	}
	f.calls = append(f.calls, call)
	if name == "systemctl" && (args[0] == "enable" || args[0] == "restart") {
		f.active[args[len(args)-1]] = true
	}
	if name == "systemctl" && args[0] == "disable" {
		for _, u := range args[2:] {
			f.active[u] = false
		}
	}
	return nil, nil
}

func newSmbFixture(t *testing.T, installed bool) (*SmbManager, *fakeSmbd) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeSmbd{active: map[string]bool{}}
	m := &SmbManager{Path: filepath.Join(dir, "smb.json"), Conf: filepath.Join(dir, "smb.conf"),
		Fragment: filepath.Join(dir, "frag.conf"), Run: f.run,
		LookPath: func(string) (string, error) {
			if installed {
				return "/usr/sbin/smbd", nil
			}
			return "", errors.New("not found")
		},
		SharePaths: func() map[string]string { return map[string]string{"/srv/data/docs": "docs"} }}
	os.WriteFile(m.Conf, []byte("# debian default\n[homes]\n"), 0o644)
	return m, f
}

func settings(enabled, netbios bool) smb.Settings {
	s := smb.Default()
	s.Enabled, s.NetBIOS = enabled, netbios
	return s
}

func TestSmbGetUnconfigured(t *testing.T) {
	m, _ := newSmbFixture(t, true)
	st, err := m.Get(context.Background())
	if err != nil || !st.Installed || st.Settings.Enabled || st.Managed || st.Running {
		t.Fatalf("got %+v, %v", st, err)
	}
}

func TestSmbEnableWritesConfigAndStarts(t *testing.T) {
	m, f := newSmbFixture(t, true)
	st, err := m.Set(context.Background(), settings(true, false), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Managed || !st.Running || st.NetbiosRunning || !st.Settings.Enabled {
		t.Fatalf("state %+v", st)
	}
	want := []string{"testparm", "systemctl enable --now smbd.service", "systemctl disable --now nmbd.service"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v, want %v", f.calls, want)
	}
	if !f.parmSawInclu {
		t.Fatal("testparm did not check the full config")
	}
	if _, err := os.Stat(m.Fragment); err != nil {
		t.Fatal("share fragment not created for the include")
	}
	b, _ := os.ReadFile(m.Conf)
	if strings.Contains(string(b), "[homes]") {
		t.Fatal("Debian default left in place")
	}
}

func TestSmbRejectedConfigChangesNothing(t *testing.T) {
	m, f := newSmbFixture(t, true)
	f.rejectParm = true
	if _, err := m.Set(context.Background(), settings(true, false), "admin"); codeOf(err) != CodeInternal {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(m.Path); err == nil {
		t.Fatal("smb.json written")
	}
	b, _ := os.ReadFile(m.Conf)
	if !strings.Contains(string(b), "debian default") {
		t.Fatal("smb.conf overwritten")
	}
	if len(f.calls) != 1 {
		t.Fatalf("daemons touched: %v", f.calls)
	}
}

func TestSmbReloadRestartAndDisable(t *testing.T) {
	ctx := context.Background()
	m, f := newSmbFixture(t, true)
	m.Set(ctx, settings(true, false), "admin")
	// A running smbd reloads on a plain change …
	f.calls = nil
	s := settings(true, false)
	s.Workgroup = "HOME"
	if _, err := m.Set(ctx, s, "admin"); err != nil {
		t.Fatal(err)
	}
	if !contains(f.calls, "smbcontrol smbd reload-config") || contains(f.calls, "systemctl restart smbd.service") {
		t.Fatalf("plain change: %v", f.calls)
	}
	// … and restarts when the ports change (NetBIOS).
	f.calls = nil
	st, err := m.Set(ctx, settings(true, true), "admin")
	if err != nil || !contains(f.calls, "systemctl restart smbd.service") || !contains(f.calls, "systemctl enable --now nmbd.service") || !st.NetbiosRunning {
		t.Fatalf("netbios: %v %+v %v", f.calls, st, err)
	}
	f.calls = nil
	st, err = m.Set(ctx, settings(false, true), "admin")
	if err != nil || st.Running || st.NetbiosRunning || !contains(f.calls, "systemctl disable --now smbd.service nmbd.service") {
		t.Fatalf("disable: %v %+v %v", f.calls, st, err)
	}
}

func TestSmbNotInstalled(t *testing.T) {
	m, _ := newSmbFixture(t, false)
	if _, err := m.Set(context.Background(), settings(true, false), "admin"); codeOf(err) != CodeInvalid {
		t.Fatalf("err %v", err)
	}
	st, err := m.Status(context.Background())
	if err != nil || len(st.Sessions) != 0 {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestSmbInvalidSettings(t *testing.T) {
	m, f := newSmbFixture(t, true)
	s := settings(true, false)
	s.MinProtocol = "NT1"
	if _, err := m.Set(context.Background(), s, "admin"); codeOf(err) != CodeInvalid {
		t.Fatalf("err %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestSmbRestoreAbsentTurnsOff(t *testing.T) {
	ctx := context.Background()
	m, f := newSmbFixture(t, true)
	m.Set(ctx, settings(true, false), "admin")
	if err := m.Restore(ctx, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Path); err == nil {
		t.Fatal("smb.json kept")
	}
	if f.active["smbd.service"] {
		t.Fatal("smbd still running")
	}
	if err := m.Restore(ctx, []byte(`{"version":1,"workgroup":"WG","minProtocol":"NT1"}`), true); codeOf(err) != CodeInvalid {
		t.Fatalf("SMB1 restored: %v", err)
	}
}

func TestSmbStatusSkipsNotes(t *testing.T) {
	m, f := newSmbFixture(t, true)
	f.active["smbd.service"] = true
	f.status = "/run/samba/locking.tdb not initialised\nThis is normal.\n" + `{"sessions":{},"tcons":{}}`
	st, err := m.Status(context.Background())
	if err != nil || st.Sessions == nil || st.OpenFiles == nil {
		t.Fatalf("%+v %v", st, err)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
