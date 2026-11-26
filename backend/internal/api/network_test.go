package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/netinfo"
)

type fakeNetInfo struct {
	calls    []string
	hostname string
}

func (f *fakeNetInfo) Read(context.Context) (netinfo.Info, error) {
	f.calls = append(f.calls, "read")
	return netinfo.Info{Hostname: f.hostname, Domains: []string{}, Interfaces: []netinfo.Interface{}, Routes: []netinfo.Route{}}, nil
}

func (f *fakeNetInfo) HostnameSet(_ context.Context, name string) (helper.HostnameState, error) {
	f.calls = append(f.calls, "hostname "+name)
	f.hostname = name
	return helper.HostnameState{Hostname: name}, nil
}

func (f *fakeNetInfo) NetDiag(_ context.Context, test, target string) (helper.DiagResult, error) {
	f.calls = append(f.calls, "diag "+test+" "+target)
	if target == "busy.example" {
		return helper.DiagResult{}, &helper.Error{Code: helper.CodeRateLimited, Message: "x"}
	}
	return helper.DiagResult{Test: test, Target: target, OK: true}, nil
}

func TestNetworkEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeNetInfo{hostname: "nsa310"}
	s.NetInfo, s.NetAdmin = f, f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/network", "", 200, 200, 403, "read"},
		{"PUT", "/api/v1/network/hostname", `{"hostname":"Nas"}`, 200, 403, 403, "hostname nas"},
		// Rejected before the helper is asked.
		{"PUT", "/api/v1/network/hostname", `{"hostname":"nas.home"}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/network/hostname", `{"hostname":"-x"}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/network/hostname", `{"name":"nas"}`, 400, 403, 403, ""},
		{"POST", "/api/v1/network/diagnostics", `{"test":"ping","target":" 192.0.2.1 "}`, 200, 403, 403, "diag ping 192.0.2.1"},
		{"POST", "/api/v1/network/diagnostics", `{"test":"ports"}`, 200, 403, 403, "diag ports "},
		// Rejected before the helper is asked.
		{"POST", "/api/v1/network/diagnostics", `{"test":"ping","target":"224.0.0.1"}`, 400, 403, 403, ""},
		{"POST", "/api/v1/network/diagnostics", `{"test":"portscan","target":"192.0.2.1"}`, 400, 403, 403, ""},
		{"POST", "/api/v1/network/diagnostics", `{"test":"ping","target":"192.0.2.1","count":1000}`, 400, 403, 403, ""},
	}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			f.calls = nil
			rec := do(h, c.method, c.path, c.body, xrw, cookies[name])
			if rec.Code != want {
				t.Errorf("%s %s %s as %s = %d, want %d (%s)", c.method, c.path, c.body, name, rec.Code, want, rec.Body)
			}
			if want != 200 {
				if len(f.calls) != 0 {
					t.Errorf("%s %s as %s: calls %v", c.method, c.path, name, f.calls)
				}
				continue
			}
			if len(f.calls) == 0 || f.calls[0] != c.call {
				t.Errorf("%s %s as %s: calls %v, want %q", c.method, c.path, name, f.calls, c.call)
			}
		}
	}
	if rec := do(h, "POST", "/api/v1/network/diagnostics", `{"test":"ping","target":"busy.example"}`, xrw, cookies["jana"]); rec.Code != 429 {
		t.Errorf("rate limited = %d", rec.Code)
	}
	// The change answers with the fresh overview.
	rec := do(h, "PUT", "/api/v1/network/hostname", `{"hostname":"archiv"}`, xrw, cookies["jana"])
	var info netinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || info.Hostname != "archiv" {
		t.Fatalf("response %s: %v", rec.Body, err)
	}
}
