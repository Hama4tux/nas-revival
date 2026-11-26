package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"nas-revival/backend/internal/helper"
)

type fakeServices struct{ calls []string }

func (f *fakeServices) ServiceList(context.Context) (helper.ServiceList, error) {
	f.calls = append(f.calls, "list")
	return helper.ServiceList{Services: []helper.Service{}}, nil
}
func (f *fakeServices) ServiceAction(_ context.Context, name, action string) (helper.Service, error) {
	f.calls = append(f.calls, action+" "+name)
	return helper.Service{Name: name}, nil
}
func (f *fakeServices) ServiceLog(_ context.Context, name string, lines int) (helper.ServiceLog, error) {
	f.calls = append(f.calls, "log "+name+" "+strconv.Itoa(lines))
	return helper.ServiceLog{Name: name}, nil
}

func TestServicesEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeServices{}
	s.Services = f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	cases := []struct {
		method, path        string
		admin, viewer, user int
		call                string // helper call made for the administrator
	}{
		{"GET", "/api/v1/services", 200, 200, 403, "list"},
		{"POST", "/api/v1/services/cron.service/restart", 200, 403, 403, "restart cron.service"},
		{"GET", "/api/v1/services/ssh.service/log", 200, 403, 403, "log ssh.service 100"},
		{"GET", "/api/v1/services/ssh.service/log?lines=200", 200, 403, 403, "log ssh.service 200"},
		// Rejected before the helper is asked.
		{"GET", "/api/v1/services/ssh.service/log?lines=201", 400, 403, 403, ""},
		{"GET", "/api/v1/services/ssh.service/log?lines=x", 400, 403, 403, ""},
		{"POST", "/api/v1/services/ssh.service/stop", 400, 403, 403, ""},
		{"POST", "/api/v1/services/cron.service/kill", 400, 403, 403, ""},
		{"POST", "/api/v1/services/getty@tty1.service/stop", 404, 403, 403, ""},
		{"GET", "/api/v1/services/getty@tty1.service/log", 404, 403, 403, ""},
	}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			f.calls = nil
			rec := do(h, c.method, c.path, "", xrw, cookies[name])
			if rec.Code != want {
				t.Errorf("%s %s as %s = %d, want %d (%s)", c.method, c.path, name, rec.Code, want, rec.Body)
			}
			wantCall := ""
			if name == "jana" || (name == "petr" && want == 200) {
				wantCall = c.call
			}
			got := ""
			if len(f.calls) > 0 {
				got = f.calls[0]
			}
			if got != wantCall || len(f.calls) > 1 {
				t.Errorf("%s %s as %s: calls %v, want %q", c.method, c.path, name, f.calls, wantCall)
			}
		}
	}
	if rec := do(h, "POST", "/api/v1/services/cron.service/restart", "", nil, cookies["jana"]); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF header = %d", rec.Code)
	}
}

func TestServicesUnavailable(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	c := loginAs(t, h, "jana")
	if rec := do(h, "GET", "/api/v1/services", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no helper = %d", rec.Code)
	}
}
