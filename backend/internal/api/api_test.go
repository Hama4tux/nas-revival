package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/system"
)

type fakeOverview struct{}

func (fakeOverview) Collect(context.Context) (system.Overview, error) {
	return system.Overview{Host: system.Host{Hostname: "nas"}}, nil
}

type fakeSmart struct{}

func (fakeSmart) Report(context.Context) smart.Report { return smart.Report{Disks: []smart.Disk{}} }

func newServer(t *testing.T) http.Handler {
	t.Helper()
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{},
		UI: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ui")) }),
	}
	return s.Handler()
}

func do(h http.Handler, method, path, body string, hdr map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://nas.test"+path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.5:5555"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var xrw = map[string]string{"X-Requested-With": "nas-revival", "Content-Type": "application/json"}

func login(t *testing.T, h http.Handler, password string) *httptest.ResponseRecorder {
	return do(h, "POST", "/api/v1/session", `{"username":"admin","password":"`+password+`"}`, xrw, nil)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestLoginFlow(t *testing.T) {
	h := newServer(t)
	if rec := do(h, "GET", "/api/v1/overview", "", nil, nil); rec.Code != 401 {
		t.Fatalf("unauthenticated overview: %d", rec.Code)
	}
	rec := login(t, h, "secret")
	if rec.Code != 204 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags %+v", c)
	}
	rec = do(h, "GET", "/api/v1/overview", "", nil, c)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("overview: %d", rec.Code)
	}
	var o system.Overview
	json.Unmarshal(rec.Body.Bytes(), &o)
	if o.Host.Hostname != "nas" {
		t.Fatalf("body %s", rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/session", "", nil, c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"username":"admin"`) {
		t.Fatalf("session: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/smart", "", nil, c); rec.Code != 200 {
		t.Fatalf("smart: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/session", "", map[string]string{"X-Requested-With": "nas-revival"}, c); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/overview", "", nil, c); rec.Code != 401 {
		t.Fatalf("after logout: %d", rec.Code)
	}
}

func TestCSRF(t *testing.T) {
	h := newServer(t)
	body := `{"username":"admin","password":"secret"}`
	if rec := do(h, "POST", "/api/v1/session", body, map[string]string{"Content-Type": "application/json"}, nil); rec.Code != 403 {
		t.Fatalf("missing header: %d", rec.Code)
	}
	hdr := map[string]string{"X-Requested-With": "nas-revival", "Origin": "https://evil.example"}
	if rec := do(h, "POST", "/api/v1/session", body, hdr, nil); rec.Code != 403 {
		t.Fatalf("foreign origin: %d", rec.Code)
	}
	hdr["Origin"] = "https://nas.test"
	if rec := do(h, "POST", "/api/v1/session", body, hdr, nil); rec.Code != 204 {
		t.Fatalf("same origin: %d", rec.Code)
	}
}

func TestLoginValidationAndRateLimit(t *testing.T) {
	h := newServer(t)
	for _, b := range []string{`{}`, `not json`, `{"username":"admin","password":"x","extra":1}`} {
		if rec := do(h, "POST", "/api/v1/session", b, xrw, nil); rec.Code != 400 {
			t.Fatalf("%s: %d", b, rec.Code)
		}
	}
	for i := 0; i < 5; i++ {
		if rec := login(t, h, "wrong"); rec.Code != 401 || !strings.Contains(rec.Body.String(), "invalid_credentials") {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := login(t, h, "secret")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected rate limit even for the right password: %d", rec.Code)
	}
}

func TestSecurityHeadersAndRouting(t *testing.T) {
	h := newServer(t)
	rec := do(h, "GET", "/", "", nil, nil)
	if rec.Body.String() != "ui" {
		t.Fatalf("ui not served: %s", rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", rec.Header())
	}
	if rec := do(h, "GET", "/api/v1/nope", "", nil, nil); rec.Code != 404 {
		t.Fatalf("unknown api: %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/overview", "", map[string]string{"X-Requested-With": "nas-revival"}, nil); rec.Code == 200 {
		t.Fatal("wrong method accepted")
	}
}
