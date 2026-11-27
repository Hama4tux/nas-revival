package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/firewall"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/netinfo"
)

type fakeFirewall struct {
	err     error
	state   helper.FirewallState
	applied *firewall.Settings
	client  string
}

func (f *fakeFirewall) FirewallGet(context.Context) (helper.FirewallState, error) {
	return f.state, f.err
}
func (f *fakeFirewall) FirewallApply(_ context.Context, s firewall.Settings, c string) (helper.FirewallState, error) {
	f.applied, f.client = &s, c
	st := f.state
	st.Pending = &helper.FirewallPending{Deadline: time.Date(2026, 11, 27, 12, 3, 0, 0, time.UTC), Next: s}
	return st, f.err
}
func (f *fakeFirewall) FirewallConfirm(context.Context) (helper.FirewallState, error) {
	return f.state, f.err
}

type fakeLanInfo struct{}

func (fakeLanInfo) Read(context.Context) (netinfo.Info, error) {
	return netinfo.Info{Interfaces: []netinfo.Interface{{Name: "enp1s0", Addresses: []string{"fe80::1/64", "192.0.2.60/24"}}}}, nil
}

func newFirewallServer(t *testing.T) (http.Handler, *fakeFirewall) {
	t.Helper()
	s, h, _, _ := newRoleServer(t)
	f := &fakeFirewall{state: helper.FirewallState{Supported: true, Active: false, Settings: firewall.Default(),
		Listening: []firewall.Listen{{Proto: "tcp", Port: 8443, Process: "nas-revival"}}}}
	s.Firewall = f
	s.NetInfo = fakeLanInfo{}
	return h, f
}

const fwBody = `{"enabled":true,"allow":{"web":["192.0.2.0/24"],"ssh":["192.0.2.5"]}}`

func TestFirewallGet(t *testing.T) {
	h, _ := newFirewallServer(t)
	rec := do(h, "GET", "/api/v1/security/firewall", "", xrw, loginAs(t, h, "petr")) // read-only admin
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"supported": "true", "active": "false", "pending": "null",
		"clientAddress": `"192.0.2.5"`, "lanNetwork": `"192.0.2.0/24"`,
		"settings":  `{"enabled":false,"allow":{"nfs":[],"smb":[],"ssh":[],"web":[]},"rules":[]}`,
		"listening": `[{"proto":"tcp","port":8443,"process":"nas-revival","allowed":false}]`} {
		if string(v[k]) != want {
			t.Errorf("%s = %s, want %s", k, v[k], want)
		}
	}
	if !strings.Contains(string(v["services"]), `{"key":"nfs","ports":[{"proto":"tcp","port":2049},{"proto":"tcp","port":111}]}`) {
		t.Errorf("services: %s", v["services"])
	}
}

func TestFirewallLanNetworkNull(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	s.Firewall = &fakeFirewall{state: helper.FirewallState{Settings: firewall.Default()}}
	rec := do(h, "GET", "/api/v1/security/firewall", "", xrw, loginAs(t, h, "jana"))
	if !strings.Contains(rec.Body.String(), `"lanNetwork":null`) {
		t.Fatal(rec.Body)
	}
}

func TestFirewallPut(t *testing.T) {
	h, f := newFirewallServer(t)
	rec := do(h, "PUT", "/api/v1/security/firewall", fwBody, xrw, loginAs(t, h, "jana"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pending":{"deadline":"2026-11-27T12:03:00Z"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.client != "192.0.2.5" || f.applied == nil || len(f.applied.Allow["smb"]) != 0 || f.applied.Allow["smb"] == nil {
		t.Fatalf("%q %+v", f.client, f.applied)
	}
}

func TestFirewallPutErrors(t *testing.T) {
	h, f := newFirewallServer(t)
	jana := loginAs(t, h, "jana")
	rec := do(h, "PUT", "/api/v1/security/firewall", `{"enabled":true,"allow":{"web":["10.0.0.0/8"],"ssh":["192.0.2.5"]}}`, xrw, jana)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"allow.web"`) {
		t.Fatalf("lockout: %d %s", rec.Code, rec.Body)
	}
	// X-Forwarded-For is never trusted.
	hdr := map[string]string{"X-Forwarded-For": "10.1.1.1", "X-Requested-With": xrw["X-Requested-With"], "Content-Type": "application/json"}
	rec = do(h, "PUT", "/api/v1/security/firewall", `{"enabled":true,"allow":{"web":["10.1.1.1"],"ssh":["10.1.1.1"]}}`, hdr, jana)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("xff: %d", rec.Code)
	}
	rec = do(h, "PUT", "/api/v1/security/firewall", `{"enabled":false,"allow":{"ftp":[]}}`, xrw, jana)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key: %d", rec.Code)
	}
	if f.applied != nil {
		t.Fatal("helper called for invalid input")
	}
	f.err = &helper.Error{Code: helper.CodeChangePending, Message: "x"}
	if rec := do(h, "PUT", "/api/v1/security/firewall", fwBody, xrw, jana); rec.Code != http.StatusConflict {
		t.Fatalf("pending: %d", rec.Code)
	}
}

func TestFirewallConfirm(t *testing.T) {
	h, f := newFirewallServer(t)
	jana := loginAs(t, h, "jana")
	if rec := do(h, "POST", "/api/v1/security/firewall/confirm", "", xrw, jana); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f.err = &helper.Error{Code: helper.CodeNothingPending, Message: "x"}
	if rec := do(h, "POST", "/api/v1/security/firewall/confirm", "", xrw, jana); rec.Code != http.StatusConflict {
		t.Fatalf("nothing pending: %d", rec.Code)
	}
}

func TestFirewallRoles(t *testing.T) {
	h, _ := newFirewallServer(t)
	for _, c := range []struct {
		method, path, body string
		viewer, user       int
	}{
		{"GET", "/api/v1/security/firewall", "", 200, 403},
		{"PUT", "/api/v1/security/firewall", fwBody, 403, 403},
		{"POST", "/api/v1/security/firewall/confirm", "", 403, 403},
	} {
		if rec := do(h, c.method, c.path, c.body, xrw, loginAs(t, h, "petr")); rec.Code != c.viewer {
			t.Errorf("viewer %s %s = %d", c.method, c.path, rec.Code)
		}
		if rec := do(h, c.method, c.path, c.body, xrw, loginAs(t, h, "eva")); rec.Code != c.user {
			t.Errorf("user %s %s = %d", c.method, c.path, rec.Code)
		}
	}
}

func TestFirewallUnavailable(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	if rec := do(h, "GET", "/api/v1/security/firewall", "", xrw, loginAs(t, h, "jana")); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAdvisorFirewallOff(t *testing.T) {
	h, _ := newFirewallServer(t)
	rec := do(h, "GET", "/api/v1/security/advisor", "", xrw, loginAs(t, h, "jana"))
	var body struct {
		Findings []struct{ ID, Status, Fix string }
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	for _, f := range body.Findings {
		if f.ID == "firewall.off" {
			if f.Status != "warn" || f.Fix != "#/system/security" {
				t.Fatalf("%+v", f)
			}
			return
		}
	}
	t.Fatal("no firewall.off finding")
}

func TestFirewallPutRules(t *testing.T) {
	h, f := newFirewallServer(t)
	jana := loginAs(t, h, "jana")
	body := `{"enabled":true,"allow":{"web":["192.0.2.0/24"],"ssh":["192.0.2.5"]},"rules":[{"source":"192.0.2.7/24","ports":"06000-6010","proto":"both","action":"drop","note":" Dev "}]}`
	rec := do(h, "PUT", "/api/v1/security/firewall", body, xrw, jana)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pending":{"deadline":"2026-11-27T12:03:00Z"`) ||
		!strings.Contains(rec.Body.String(), `"rules":[{"source":"192.0.2.0/24","ports":"6000-6010","proto":"both","action":"drop","note":"Dev"}]`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.applied == nil || len(f.applied.Rules) != 1 {
		t.Fatalf("%+v", f.applied)
	}
	f.applied = nil
	lock := strings.Replace(body, `"6000-6010"`, `"8443"`, 1)
	lock = strings.Replace(lock, `06000-6010`, `8443`, 1)
	rec = do(h, "PUT", "/api/v1/security/firewall", lock, xrw, jana)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"rules.0.action"`) || f.applied != nil {
		t.Fatalf("lockout: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "PUT", "/api/v1/security/firewall", strings.Replace(body, `"both"`, `"icmp"`, 1), xrw, jana)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"rules.0.proto"`) || f.applied != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
