package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tlscert"
)

type fakeNet struct {
	err     error
	applied *helper.NetConfig
}

func (f *fakeNet) st() helper.NetworkState {
	return helper.NetworkState{Interface: "enp1s0", Config: helper.DefaultNetConfig()}
}
func (f *fakeNet) NetworkGet(context.Context) (helper.NetworkState, error) { return f.st(), f.err }
func (f *fakeNet) NetworkApply(_ context.Context, c helper.NetConfig) (helper.NetworkState, error) {
	f.applied = &c
	return f.st(), f.err
}
func (f *fakeNet) NetworkConfirm(context.Context) (helper.NetworkState, error) { return f.st(), f.err }

type fakeTime struct {
	err     error
	servers []string
}

func (f *fakeTime) TimeGet(context.Context) (helper.TimeState, error) {
	return helper.TimeState{NTPServers: []string{"a"}, DefaultServers: true}, f.err
}
func (f *fakeTime) TimeApply(_ context.Context, s []string) (helper.TimeState, error) {
	f.servers = s
	return helper.TimeState{NTPServers: s}, f.err
}

type fakeTLS struct {
	err  error
	cert string
}

func (f *fakeTLS) Info() tlscert.Info        { return tlscert.Info{Subject: "CN=nas", KeyType: "ECDSA P-256"} }
func (f *fakeTLS) Install(c, k []byte) error { f.cert = string(c); return f.err }
func (f *fakeTLS) Generate() error           { return f.err }

func settingsServer(t *testing.T, n *fakeNet, tm *fakeTime, tl *fakeTLS) (http.Handler, *http.Cookie) {
	t.Helper()
	hash, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: hash})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(), Overview: fakeOverview{}, Smart: fakeSmart{}}
	if n != nil {
		s.Network = n
	}
	if tm != nil {
		s.Time = tm
	}
	if tl != nil {
		s.TLS = tl
	}
	h := s.Handler()
	return h, sessionCookie(t, login(t, h, "secret"))
}

func errBody(t *testing.T, body string) map[string]string {
	t.Helper()
	var e struct{ Error map[string]string }
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatal(body)
	}
	return e.Error
}

func TestSettingsRequireSessionAndCSRF(t *testing.T) {
	h, c := settingsServer(t, &fakeNet{}, &fakeTime{}, &fakeTLS{})
	for _, p := range []string{"/api/v1/settings/network", "/api/v1/settings/time", "/api/v1/settings/tls"} {
		if rec := do(h, "GET", p, "", nil, nil); rec.Code != 401 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
		if rec := do(h, "PUT", p, `{}`, map[string]string{}, c); rec.Code != 403 {
			t.Fatalf("%s csrf: %d", p, rec.Code)
		}
	}
	if rec := do(h, "POST", "/api/v1/settings/tls/generate", "", map[string]string{}, c); rec.Code != 403 {
		t.Fatal(rec.Code)
	}
}

func TestNetworkEndpoints(t *testing.T) {
	n := &fakeNet{}
	h, c := settingsServer(t, n, nil, nil)
	if rec := do(h, "GET", "/api/v1/settings/network", "", nil, c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":null`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	ok := `{"mode":"static","address":"192.168.1.10","prefixLength":24,"gateway":"192.168.1.1","dns":["192.168.1.1"]}`
	if rec := do(h, "PUT", "/api/v1/settings/network", ok, xrw, c); rec.Code != 202 || n.applied == nil || !n.applied.UseDhcpDns {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for body, field := range map[string]string{
		`{"mode":"static","address":"192.168.1.0","prefixLength":24,"gateway":"192.168.1.1","dns":["1.1.1.1"]}`: "address",
		`{"mode":"static","address":"192.168.1.10","prefixLength":24,"gateway":"192.168.1.1","dns":[]}`:         "dns",
		`{"mode":"nope"}`: "mode",
	} {
		rec := do(h, "PUT", "/api/v1/settings/network", body, xrw, c)
		if e := errBody(t, rec.Body.String()); rec.Code != 400 || e["code"] != "invalid" || e["field"] != field {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	for _, body := range []string{`{"mode":"dhcp","bogus":1}`, `nope`, `{"mode":"dhcp"} {}`} {
		if rec := do(h, "PUT", "/api/v1/settings/network", body, xrw, c); rec.Code != 400 {
			t.Fatalf("%s: %d", body, rec.Code)
		}
	}
	big := `{"mode":"dhcp","dns":["` + strings.Repeat("1", 17000) + `"]}`
	if rec := do(h, "PUT", "/api/v1/settings/network", big, xrw, c); rec.Code != 413 {
		t.Fatalf("big: %d", rec.Code)
	}
	for err, want := range map[string]int{helper.CodeChangePending: 409, helper.CodeNothingPending: 409, helper.CodeHelperUnavailable: 503, helper.CodeInternal: 500} {
		n.err = &helper.Error{Code: err, Message: "x"}
		if rec := do(h, "PUT", "/api/v1/settings/network", ok, xrw, c); rec.Code != want || errBody(t, rec.Body.String())["code"] != map[bool]string{true: err, false: "internal"}[err != helper.CodeInternal] {
			t.Fatalf("%s: %d %s", err, rec.Code, rec.Body)
		}
	}
	n.err = &helper.Error{Code: helper.CodeNothingPending, Message: "x"}
	if rec := do(h, "POST", "/api/v1/settings/network/confirm", "", xrw, c); rec.Code != 409 {
		t.Fatal(rec.Code)
	}
	n.err = nil
	if rec := do(h, "POST", "/api/v1/settings/network/confirm", "", xrw, c); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	n.err = &helper.Error{Code: helper.CodeHelperUnavailable, Message: "x"}
	if rec := do(h, "GET", "/api/v1/settings/network", "", nil, c); rec.Code != 503 {
		t.Fatal(rec.Code)
	}
}

func TestTimeEndpoints(t *testing.T) {
	tm := &fakeTime{}
	h, c := settingsServer(t, nil, tm, nil)
	rec := do(h, "GET", "/api/v1/settings/time", "", nil, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"now":"`) || !strings.Contains(rec.Body.String(), `"defaultServers":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/time", `{"ntpServers":["tik.cesnet.cz"]}`, xrw, c); rec.Code != 200 || tm.servers[0] != "tik.cesnet.cz" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/time", `{"ntpServers":[]}`, xrw, c); rec.Code != 200 || len(tm.servers) != 0 {
		t.Fatalf("%d", rec.Code)
	}
	rec = do(h, "PUT", "/api/v1/settings/time", `{"ntpServers":["a b"]}`, xrw, c)
	if e := errBody(t, rec.Body.String()); rec.Code != 400 || e["field"] != "ntpServers" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/time", `{"ntpServers":[],"x":1}`, xrw, c); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	tm.err = &helper.Error{Code: helper.CodeHelperUnavailable, Message: "x"}
	if rec := do(h, "PUT", "/api/v1/settings/time", `{"ntpServers":[]}`, xrw, c); rec.Code != 503 {
		t.Fatal(rec.Code)
	}
}

func TestTLSEndpoints(t *testing.T) {
	tl := &fakeTLS{}
	h, c := settingsServer(t, nil, nil, tl)
	if rec := do(h, "GET", "/api/v1/settings/tls", "", nil, c); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ECDSA P-256") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/tls", `{"certificate":"C","privateKey":"K"}`, xrw, c); rec.Code != 200 || tl.cert != "C" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/tls", `{"certificate":"C"}`, xrw, c); rec.Code != 400 || errBody(t, rec.Body.String())["field"] != "privateKey" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/settings/tls", `{"certificate":"`+strings.Repeat("A", 70000)+`","privateKey":"K"}`, xrw, c); rec.Code != 413 {
		t.Fatalf("big %d", rec.Code)
	}
	// 20 KiB is fine for TLS although above the 16 KiB general limit
	if rec := do(h, "PUT", "/api/v1/settings/tls", `{"certificate":"`+strings.Repeat("A", 20000)+`","privateKey":"K"}`, xrw, c); rec.Code != 200 {
		t.Fatalf("20k %d", rec.Code)
	}
	tl.err = &tlscert.Error{Field: "privateKey", Message: "Soukromý klíč neodpovídá certifikátu."}
	rec := do(h, "PUT", "/api/v1/settings/tls", `{"certificate":"C","privateKey":"K"}`, xrw, c)
	if e := errBody(t, rec.Body.String()); rec.Code != 400 || e["field"] != "privateKey" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	tl.err = nil
	if rec := do(h, "POST", "/api/v1/settings/tls/generate", "", xrw, c); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	tl.err = context.Canceled
	if rec := do(h, "POST", "/api/v1/settings/tls/generate", "", xrw, c); rec.Code != 500 {
		t.Fatal(rec.Code)
	}
}

func TestSettingsNilBackend(t *testing.T) {
	h, c := settingsServer(t, nil, nil, nil)
	if rec := do(h, "GET", "/api/v1/settings/network", "", nil, c); rec.Code != 503 {
		t.Fatal(rec.Code)
	}
}
