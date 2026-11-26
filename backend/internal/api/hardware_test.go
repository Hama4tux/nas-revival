package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/hardware"
)

type fakeHardware struct {
	since   []int64
	profile string
	th      *hardware.Thresholds
	leds    *hardware.LEDs
}

func (f *fakeHardware) State(context.Context) hardware.State {
	return hardware.State{Settings: hardware.Defaults(), Profiles: hardware.Profiles, Sensors: []hardware.Sensor{}, LEDs: []hardware.LED{}}
}

func (f *fakeHardware) History(since int64) []hardware.Point {
	f.since = append(f.since, since)
	return []hardware.Point{{T: since + 60000, Temps: map[string]float64{"lm85/temp1": 40}}}
}

func (f *fakeHardware) HardwareThresholds(_ context.Context, t hardware.Thresholds) (hardware.Settings, error) {
	f.th = &t
	return hardware.Defaults(), nil
}

func (f *fakeHardware) FanProfile(_ context.Context, p string) (hardware.Settings, error) {
	f.profile = p
	return hardware.Defaults(), nil
}

func (f *fakeHardware) LEDSettings(_ context.Context, l hardware.LEDs) (hardware.Settings, error) {
	f.leds = &l
	return hardware.Defaults(), nil
}

func TestHardwareEndpoints(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	h := s.Handler()
	admin := loginAs(t, h, "jana")
	if rec := do(h, "GET", "/api/v1/hardware", "", xrw, admin); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no source = %d", rec.Code)
	}
	f := &fakeHardware{}
	s.Hardware, s.HardwareAdmin = f, f
	h = s.Handler()
	for name, want := range map[string]int{"jana": 200, "petr": 200, "eva": 403} {
		if rec := do(h, "GET", "/api/v1/hardware", "", xrw, loginAs(t, h, name)); rec.Code != want {
			t.Errorf("state as %s = %d, want %d", name, rec.Code, want)
		}
	}
	if rec := do(h, "GET", "/api/v1/hardware/history?since=120000", "", xrw, admin); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"intervalSeconds":60`) || f.since[0] != 120000 {
		t.Errorf("history = %d %s %v", rec.Code, rec.Body, f.since)
	}
	if rec := do(h, "GET", "/api/v1/hardware/history?since=x", "", xrw, admin); rec.Code != 400 {
		t.Errorf("bad since = %d", rec.Code)
	}

	// The read-only administrator cannot change anything.
	viewer := loginAs(t, h, "petr")
	if rec := do(h, "PUT", "/api/v1/hardware/fan", `{"profile":"quiet"}`, xrw, viewer); rec.Code != 403 || f.profile != "" {
		t.Errorf("viewer fan = %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/hardware/fan", `{"profile":"quiet"}`, xrw, admin); rec.Code != 200 || f.profile != "quiet" {
		t.Errorf("fan = %d %q", rec.Code, f.profile)
	}
	if rec := do(h, "PUT", "/api/v1/hardware/fan", `{"profile":"turbo"}`, xrw, admin); rec.Code != 400 {
		t.Errorf("unknown profile = %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/hardware/thresholds", `{"boardWarnC":60,"boardCritC":60,"diskWarnC":50,"diskCritC":55}`, xrw, admin); rec.Code != 400 ||
		!strings.Contains(rec.Body.String(), `"field":"boardCritC"`) || f.th != nil {
		t.Errorf("crit = warn: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/hardware/thresholds", `{"boardWarnC":58,"boardCritC":68,"diskWarnC":48,"diskCritC":54}`, xrw, admin); rec.Code != 200 || f.th.DiskWarnC != 48 {
		t.Errorf("thresholds = %d", rec.Code)
	}
	body := `{"night":{"enabled":true,"start":"22:00","end":"06:00"},"faultBlink":false}`
	if rec := do(h, "PUT", "/api/v1/hardware/leds", body, xrw, admin); rec.Code != 200 || !f.leds.Night.Enabled || f.leds.FaultBlink {
		t.Errorf("leds = %d %+v", rec.Code, f.leds)
	}
	if rec := do(h, "PUT", "/api/v1/hardware/leds", `{"night":{"enabled":true,"start":"22:00","end":"22:00"}}`, xrw, admin); rec.Code != 400 {
		t.Errorf("same times = %d", rec.Code)
	}
}
