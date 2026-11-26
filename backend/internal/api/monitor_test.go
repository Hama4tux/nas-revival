package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"nas-revival/backend/internal/monitor"
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
