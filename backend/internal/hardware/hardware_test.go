package hardware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	if err := Validate(Defaults()); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	bad := map[string]func(*Settings){
		"fanProfile": func(s *Settings) { s.FanProfile = "turbo" },
		"boardWarnC": func(s *Settings) { s.Thresholds.BoardWarnC = 30 },
		"boardCritC": func(s *Settings) { s.Thresholds.BoardCritC = s.Thresholds.BoardWarnC },
		"diskWarnC":  func(s *Settings) { s.Thresholds.DiskWarnC = 70 },
		"diskCritC":  func(s *Settings) { s.Thresholds.DiskCritC = 80 },
		"start":      func(s *Settings) { s.LEDs.Night.Start = "24:00" },
		"end":        func(s *Settings) { s.LEDs.Night.End = s.LEDs.Night.Start },
	}
	for field, mut := range bad {
		s := Defaults()
		mut(&s)
		var fe *FieldError
		if err := Validate(s); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v", field, err)
		}
	}
}

func TestLoadMissingAndPartial(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hardware.json")
	if s, err := Load(p); err != nil || s != Defaults() {
		t.Fatalf("missing: %+v, %v", s, err)
	}
	os.WriteFile(p, []byte(`{"fanProfile":"quiet"}`), 0o644)
	s, err := Load(p)
	if err != nil || s.FanProfile != ProfileQuiet || s.Thresholds != Defaults().Thresholds {
		t.Fatalf("partial: %+v, %v", s, err)
	}
	os.WriteFile(p, []byte(`{"fanProfile":"turbo"}`), 0o644)
	if s, err := Load(p); err == nil || s != Defaults() {
		t.Fatalf("invalid: %+v, %v", s, err)
	}
}

func TestRender(t *testing.T) {
	q, _ := ProfileByID(ProfileQuiet)
	if got := RenderFanDefaults(q); !strings.Contains(got, "\nZONE_MIN=45000\nZONE_MAX=65000\nPWM_MIN=52\n") {
		t.Errorf("fan defaults:\n%s", got)
	}
	n := NightMode{Enabled: true, Start: "22:30", End: "06:45"}
	if got := RenderLEDDefaults(n); !strings.HasSuffix(got, "NIGHT_START=2230\nNIGHT_END=0645\n") {
		t.Errorf("led defaults:\n%s", got)
	}
	n.Enabled = false
	if got := RenderLEDDefaults(n); !strings.HasSuffix(got, "NIGHT_START=\nNIGHT_END=\n") {
		t.Errorf("led defaults off:\n%s", got)
	}
	if got := RenderLEDTimer(NightMode{Start: "22:30", End: "06:45"}); !strings.Contains(got, "OnCalendar=*-*-* 22:30:00\nOnCalendar=*-*-* 06:45:00\n") {
		t.Errorf("timer:\n%s", got)
	}
	// Every profile keeps the 20 °C LM85 range and a floor above the stall point.
	for _, p := range Profiles {
		if p.ZoneMaxC-p.ZoneMinC != 20 || p.PWMMin <= 40 || p.ZoneMaxC >= ZoneCritC {
			t.Errorf("unsafe profile %+v", p)
		}
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, v := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadFanAndLEDs(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"sys/class/hwmon/hwmon0/name":                "rtc",
		"sys/class/hwmon/hwmon1/name":                "lm85",
		"sys/class/hwmon/hwmon1/pwm1_enable":         "2",
		"sys/class/hwmon/hwmon1/pwm1":                "98",
		"sys/class/hwmon/hwmon1/temp1_auto_temp_min": "40000",
		"sys/class/hwmon/hwmon1/temp1_auto_temp_max": "60000",
		"sys/class/hwmon/hwmon1/pwm1_auto_pwm_min":   "64",
		"sys/class/leds/nsa310:red:sys/trigger":      "none timer [panic] netdev",
		"sys/class/leds/nsa310:red:sys/brightness":   "0",
		"sys/class/leds/nsa310:green:sys/trigger":    "none [default-on]",
		"sys/class/leds/nsa310:green:sys/brightness": "1",
	})
	f := ReadFan(root)
	if f.Mode != FanAuto || *f.PWM != 98 || *f.PWMPercent != 38 || f.Profile != ProfileBalanced {
		t.Fatalf("fan = %+v", f)
	}
	writeFiles(t, root, map[string]string{"sys/class/hwmon/hwmon1/pwm1_auto_pwm_min": "70"})
	if f := ReadFan(root); f.Profile != "" {
		t.Errorf("hand-set limits matched profile %q", f.Profile)
	}
	if f := ReadFan(t.TempDir()); f.Mode != FanUnknown || f.PWM != nil {
		t.Errorf("no chip: %+v", f)
	}
	l := ReadLEDs(root)
	if len(l) != 2 || l[0].Name != "nsa310:green:sys" || !l[0].On || l[0].Trigger != "default-on" ||
		!l[1].Red || l[1].Trigger != "panic" {
		t.Errorf("leds = %+v", l)
	}
}

func TestHistoryKeepsMinuteMaximum(t *testing.T) {
	h := NewHistory(nil)
	h.Size = 3
	base := time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC)
	rpm := func(v int64) *int64 { return &v }
	h.Add(base, Reading{Temps: map[string]float64{"a": 40}, FanRPM: rpm(4000)})
	h.Add(base.Add(20*time.Second), Reading{Temps: map[string]float64{"a": 44}, FanRPM: rpm(4500)})
	h.Add(base.Add(50*time.Second), Reading{Temps: map[string]float64{"a": 41}})
	pts := h.Since(0)
	if len(pts) != 1 || pts[0].Temps["a"] != 44 || *pts[0].FanRPM != 4500 {
		t.Fatalf("running minute = %+v", pts)
	}
	for i := 1; i <= 4; i++ {
		h.Add(base.Add(time.Duration(i)*time.Minute), Reading{Temps: map[string]float64{"a": float64(40 + i)}})
	}
	pts = h.Since(0)
	// Ring of 3 closed minutes (1..3) plus the running minute 4.
	if len(pts) != 4 || pts[0].T != base.Add(time.Minute).UnixMilli() || pts[3].Temps["a"] != 44 {
		t.Fatalf("points = %+v", pts)
	}
	if got := h.Since(pts[2].T); len(got) != 1 || got[0].T != pts[3].T {
		t.Errorf("since = %+v", got)
	}
	pts[3].Temps["a"] = 99 // callers get copies
	if h.Since(pts[2].T)[0].Temps["a"] != 44 {
		t.Error("history shares its maps with callers")
	}
}

func TestWatcherSendsLEDOnChangeOnly(t *testing.T) {
	var sent []bool
	fail := false
	s := Defaults()
	w := &Watcher{Settings: func() Settings { return s }, SetFault: func(_ context.Context, on bool) error {
		sent = append(sent, on)
		if fail {
			return errors.New("helper down")
		}
		return nil
	}}
	ctx := context.Background()
	w.Update(ctx, false, nil)
	w.Update(ctx, false, nil)
	w.Update(ctx, true, map[string]string{"tempCrit:Dioda 2": "crit"})
	w.Update(ctx, true, map[string]string{"tempCrit:Dioda 2": "crit"})
	if len(sent) != 2 || sent[0] || !sent[1] {
		t.Fatalf("sent = %v", sent)
	}
	// A failed call is repeated on the next evaluation.
	fail = true
	w.Update(ctx, false, nil)
	fail = false
	w.Update(ctx, false, nil)
	if len(sent) != 4 || sent[3] {
		t.Fatalf("retry: sent = %v", sent)
	}
	// Fault blinking switched off: a critical state leaves the LED alone.
	s.LEDs.FaultBlink = false
	w.Update(ctx, true, nil)
	if len(sent) != 4 {
		t.Errorf("blink off: sent = %v", sent)
	}
}
