package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/auth"
)

// fakeRoles is a mutable directory: name → role; missing = disabled/deleted.
type fakeRoles struct {
	roles map[string]accounts.Role
	nas   map[string]bool
}

func (f *fakeRoles) Role(n string) (accounts.Role, bool) { r, ok := f.roles[n]; return r, ok }
func (f *fakeRoles) IsNASUser(n string) bool             { return f.nas[n] }

const rolePassword = "correct horse battery"

func newRoleServer(t *testing.T) (*Server, http.Handler, *fakeRoles, *fakeUsers) {
	t.Helper()
	h, _ := auth.HashPassword(rolePassword)
	v, _ := auth.NewLookupVerifier(func(string) (string, bool) { return h, true })
	roles := &fakeRoles{
		roles: map[string]accounts.Role{"jana": accounts.RoleAdmin, "petr": accounts.RoleViewer, "eva": accounts.RoleUser, "rescue": accounts.RoleAdmin},
		nas:   map[string]bool{"jana": true, "petr": true, "eva": true},
	}
	u := &fakeUsers{}
	s := &Server{Verifier: v, Roles: roles, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Users: u}
	return s, s.Handler(), roles, u
}

func loginAs(t *testing.T, h http.Handler, name string) *http.Cookie {
	t.Helper()
	rec := do(h, "POST", "/api/v1/session", `{"username":"`+name+`","password":"`+rolePassword+`"}`, xrw, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login %s = %d %s", name, rec.Code, rec.Body)
	}
	return sessionCookie(t, rec)
}

func TestRolePolicy(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
	}{
		{"GET", "/api/v1/session", "", 200, 200, 200},
		{"GET", "/api/v1/overview", "", 200, 200, 403},
		{"GET", "/api/v1/users", "", 200, 200, 403},
		{"POST", "/api/v1/groups", `{"name":"foto"}`, 201, 403, 403},
		{"PUT", "/api/v1/users/karel/disabled", `{"disabled":true}`, 200, 403, 403},
		{"POST", "/api/v1/system/reboot/preview", `{}`, 503, 403, 403},
		{"PUT", "/api/v1/settings/network", `{}`, 503, 403, 403},
	}
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			if rec := do(h, c.method, c.path, c.body, xrw, cookies[name]); rec.Code != want {
				t.Errorf("%s %s as %s = %d, want %d (%s)", c.method, c.path, name, rec.Code, want, rec.Body)
			}
		}
	}
}

func TestSessionReportsRoleAndEndsWhenAccountGone(t *testing.T) {
	_, h, roles, _ := newRoleServer(t)
	c := loginAs(t, h, "petr")
	rec := do(h, "GET", "/api/v1/session", "", nil, c)
	var v struct{ Username, Role string }
	if json.Unmarshal(rec.Body.Bytes(), &v) != nil || v.Username != "petr" || v.Role != "viewer" {
		t.Fatalf("session = %s", rec.Body)
	}
	// Promoted on the console: applies to the next request.
	roles.roles["petr"] = accounts.RoleAdmin
	if rec := do(h, "POST", "/api/v1/groups", `{"name":"foto"}`, xrw, c); rec.Code != 201 {
		t.Fatalf("after promotion = %d", rec.Code)
	}
	delete(roles.roles, "petr") // disabled or deleted
	if rec := do(h, "GET", "/api/v1/session", "", nil, c); rec.Code != 401 {
		t.Fatalf("after disable = %d", rec.Code)
	}
	roles.roles["petr"] = accounts.RoleViewer
	if rec := do(h, "GET", "/api/v1/session", "", nil, c); rec.Code != 401 {
		t.Fatalf("session must stay ended, got %d", rec.Code)
	}
}

func TestRoleChangeEndsSessionsAndSelfDemotionRefused(t *testing.T) {
	_, h, _, u := newRoleServer(t)
	admin := loginAs(t, h, "jana")
	eva := loginAs(t, h, "eva")
	if rec := do(h, "PUT", "/api/v1/users/jana", `{"fullName":"J","groups":[],"role":"viewer"}`, xrw, admin); rec.Code != 400 {
		t.Fatalf("self demotion = %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/users/eva", `{"fullName":"E","groups":[],"role":"viewer"}`, xrw, admin); rec.Code != 200 {
		t.Fatalf("role change = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/session", "", nil, eva); rec.Code != 401 {
		t.Fatalf("eva's session survived a role change: %d", rec.Code)
	}
	if len(u.calls) != 1 || u.calls[0] != "update eva" {
		t.Fatalf("calls = %v", u.calls)
	}
}

func TestOwnPassword(t *testing.T) {
	_, h, _, u := newRoleServer(t)
	eva := loginAs(t, h, "eva")
	other := loginAs(t, h, "eva")
	if rec := do(h, "PUT", "/api/v1/session/password", `{"current":"wrong password!","password":"new password 123"}`, xrw, eva); rec.Code != 400 || len(u.calls) != 0 {
		t.Fatalf("wrong current = %d %v", rec.Code, u.calls)
	}
	if rec := do(h, "PUT", "/api/v1/session/password", `{"current":"`+rolePassword+`","password":"short"}`, xrw, eva); rec.Code != 400 || len(u.calls) != 0 {
		t.Fatalf("short = %d %v", rec.Code, u.calls)
	}
	if rec := do(h, "PUT", "/api/v1/session/password", `{"current":"`+rolePassword+`","password":"new password 123"}`, xrw, eva); rec.Code != 200 {
		t.Fatalf("change = %d %s", rec.Code, rec.Body)
	}
	if len(u.calls) != 1 || u.calls[0] != "password eva" {
		t.Fatalf("calls = %v", u.calls)
	}
	if rec := do(h, "GET", "/api/v1/session", "", nil, other); rec.Code != 401 {
		t.Fatalf("other session survived = %d", rec.Code)
	}
	rescue := loginAs(t, h, "rescue")
	if rec := do(h, "PUT", "/api/v1/session/password", `{"current":"`+rolePassword+`","password":"new password 123"}`, xrw, rescue); rec.Code != 409 {
		t.Fatalf("recovery admin = %d", rec.Code)
	}
}

func TestOwnPasswordWrongCurrentCountsAsFailedLogin(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	eva := loginAs(t, h, "eva")
	for i := 0; i < 5; i++ {
		do(h, "PUT", "/api/v1/session/password", `{"current":"wrong password!","password":"new password 123"}`, xrw, eva)
	}
	if rec := do(h, "PUT", "/api/v1/session/password", `{"current":"`+rolePassword+`","password":"new password 123"}`, xrw, eva); rec.Code != 429 {
		t.Fatalf("after 5 failures = %d", rec.Code)
	}
}
