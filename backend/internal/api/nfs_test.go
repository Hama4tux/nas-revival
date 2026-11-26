package api

import (
	"context"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/nfs"
)

type fakeNfs struct {
	calls []string
	got   nfs.Settings
}

func (f *fakeNfs) NfsGet(context.Context) (helper.NfsService, error) {
	f.calls = append(f.calls, "get")
	return helper.NfsService{Settings: nfs.Default()}, nil
}
func (f *fakeNfs) NfsSet(_ context.Context, s nfs.Settings) (helper.NfsService, error) {
	f.calls = append(f.calls, "set")
	f.got = s
	return helper.NfsService{Settings: s}, nil
}
func (f *fakeNfs) NfsStatus(context.Context) (helper.NfsStatus, error) {
	f.calls = append(f.calls, "status")
	return helper.NfsStatus{}, nil
}

func TestNfsEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeNfs{}
	s.Nfs = f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	ok := `{"enabled":true,"exports":[{"share":"zalohy","clients":[{"host":"192.168.1.77/24"}]}]}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/services/nfs", "", 200, 200, 403, "get"},
		{"PUT", "/api/v1/services/nfs", ok, 200, 403, 403, "set"},
		// Client addresses with a lease: admins only.
		{"GET", "/api/v1/services/nfs/status", "", 200, 403, 403, "status"},
		// Rejected before the helper is asked: world export, no_root_squash, unknown field.
		{"PUT", "/api/v1/services/nfs", `{"exports":[{"share":"zalohy","clients":[{"host":"*"}]}]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/nfs", `{"exports":[{"share":"zalohy","clients":[{"host":"10.0.0.5"}],"squash":"none"}]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/nfs", `{"exports":[{"share":"../etc","clients":[{"host":"10.0.0.5"}]}]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/nfs", `{"exports":[{"share":"zalohy","path":"/etc","clients":[{"host":"10.0.0.5"}]}]}`, 400, 403, 403, ""},
	}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			f.calls = nil
			rec := do(h, c.method, c.path, c.body, xrw, cookies[name])
			if rec.Code != want {
				t.Errorf("%s %s %s as %s = %d, want %d (%s)", c.method, c.path, c.body, name, rec.Code, want, rec.Body)
			}
			wantCall := ""
			if want == 200 {
				wantCall = c.call
			}
			if (len(f.calls) == 0 && wantCall != "") || (len(f.calls) > 0 && f.calls[0] != wantCall) || len(f.calls) > 1 {
				t.Errorf("%s %s as %s: calls %v, want %q", c.method, c.path, name, f.calls, wantCall)
			}
		}
	}
	if f.got.Exports[0].Clients[0].Host != "192.168.1.0/24" || f.got.Exports[0].Squash != nfs.SquashRoot {
		t.Fatalf("not normalised: %+v", f.got)
	}
}
