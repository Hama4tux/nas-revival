package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/monitor"
	"nas-revival/backend/internal/procs"
)

type fakeMonitor struct{ since []int64 }

func (f *fakeMonitor) History(since int64) monitor.History {
	f.since = append(f.since, since)
	return monitor.History{IntervalSeconds: 10, NetTotals: []monitor.NetTotal{}, Samples: []monitor.Sample{{T: since + 1}}}
}

func TestMonitorEndpointAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	h := s.Handler()
	admin := loginAs(t, h, "jana")
	if rec := do(h, "GET", "/api/v1/monitor", "", xrw, admin); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no monitor = %d", rec.Code)
	}
	f := &fakeMonitor{}
	s.Monitor = f
	h = s.Handler()
	for name, want := range map[string]int{"jana": 200, "petr": 200, "eva": 403} {
		if rec := do(h, "GET", "/api/v1/monitor?since=5", "", xrw, loginAs(t, h, name)); rec.Code != want {
			t.Errorf("as %s = %d, want %d", name, rec.Code, want)
		}
	}
	if len(f.since) != 2 || f.since[0] != 5 {
		t.Fatalf("since %v", f.since)
	}
	rec := do(h, "GET", "/api/v1/monitor", "", xrw, admin)
	var got monitor.History
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.IntervalSeconds != 10 || len(got.Samples) != 1 || f.since[2] != 0 {
		t.Fatalf("history %s %v", rec.Body, f.since)
	}
	for _, q := range []string{"abc", "-1"} {
		if rec := do(h, "GET", "/api/v1/monitor?since="+q, "", xrw, admin); rec.Code != 400 {
			t.Errorf("since=%s = %d", q, rec.Code)
		}
	}
}

type fakeProcs struct {
	calls  int
	killed []string
	err    error
}

func (f *fakeProcs) List(context.Context) (procs.List, error) {
	f.calls++
	return procs.List{Processes: []procs.Process{{PID: 900, Name: "rsync", Start: 1234}}, MemTotal: 1 << 28}, nil
}

func (f *fakeProcs) ProcessKill(_ context.Context, pid int, start uint64, force bool) (helper.ProcessKillResult, error) {
	if f.err != nil {
		return helper.ProcessKillResult{}, f.err
	}
	f.killed = append(f.killed, fmt.Sprintf("%d/%d/%v", pid, start, force))
	return helper.ProcessKillResult{PID: pid, Name: "rsync", Signal: "TERM"}, nil
}

func TestProcessEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	h := s.Handler()
	admin := loginAs(t, h, "jana")
	if rec := do(h, "GET", "/api/v1/monitor/processes", "", xrw, admin); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no source = %d", rec.Code)
	}
	f := &fakeProcs{}
	s.Processes, s.ProcessKill = f, f
	h = s.Handler()
	for name, want := range map[string]int{"jana": 200, "petr": 200, "eva": 403} {
		if rec := do(h, "GET", "/api/v1/monitor/processes", "", xrw, loginAs(t, h, name)); rec.Code != want {
			t.Errorf("list as %s = %d, want %d", name, rec.Code, want)
		}
	}
	rec := do(h, "GET", "/api/v1/monitor/processes", "", xrw, admin)
	var l procs.List
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil || len(l.Processes) != 1 || l.Processes[0].Start != 1234 {
		t.Fatalf("list %s", rec.Body)
	}

	body := `{"start":1234,"force":true}`
	for name, want := range map[string]int{"petr": 403, "eva": 403} {
		if rec := do(h, "POST", "/api/v1/monitor/processes/900/kill", body, xrw, loginAs(t, h, name)); rec.Code != want {
			t.Errorf("kill as %s = %d, want %d", name, rec.Code, want)
		}
	}
	if len(f.killed) != 0 {
		t.Fatalf("non-admin killed %v", f.killed)
	}
	if rec := do(h, "POST", "/api/v1/monitor/processes/900/kill", body, xrw, admin); rec.Code != 200 {
		t.Fatalf("kill as admin = %d %s", rec.Code, rec.Body)
	}
	if len(f.killed) != 1 || f.killed[0] != "900/1234/true" {
		t.Fatalf("killed %v", f.killed)
	}
	for path, b := range map[string]string{"/api/v1/monitor/processes/abc/kill": body, "/api/v1/monitor/processes/0/kill": body,
		"/api/v1/monitor/processes/900/kill": `{"force":true}`} {
		if rec := do(h, "POST", path, b, xrw, admin); rec.Code != 400 {
			t.Errorf("%s %s = %d", path, b, rec.Code)
		}
	}
	for code, want := range map[string]int{helper.CodeProcessProtected: 409, helper.CodeProcessChanged: 409, helper.CodeNotFound: 404} {
		f.err = &helper.Error{Code: code, Message: "x"}
		if rec := do(h, "POST", "/api/v1/monitor/processes/900/kill", body, xrw, admin); rec.Code != want {
			t.Errorf("helper %s = %d, want %d", code, rec.Code, want)
		}
	}
}
