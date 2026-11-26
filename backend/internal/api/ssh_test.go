package api

import (
	"context"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/sshd"
)

type fakeSsh struct {
	calls []string
	got   sshd.Settings
}

func (f *fakeSsh) SshGet(context.Context) (helper.SshService, error) {
	f.calls = append(f.calls, "get")
	return helper.SshService{Settings: sshd.Default()}, nil
}
func (f *fakeSsh) SshSet(_ context.Context, s sshd.Settings) (helper.SshService, error) {
	f.calls = append(f.calls, "set")
	f.got = s
	return helper.SshService{Settings: s}, nil
}

func TestSshEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeSsh{}
	s.Ssh = f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	ok := `{"port":2222,"passwordAuth":false,"sftpGroups":["nas-users","foto","nas-users"]}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/services/ssh", "", 200, 200, 403, "get"},
		{"PUT", "/api/v1/services/ssh", ok, 200, 403, 403, "set"},
		// Rejected before the helper is asked.
		{"PUT", "/api/v1/services/ssh", `{"port":23}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/ssh", `{"port":22,"sftpGroups":["root"]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/ssh", `{"port":22,"permitRootLogin":"yes"}`, 400, 403, 403, ""},
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
	if f.got.Port != 2222 || len(f.got.SftpGroups) != 2 || f.got.SftpGroups[0] != "foto" {
		t.Fatalf("settings not normalised: %+v", f.got)
	}
}
