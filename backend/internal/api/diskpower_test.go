package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/helper"
)

type fakePower struct {
	cfg   diskpower.Config
	sets  []diskpower.Settings
	sleep []string
	err   error
}

func (f *fakePower) Read(_ context.Context, dev string) diskpower.Live {
	return diskpower.Live{State: diskpower.StateActive, CheckedAt: time.Unix(0, 0)}
}
func (f *fakePower) Config() (diskpower.Config, error) { return f.cfg, nil }
func (f *fakePower) DiskSetPower(_ context.Context, id string, s diskpower.Settings) (helper.DiskPowerResult, error) {
	if f.err != nil {
		return helper.DiskPowerResult{}, f.err
	}
	f.sets = append(f.sets, s)
	f.cfg.Disks[id] = s
	return helper.DiskPowerResult{DiskID: id, Settings: s}, nil
}
func (f *fakePower) DiskStandby(_ context.Context, id string) (helper.DiskStandbyResult, error) {
	if f.err != nil {
		return helper.DiskStandbyResult{}, f.err
	}
	f.sleep = append(f.sleep, id)
	return helper.DiskStandbyResult{DiskID: id}, nil
}

func newPowerServer(t *testing.T, p *fakePower) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Storage: fakeStorage{}, DiskPower: p, DiskPowerSet: p}
	hd := s.Handler()
	return hd, sessionCookie(t, login(t, hd, "secret"))
}

func TestDiskPowerGetDefault(t *testing.T) {
	p := &fakePower{cfg: diskpower.Config{Disks: map[string]diskpower.Settings{}}}
	h, c := newPowerServer(t, p)
	if rec := do(h, "GET", "/api/v1/storage/disks/wwn-0x1/power", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/storage/disks/wwn-0x1/power", "", nil, c)
	var v diskPowerView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if v.Stored || v.Settings.Profile != diskpower.Balanced || v.Settings.StandbyMinutes != 0 || !v.SystemDisk ||
		v.State != diskpower.StateActive || len(v.Profiles) != 3 {
		t.Fatalf("view %+v", v)
	}
	if rec := do(h, "GET", "/api/v1/storage/disks/usb-stick/power", "", nil, c); rec.Code != 404 ||
		!strings.Contains(rec.Body.String(), "power_unsupported") {
		t.Fatalf("usb: %d %s", rec.Code, rec.Body)
	}
}

func TestDiskPowerPut(t *testing.T) {
	p := &fakePower{cfg: diskpower.Config{Disks: map[string]diskpower.Settings{}}}
	h, c := newPowerServer(t, p)
	url := "/api/v1/storage/disks/wwn-0x1/power"
	if rec := do(h, "PUT", url, `{"profile":"performance"}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("without CSRF = %d", rec.Code)
	}
	// Spinning down the system disk needs the explicit confirmation.
	rec := do(h, "PUT", url, `{"profile":"eco"}`, xrw, c)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "confirm_required") || len(p.sets) != 0 {
		t.Fatalf("unconfirmed: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "PUT", url, `{"profile":"eco","confirmSystemDisk":true}`, xrw, c)
	var v diskPowerView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil || !v.Stored || v.Settings.Profile != diskpower.Eco {
		t.Fatalf("eco: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "PUT", url, `{"profile":"custom","standbyMinutes":0,"apm":200}`, xrw, c)
	if rec.Code != 200 || *p.sets[len(p.sets)-1].APM != 200 {
		t.Fatalf("custom: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{"profile":"turbo"}`, `{"profile":"custom","standbyMinutes":7}`,
		`{"profile":"custom","apm":300}`, `{"profile":"performance","device":"/dev/sda"}`} {
		if rec := do(h, "PUT", url, bad, xrw, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	p.err = &helper.Error{Code: helper.CodeHelperUnavailable, Message: "x"}
	if rec := do(h, "PUT", url, `{"profile":"performance"}`, xrw, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("helper down: %d", rec.Code)
	}
}

func TestDiskStandby(t *testing.T) {
	p := &fakePower{cfg: diskpower.Config{Disks: map[string]diskpower.Settings{}}}
	h, c := newPowerServer(t, p)
	rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/standby", "", xrw, c)
	if rec.Code != 200 || len(p.sleep) != 1 || p.sleep[0] != "wwn-0x1" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, p.sleep)
	}
	p.err = &helper.Error{Code: helper.CodeTestRunning, Message: "x"}
	if rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/standby", "", xrw, c); rec.Code != http.StatusConflict {
		t.Fatalf("self-test running: %d", rec.Code)
	}
}
