package services

import (
	"reflect"
	"strings"
	"testing"
)

const showSSH = "Id=ssh.service\nDescription=OpenBSD Secure Shell server\nLoadState=loaded\nActiveState=active\nSubState=running\n" +
	"UnitFileState=enabled\nType=notify\nResult=success\nExecMainStatus=0\nMainPID=220\nCPUUsageNSec=363649008000\n" +
	"ControlGroup=/system.slice/ssh.service\nCanReload=yes\nStateChangeTimestamp=@1795621304\n"

func TestParseShow(t *testing.T) {
	raw := showSSH + "\n" +
		"Id=smbd.service\nLoadState=not-found\nActiveState=inactive\nCPUUsageNSec=[not set]\nStateChangeTimestamp=\n\n" +
		"Id=bad.service\nLoadState=loaded\nActiveState=failed\nType=simple\nResult=exit-code\nExecMainStatus=1\nUnitFileState=disabled\n\n" +
		"Id=leds.service\nLoadState=loaded\nActiveState=inactive\nType=oneshot\nResult=success\nUnitFileState=enabled\n"
	u := ParseShow([]byte(raw))
	if len(u) != 4 {
		t.Fatalf("got %d units", len(u))
	}
	s := u[0]
	if s.ID != "ssh.service" || s.MainPID != 220 || s.CPUNSec == nil || *s.CPUNSec != 363649008000 || !s.CanReload ||
		s.Since == nil || s.Since.Unix() != 1795621304 || s.ControlGroup != "/system.slice/ssh.service" {
		t.Fatalf("ssh = %+v", s)
	}
	if u[1].CPUNSec != nil || u[1].Since != nil || u[1].Installed() || u[1].State() != "not-installed" {
		t.Fatalf("smbd = %+v", u[1])
	}
	if u[2].State() != "failed" || u[2].ExecMainStatus != 1 {
		t.Fatalf("bad = %+v", u[2])
	}
	if u[3].State() != "done" {
		t.Fatalf("oneshot = %q", u[3].State())
	}
}

func TestAvailableFollowsCatalogAndState(t *testing.T) {
	ssh, _ := Lookup("ssh.service")
	u := ParseShow([]byte(showSSH))[0]
	if got := u.Available(ssh); !reflect.DeepEqual(got, []string{ActRestart, ActReload}) {
		t.Fatalf("ssh actions = %v", got)
	}
	cron, _ := Lookup("cron.service")
	run := Unit{LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	if got := run.Available(cron); !reflect.DeepEqual(got, []string{ActStop, ActRestart, ActDisable}) {
		t.Fatalf("running cron = %v", got)
	}
	stopped := Unit{LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	if got := stopped.Available(cron); !reflect.DeepEqual(got, []string{ActStart, ActRestart, ActEnable}) {
		t.Fatalf("stopped cron = %v", got)
	}
	static := Unit{LoadState: "loaded", ActiveState: "inactive", UnitFileState: "static"}
	if got := static.Available(cron); !reflect.DeepEqual(got, []string{ActStart, ActRestart}) {
		t.Fatalf("static = %v", got)
	}
	for _, u := range []Unit{{LoadState: "not-found"}, {LoadState: "masked", UnitFileState: "masked"}} {
		if got := u.Available(cron); len(got) != 0 {
			t.Fatalf("%+v actions = %v", u, got)
		}
	}
}

func TestProtectedUnitsCannotStopOrDisable(t *testing.T) {
	for _, n := range []string{"ssh.service", "systemd-networkd.service", "nas-revival.service", "nas-revival-helper.service", "nsa310-fan.service"} {
		d, ok := Lookup(n)
		if !ok || !d.Protected {
			t.Fatalf("%s must be protected", n)
		}
		for _, a := range []string{ActStop, ActDisable, ActStart, ActEnable} {
			if d.Allows(a) {
				t.Errorf("%s allows %s", n, a)
			}
		}
	}
	// The web UI must never restart itself or its helper mid-request.
	for _, n := range []string{"nas-revival.service", "nas-revival-helper.service", "systemd-networkd.service"} {
		if d, _ := Lookup(n); len(d.Actions) != 0 {
			t.Errorf("%s actions %v", n, d.Actions)
		}
	}
	if _, ok := Lookup("../etc/passwd"); ok {
		t.Fatal("lookup accepted a path")
	}
}

func TestParseSS(t *testing.T) {
	raw := `udp UNCONN 0      0         127.0.0.54:53   0.0.0.0:* users:(("systemd-resolve",pid=3265,fd=16))
tcp LISTEN 0      4096         0.0.0.0:8443 0.0.0.0:* users:(("nas-revival",pid=13634,fd=3))
tcp LISTEN 0      128          0.0.0.0:22   0.0.0.0:* users:(("sshd",pid=220,fd=6),("sshd",pid=221,fd=6))
tcp LISTEN 0      4096   127.0.0.53%lo:53   0.0.0.0:* users:(("systemd-resolve",pid=3265,fd=15))
tcp LISTEN 0      50              [::]:445     [::]:*
garbage
`
	l := ParseSS([]byte(raw))
	if len(l) != 5 {
		t.Fatalf("got %+v", l)
	}
	if l[0].Proto != "udp" || l[0].Address != "127.0.0.54" || l[0].Port.Port != 53 || !reflect.DeepEqual(l[0].PIDs, []int{3265}) {
		t.Errorf("0 = %+v", l[0])
	}
	if !reflect.DeepEqual(l[2].PIDs, []int{220, 221}) {
		t.Errorf("ssh pids = %v", l[2].PIDs)
	}
	if l[3].Address != "127.0.0.53" {
		t.Errorf("scope id kept: %q", l[3].Address)
	}
	if l[2].Process != "sshd" || l[0].Process != "systemd-resolve" || l[4].Process != "" {
		t.Errorf("process names: %q %q %q", l[2].Process, l[0].Process, l[4].Process)
	}
	if l[4].Address != "::" || l[4].Port.Port != 445 || len(l[4].PIDs) != 0 {
		t.Errorf("ipv6 = %+v", l[4])
	}
}

func TestParseJournal(t *testing.T) {
	long := strings.Repeat("é", MaxMessage+5)
	raw := `{"__REALTIME_TIMESTAMP":"1795621304000000","PRIORITY":"3","MESSAGE":"boom","SYSLOG_IDENTIFIER":"sshd","_PID":"220"}
{"__REALTIME_TIMESTAMP":"1795621305000000","MESSAGE":[104,105,255]}
not json
{"__REALTIME_TIMESTAMP":"1795621306000000","PRIORITY":"9","MESSAGE":"` + long + `"}
`
	e := ParseJournal([]byte(raw))
	if len(e) != 3 {
		t.Fatalf("got %+v", e)
	}
	if e[0].Priority != 3 || e[0].Ident != "sshd" || e[0].PID != 220 || e[0].Message != "boom" || e[0].Time.Unix() != 1795621304 {
		t.Errorf("0 = %+v", e[0])
	}
	if e[1].Message != "hi�" || e[1].Priority != 6 {
		t.Errorf("binary message = %q prio %d", e[1].Message, e[1].Priority)
	}
	if e[2].Priority != 6 || len([]rune(e[2].Message)) != MaxMessage+1 {
		t.Errorf("long message %d runes prio %d", len([]rune(e[2].Message)), e[2].Priority)
	}
	var b strings.Builder
	for i := 0; i < MaxLogLines+10; i++ {
		b.WriteString(`{"MESSAGE":"x"}` + "\n")
	}
	if n := len(ParseJournal([]byte(b.String()))); n != MaxLogLines {
		t.Errorf("bounded to %d", n)
	}
	if e := ParseJournal(nil); e == nil || len(e) != 0 {
		t.Error("empty journal must be an empty list")
	}
}
