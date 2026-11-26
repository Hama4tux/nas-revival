package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/hardware"
)

type hwRun struct {
	calls []string
	fail  map[string]int // command → number of failing calls left
}

func (r *hwRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	c := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, c)
	if r.fail[c] > 0 {
		r.fail[c]--
		return nil, errors.New("exit status 1")
	}
	return nil, nil
}

func newHW(t *testing.T) (*HardwareManager, *hwRun) {
	dir := t.TempDir()
	r := &hwRun{fail: map[string]int{}}
	return &HardwareManager{Path: filepath.Join(dir, "nas-revival/hardware.json"), FanDefaults: filepath.Join(dir, "fan"),
		LEDDefaults: filepath.Join(dir, "leds"), LEDTimer: filepath.Join(dir, "nsa310-leds.timer"), Run: r.run}, r
}

func TestSetFanProfile(t *testing.T) {
	m, r := newHW(t)
	s, err := m.SetFanProfile(context.Background(), hardware.ProfileQuiet, "admin")
	if err != nil || s.FanProfile != hardware.ProfileQuiet {
		t.Fatalf("set = %+v, %v", s, err)
	}
	if b, _ := os.ReadFile(m.FanDefaults); !strings.Contains(string(b), "ZONE_MIN=45000") {
		t.Errorf("defaults = %s", b)
	}
	if got, _ := hardware.Load(m.Path); got.FanProfile != hardware.ProfileQuiet {
		t.Errorf("saved = %+v", got)
	}
	// The script rejects the new profile: the quiet one is put back and re-run.
	r.calls = nil
	r.fail["systemctl restart nsa310-fan.service"] = 1
	if _, err := m.SetFanProfile(context.Background(), hardware.ProfilePerformance, "admin"); codeOf(err) != CodeFanApplyFailed {
		t.Fatalf("rejected: %v", err)
	}
	if b, _ := os.ReadFile(m.FanDefaults); !strings.Contains(string(b), "ZONE_MIN=45000") || len(r.calls) != 2 {
		t.Errorf("not restored: %s, calls %q", b, r.calls)
	}
	if got, _ := hardware.Load(m.Path); got.FanProfile != hardware.ProfileQuiet {
		t.Errorf("failed profile saved: %+v", got)
	}
	if _, err := m.SetFanProfile(context.Background(), "turbo", "admin"); codeOf(err) != CodeInvalid {
		t.Errorf("unknown profile: %v", err)
	}
}

func TestSetLEDsNightModeTimer(t *testing.T) {
	m, r := newHW(t)
	on := hardware.LEDs{Night: hardware.NightMode{Enabled: true, Start: "23:00", End: "06:30"}, FaultBlink: true}
	if _, err := m.SetLEDs(context.Background(), on, "admin"); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl daemon-reload", "systemctl enable nsa310-leds.timer", "systemctl restart nsa310-leds.timer", "systemctl start nsa310-leds.service"}
	if strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Errorf("enable calls = %q", r.calls)
	}
	if b, _ := os.ReadFile(m.LEDTimer); !strings.Contains(string(b), "OnCalendar=*-*-* 23:00:00") {
		t.Errorf("timer = %s", b)
	}
	r.calls = nil
	off := on
	off.Night.Enabled = false
	if _, err := m.SetLEDs(context.Background(), off, "admin"); err != nil {
		t.Fatal(err)
	}
	want = []string{"systemctl disable --now nsa310-leds.timer", "systemctl daemon-reload", "systemctl start nsa310-leds.service"}
	if strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Errorf("disable calls = %q", r.calls)
	}
	if _, err := os.Stat(m.LEDTimer); err == nil {
		t.Error("timer file left behind")
	}
	if b, _ := os.ReadFile(m.LEDDefaults); !strings.Contains(string(b), "NIGHT_START=\n") {
		t.Errorf("defaults = %s", b)
	}
	bad := on
	bad.Night.End = "7:00"
	if _, err := m.SetLEDs(context.Background(), bad, "admin"); codeOf(err) != CodeInvalid {
		t.Errorf("bad time: %v", err)
	}
}

func TestSetThresholdsAndFault(t *testing.T) {
	m, r := newHW(t)
	th := hardware.Defaults().Thresholds
	th.DiskWarnC = 48
	if s, err := m.SetThresholds(context.Background(), th, "admin"); err != nil || s.Thresholds.DiskWarnC != 48 {
		t.Fatalf("set = %+v, %v", s, err)
	}
	th.BoardCritC = th.BoardWarnC
	if _, err := m.SetThresholds(context.Background(), th, "admin"); codeOf(err) != CodeInvalid {
		t.Errorf("crit = warn: %v", err)
	}
	if err := m.SetFault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	r.fail["systemctl start nsa310-led-fault@off.service"] = 1
	if err := m.SetFault(context.Background(), false); codeOf(err) != CodeInternal {
		t.Errorf("unit failed: %v", err)
	}
	if strings.Join(r.calls, "|") != "systemctl start nsa310-led-fault@on.service|systemctl start nsa310-led-fault@off.service" {
		t.Errorf("calls = %q", r.calls)
	}
}
