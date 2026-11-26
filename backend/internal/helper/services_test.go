package helper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSvc struct {
	show    map[string]string // unit → systemctl show block
	ss      string
	journal string
	failAct bool
	calls   []string
}

func (f *fakeSvc) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case name == "systemctl" && args[0] == "show":
		var b strings.Builder
		for _, u := range args[4:] {
			if s, ok := f.show[u]; ok {
				b.WriteString(s)
			} else {
				b.WriteString("Id=" + u + "\nLoadState=not-found\nActiveState=inactive\n")
			}
			b.WriteString("\n")
		}
		return []byte(b.String()), nil
	case name == "systemctl" && f.failAct:
		return nil, errors.New("exit 1")
	case name == "ss":
		return []byte(f.ss), nil
	case name == "journalctl":
		return []byte(f.journal), nil
	}
	return nil, nil
}

func showBlock(id, active, fileState string, pid int, cpu string, canReload bool) string {
	r := "no"
	if canReload {
		r = "yes"
	}
	return "Id=" + id + "\nDescription=" + id + " daemon\nLoadState=loaded\nActiveState=" + active + "\nSubState=running\n" +
		"UnitFileState=" + fileState + "\nType=simple\nResult=success\nExecMainStatus=0\nMainPID=" + itoa(pid) +
		"\nCPUUsageNSec=" + cpu + "\nControlGroup=/system.slice/" + id + "\nCanReload=" + r + "\nStateChangeTimestamp=@1795621304\n"
}

type svcFixture struct {
	m   *ServiceManager
	f   *fakeSvc
	now time.Time
}

func newServices(t *testing.T) *svcFixture {
	t.Helper()
	proc, cg := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ssh: main process 220 and a child in a sub-cgroup.
	write(filepath.Join(cg, "system.slice/ssh.service/cgroup.procs"), "220\n")
	write(filepath.Join(cg, "system.slice/ssh.service/sub/cgroup.procs"), "1425\n")
	write(filepath.Join(proc, "220/statm"), "1000 300 100 1 0 50 0\n")
	write(filepath.Join(proc, "1425/statm"), "1000 200 100 1 0 50 0\n")
	write(filepath.Join(cg, "system.slice/cron.service/cgroup.procs"), "181\n")
	write(filepath.Join(proc, "181/statm"), "500 100 50 1 0 20 0\n")
	fx := &svcFixture{f: &fakeSvc{
		show: map[string]string{
			"ssh.service":  showBlock("ssh.service", "active", "enabled", 220, "1000000000", true),
			"cron.service": showBlock("cron.service", "active", "enabled", 181, "500000000", false),
		},
		ss: `tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=220,fd=6))
tcp LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=220,fd=7))
tcp LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:* users:(("nas-revival",pid=999,fd=3))
`,
	}, now: time.Unix(1795621400, 0)}
	fx.m = &ServiceManager{Run: fx.f.run, ProcRoot: proc, CgroupRoot: cg, NumCPU: 1, PageSize: 4096,
		Now: func() time.Time { return fx.now }}
	return fx
}

func find(l ServiceList, name string) Service {
	for _, s := range l.Services {
		if s.Name == name {
			return s
		}
	}
	return Service{}
}

func TestServiceListReportsCatalogWithResources(t *testing.T) {
	fx := newServices(t)
	l, err := fx.m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Services) < 10 || l.Services[0].Name != "smbd.service" {
		t.Fatalf("catalog order: %+v", l.Services)
	}
	smb := find(l, "smbd.service")
	if smb.State != "not-installed" || len(smb.Actions) != 0 || smb.MemoryBytes != nil {
		t.Errorf("smbd = %+v", smb)
	}
	ssh := find(l, "ssh.service")
	if ssh.State != "active" || ssh.PID != 220 || !ssh.Protected || ssh.Enabled != "enabled" {
		t.Errorf("ssh = %+v", ssh)
	}
	if ssh.MemoryBytes == nil || *ssh.MemoryBytes != 500*4096 {
		t.Errorf("ssh rss = %v", ssh.MemoryBytes)
	}
	if len(ssh.Ports) != 2 || ssh.Ports[0].Port != 22 || ssh.Ports[1].Address != "::" {
		t.Errorf("ssh ports = %+v", ssh.Ports)
	}
	if strings.Join(ssh.Actions, ",") != "restart,reload" {
		t.Errorf("ssh actions = %v", ssh.Actions)
	}
	if ssh.CPUPercent != nil {
		t.Errorf("first list has no CPU %%: %v", *ssh.CPUPercent)
	}
	if c := find(l, "cron.service"); len(c.Ports) != 0 || c.Ports == nil {
		t.Errorf("cron ports must be an empty list: %#v", c.Ports)
	}
	for _, c := range fx.f.calls {
		if strings.HasPrefix(c, "systemctl show") && !strings.Contains(c, " -- smbd.service ") {
			t.Errorf("unit names must follow --: %s", c)
		}
	}
}

func TestServiceCPUPercentFromSamples(t *testing.T) {
	fx := newServices(t)
	fx.m.List(context.Background())
	// 0.5 s of CPU in 5 s = 10 %.
	fx.f.show["ssh.service"] = showBlock("ssh.service", "active", "enabled", 220, "1500000000", true)
	fx.now = fx.now.Add(5 * time.Second)
	l, _ := fx.m.List(context.Background())
	if p := find(l, "ssh.service").CPUPercent; p == nil || *p != 10 {
		t.Fatalf("cpu = %v", p)
	}
	// A list within one second keeps the last value.
	fx.f.show["ssh.service"] = showBlock("ssh.service", "active", "enabled", 220, "1900000000", true)
	fx.now = fx.now.Add(200 * time.Millisecond)
	l, _ = fx.m.List(context.Background())
	if p := find(l, "ssh.service").CPUPercent; p == nil || *p != 10 {
		t.Fatalf("quick list cpu = %v", p)
	}
	// A restart resets the counter: no value rather than a negative one.
	fx.f.show["ssh.service"] = showBlock("ssh.service", "active", "enabled", 220, "100", true)
	fx.now = fx.now.Add(5 * time.Second)
	l, _ = fx.m.List(context.Background())
	if p := find(l, "ssh.service").CPUPercent; p != nil {
		t.Fatalf("after restart cpu = %v", *p)
	}
}

func TestServiceActionAllowlist(t *testing.T) {
	fx := newServices(t)
	ctx := context.Background()
	cases := []struct{ name, action, code string }{
		{"ssh.service", "stop", CodeInvalid},
		{"ssh.service", "disable", CodeInvalid},
		{"nas-revival.service", "restart", CodeInvalid},
		{"systemd-networkd.service", "restart", CodeInvalid},
		{"getty@tty1.service", "stop", CodeNotFound},
		{"../../etc/passwd", "start", CodeNotFound},
		{"cron.service", "kill", CodeInvalid},
		{"cron.service", "reload", CodeInvalid}, // CanReload=no
		{"smbd.service", "start", CodeNotFound}, // not installed
	}
	for _, c := range cases {
		fx.f.calls = nil
		if _, err := fx.m.Action(ctx, c.name, c.action, "jana"); codeOf(err) != c.code {
			t.Errorf("%s %s: %v", c.name, c.action, err)
		}
		for _, call := range fx.f.calls {
			if strings.HasPrefix(call, "systemctl "+c.action) {
				t.Errorf("%s %s ran %q", c.name, c.action, call)
			}
		}
	}
}

func TestServiceActionRunsSystemctl(t *testing.T) {
	fx := newServices(t)
	s, err := fx.m.Action(context.Background(), "cron.service", "stop", "jana")
	if err != nil {
		t.Fatal(err)
	}
	if fx.f.calls[1] != "systemctl stop -- cron.service" {
		t.Fatalf("calls = %q", fx.f.calls)
	}
	if s.Name != "cron.service" || s.Label == "" {
		t.Fatalf("result = %+v", s)
	}
	if _, err := fx.m.Action(context.Background(), "ssh.service", "reload", "jana"); err != nil {
		t.Fatalf("ssh reload: %v", err)
	}
	fx.f.failAct = true
	if _, err := fx.m.Action(context.Background(), "cron.service", "start", "jana"); codeOf(err) != CodeInternal {
		t.Fatalf("failed start: %v", err)
	}
}

func TestServiceLog(t *testing.T) {
	fx := newServices(t)
	fx.f.journal = `{"__REALTIME_TIMESTAMP":"1795621304000000","PRIORITY":"3","MESSAGE":"error: bad config"}` + "\n"
	l, err := fx.m.Log(context.Background(), "ssh.service", 50)
	if err != nil || len(l.Entries) != 1 || l.Entries[0].Priority != 3 {
		t.Fatalf("log = %+v, %v", l, err)
	}
	want := "journalctl --unit=ssh.service --lines=50 --no-pager --quiet --output=json --output-fields=PRIORITY,MESSAGE,SYSLOG_IDENTIFIER,_PID"
	if fx.f.calls[0] != want {
		t.Fatalf("call = %q", fx.f.calls[0])
	}
	for _, n := range []int{0, 201, -1} {
		if _, err := fx.m.Log(context.Background(), "ssh.service", n); codeOf(err) != CodeInvalid {
			t.Errorf("lines %d: %v", n, err)
		}
	}
	if _, err := fx.m.Log(context.Background(), "-k", 10); codeOf(err) != CodeNotFound {
		t.Errorf("unknown unit: %v", err)
	}
}

func TestDispatcherServiceOps(t *testing.T) {
	fx := newServices(t)
	d := &Dispatcher{Services: fx.m}
	ctx := context.Background()
	if _, err := d.Handle(ctx, request{Op: "service.list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "service.action", Args: json.RawMessage(`{"name":"cron.service","action":"restart"}`), User: "jana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "journal.unit", Args: json.RawMessage(`{"name":"cron.service","lines":10}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "service.action", Args: json.RawMessage(`{"name":"cron.service","action":"stop","unit":"x"}`)}); codeOf(err) != CodeInvalid {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := (&Dispatcher{}).Handle(ctx, request{Op: "service.list"}); codeOf(err) != CodeInvalid {
		t.Fatalf("disabled manager: %v", err)
	}
}
