package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
)

type fakeSysPower struct {
	blockers []helper.PowerBlocker
	err      error
	calls    []string
}

func (f *fakeSysPower) PowerPreflight(context.Context) (helper.PowerPreflight, error) {
	return helper.PowerPreflight{Blockers: f.blockers}, nil
}
func (f *fakeSysPower) PowerAction(_ context.Context, action string, force bool, user string) (helper.PowerActionResult, error) {
	if f.err != nil {
		return helper.PowerActionResult{}, f.err
	}
	call := action + " " + user
	if force {
		call += " force"
	}
	f.calls = append(f.calls, call)
	return helper.PowerActionResult{Action: action, Forced: force && len(f.blockers) > 0, Blockers: f.blockers}, nil
}

func newSysPowerServer(t *testing.T, p SystemPowerer) (*Server, http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, SystemPower: p}
	hd := s.Handler()
	return s, hd, sessionCookie(t, login(t, hd, "secret"))
}

func preview(t *testing.T, h http.Handler, c *http.Cookie, action string) powerPreview {
	t.Helper()
	rec := do(h, "POST", "/api/v1/system/"+action+"/preview", "", xrw, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body)
	}
	var p powerPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPowerRequiresSessionAndCSRF(t *testing.T) {
	_, h, c := newSysPowerServer(t, &fakeSysPower{})
	if rec := do(h, "POST", "/api/v1/system/reboot/preview", "", xrw, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/system/reboot", `{}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("without CSRF header = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/system/halt/preview", "", xrw, c); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown action = %d", rec.Code)
	}
}

func TestPowerPreviewThenReboot(t *testing.T) {
	f := &fakeSysPower{}
	_, h, c := newSysPowerServer(t, f)
	p := preview(t, h, c, "reboot")
	if p.Action != "reboot" || p.ConfirmToken == "" || len(p.Effects) == 0 || p.Blockers == nil {
		t.Fatalf("%+v", p)
	}
	rec := do(h, "POST", "/api/v1/system/reboot", `{"confirmToken":"`+p.ConfirmToken+`"}`, xrw, c)
	if rec.Code != http.StatusAccepted || len(f.calls) != 1 || f.calls[0] != "reboot admin" {
		t.Fatalf("reboot = %d %s %v", rec.Code, rec.Body, f.calls)
	}
	// Single use.
	rec = do(h, "POST", "/api/v1/system/reboot", `{"confirmToken":"`+p.ConfirmToken+`"}`, xrw, c)
	if rec.Code != http.StatusConflict || errBody(t, rec.Body.String())["code"] != "confirm_required" {
		t.Fatalf("reused token = %d %s", rec.Code, rec.Body)
	}
}

func TestPowerTokenBoundToAction(t *testing.T) {
	f := &fakeSysPower{}
	_, h, c := newSysPowerServer(t, f)
	p := preview(t, h, c, "reboot")
	rec := do(h, "POST", "/api/v1/system/shutdown", `{"confirmToken":"`+p.ConfirmToken+`"}`, xrw, c)
	if rec.Code != http.StatusConflict || len(f.calls) != 0 {
		t.Fatalf("reboot token for shutdown = %d %v", rec.Code, f.calls)
	}
	if rec := do(h, "POST", "/api/v1/system/shutdown", `{}`, xrw, c); rec.Code != http.StatusConflict {
		t.Fatalf("no token = %d", rec.Code)
	}
}

func TestPowerTokenExpires(t *testing.T) {
	f := &fakeSysPower{}
	s, h, c := newSysPowerServer(t, f)
	now := time.Unix(1000, 0)
	s.powerTokens.now = func() time.Time { return now }
	p := preview(t, h, c, "shutdown")
	now = now.Add(confirmTTL)
	if rec := do(h, "POST", "/api/v1/system/shutdown", `{"confirmToken":"`+p.ConfirmToken+`"}`, xrw, c); rec.Code != http.StatusConflict {
		t.Fatalf("expired token = %d", rec.Code)
	}
}

func TestPowerBlockedAndForced(t *testing.T) {
	f := &fakeSysPower{blockers: []helper.PowerBlocker{{Code: helper.BlockSmartTest, DiskID: "wwn-0x1"}}}
	_, h, c := newSysPowerServer(t, f)
	p := preview(t, h, c, "shutdown")
	if len(p.Blockers) != 1 || p.Blockers[0].Code != helper.BlockSmartTest {
		t.Fatalf("%+v", p)
	}
	f.err = &helper.Error{Code: helper.CodeBlocked, Message: "x"}
	rec := do(h, "POST", "/api/v1/system/shutdown", `{"confirmToken":"`+p.ConfirmToken+`"}`, xrw, c)
	if rec.Code != http.StatusConflict || errBody(t, rec.Body.String())["code"] != helper.CodeBlocked {
		t.Fatalf("blocked = %d %s", rec.Code, rec.Body)
	}
	f.err = nil
	p = preview(t, h, c, "shutdown")
	rec = do(h, "POST", "/api/v1/system/shutdown", `{"confirmToken":"`+p.ConfirmToken+`","force":true}`, xrw, c)
	if rec.Code != http.StatusAccepted || f.calls[0] != "shutdown admin force" {
		t.Fatalf("forced = %d %v", rec.Code, f.calls)
	}
}

func TestPowerNilBackend(t *testing.T) {
	_, h, c := newSysPowerServer(t, nil)
	if rec := do(h, "POST", "/api/v1/system/reboot/preview", "", xrw, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil backend = %d", rec.Code)
	}
}
