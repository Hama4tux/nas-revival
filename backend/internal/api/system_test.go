package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/system"
)

type fakeSystem struct{}

func (fakeSystem) Info() (system.Info, error) {
	return system.Info{Version: "v-test", Hostname: "nas", Kernel: "6.12.119"}, nil
}

func (fakeSystem) Capabilities() system.Capabilities {
	return system.CapabilitiesOf(system.DetectProfile(""))
}

func newSystemServer(t *testing.T, src SystemSource) http.Handler {
	t.Helper()
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, System: src}
	return s.Handler()
}

func TestSystemEndpointsNeedSession(t *testing.T) {
	h := newSystemServer(t, fakeSystem{})
	for _, p := range []string{"/api/v1/system/info", "/api/v1/system/capabilities"} {
		if rec := do(h, "GET", p, "", nil, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d", p, rec.Code)
		}
	}
}

func TestSystemInfoAndCapabilities(t *testing.T) {
	h := newSystemServer(t, fakeSystem{})
	c := sessionCookie(t, login(t, h, "secret"))
	rec := do(h, "GET", "/api/v1/system/info", "", nil, c)
	var in system.Info
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &in) != nil || in.Version != "v-test" {
		t.Fatalf("info: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/v1/system/capabilities", "", nil, c)
	var caps system.Capabilities
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &caps) != nil || caps.ProfileID == "" || len(caps.Capabilities) == 0 {
		t.Fatalf("caps: %d %s", rec.Code, rec.Body)
	}
}

func TestSystemUnavailable(t *testing.T) {
	h := newSystemServer(t, nil)
	c := sessionCookie(t, login(t, h, "secret"))
	if rec := do(h, "GET", "/api/v1/system/info", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil source = %d", rec.Code)
	}
}
