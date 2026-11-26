package api

import (
	"context"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/smb"
)

type fakeSmb struct {
	calls []string
	got   smb.Settings
}

func (f *fakeSmb) SmbGet(context.Context) (helper.SmbService, error) {
	f.calls = append(f.calls, "get")
	return helper.SmbService{Settings: smb.Default()}, nil
}
func (f *fakeSmb) SmbSet(_ context.Context, s smb.Settings) (helper.SmbService, error) {
	f.calls = append(f.calls, "set")
	f.got = s
	return helper.SmbService{Settings: s}, nil
}
func (f *fakeSmb) SmbStatus(context.Context) (smb.Status, error) {
	f.calls = append(f.calls, "status")
	return smb.Status{}, nil
}

func TestSmbEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeSmb{}
	s.Smb = f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	ok := `{"enabled":true,"workgroup":"home","description":"NAS","minProtocol":"SMB3_00"}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/services/smb", "", 200, 200, 403, "get"},
		{"PUT", "/api/v1/services/smb", ok, 200, 403, 403, "set"},
		// Sessions and open files are admin only (SMB-02).
		{"GET", "/api/v1/services/smb/status", "", 200, 403, 403, "status"},
		// Rejected before the helper is asked.
		{"PUT", "/api/v1/services/smb", `{"workgroup":"WG","minProtocol":"NT1"}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/smb", `{"workgroup":"WG","minProtocol":"SMB2_10","description":"50 %"}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/smb", `{"workgroup":"WG","minProtocol":"SMB2_10","guest":true}`, 400, 403, 403, ""},
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
	if f.got.Workgroup != "HOME" {
		t.Fatalf("workgroup not normalised: %+v", f.got)
	}
}
