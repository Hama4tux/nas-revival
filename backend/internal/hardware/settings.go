// Package hardware holds the hardware settings of W3.4 (HW-01, HW-02, HW-04):
// temperature thresholds, the fan profile and the LED night mode. The helper
// writes hardware.json and renders the files the board scripts read; the web
// process only reads hardware.json.
package hardware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
)

// Files on the NAS. The board scripts (deploy/debian/nsa310-fan, nsa310-leds)
// source the /etc/default files; both are rendered only from validated settings.
const (
	DefaultConfigPath = "/etc/nas-revival/hardware.json"
	FanDefaultsPath   = "/etc/default/nsa310-fan"
	LEDDefaultsPath   = "/etc/default/nsa310-leds"
	LEDTimerPath      = "/etc/systemd/system/nsa310-leds.timer"
)

// Thresholds in °C; a reading at or above warn/crit raises a health problem.
type Thresholds struct {
	BoardWarnC int `json:"boardWarnC"`
	BoardCritC int `json:"boardCritC"`
	DiskWarnC  int `json:"diskWarnC"`
	DiskCritC  int `json:"diskCritC"`
}

// NightMode switches the green and LAN LEDs off between Start and End
// ("HH:MM", local time; End before Start crosses midnight). Red LEDs stay.
type NightMode struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

type LEDs struct {
	Night NightMode `json:"night"`
	// FaultBlink blinks the red system LED while the health is critical.
	FaultBlink bool `json:"faultBlink"`
}

type Settings struct {
	FanProfile string     `json:"fanProfile"`
	Thresholds Thresholds `json:"thresholds"`
	LEDs       LEDs       `json:"leds"`
}

// FanProfile maps a named profile to LM85 automatic-mode limits: minimum PWM
// below ZoneMinC, linear ramp to full speed at ZoneMaxC. Every profile keeps
// the 20 °C range the chip supports exactly and a PWM floor above the
// measured stall point (40/255, 2026-11-25).
type FanProfile struct {
	ID       string `json:"id"`
	ZoneMinC int    `json:"zoneMinC"`
	ZoneMaxC int    `json:"zoneMaxC"`
	PWMMin   int    `json:"pwmMin"` // of 255
}

const (
	ProfileQuiet       = "quiet"
	ProfileBalanced    = "balanced"
	ProfilePerformance = "performance"
)

// Profiles in UI order. Balanced is what nsa310-fan used before W3.4.
var Profiles = []FanProfile{
	{ID: ProfileQuiet, ZoneMinC: 45, ZoneMaxC: 65, PWMMin: 52},
	{ID: ProfileBalanced, ZoneMinC: 40, ZoneMaxC: 60, PWMMin: 64},
	{ID: ProfilePerformance, ZoneMinC: 35, ZoneMaxC: 55, PWMMin: 102},
}

// ZoneCritC: all fans at full speed (LM85 temp_auto_temp_crit), every profile.
const ZoneCritC = 70

func ProfileByID(id string) (FanProfile, bool) {
	for _, p := range Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return FanProfile{}, false
}

func Defaults() Settings {
	return Settings{
		FanProfile: ProfileBalanced,
		Thresholds: Thresholds{BoardWarnC: 55, BoardCritC: 65, DiskWarnC: 50, DiskCritC: 55},
		LEDs:       LEDs{Night: NightMode{Start: "22:00", End: "07:00"}, FaultBlink: true},
	}
}

// FieldError names the invalid field.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Validate checks every field; the limits keep thresholds meaningful for a
// small NAS (a board warning below 40 °C would fire all summer).
func Validate(s Settings) error {
	if _, ok := ProfileByID(s.FanProfile); !ok {
		return &FieldError{"fanProfile", "Neznámý profil ventilátoru."}
	}
	t := s.Thresholds
	if t.BoardWarnC < 40 || t.BoardWarnC > 80 {
		return &FieldError{"boardWarnC", "Varování pro desku musí být 40–80 °C."}
	}
	if t.BoardCritC <= t.BoardWarnC || t.BoardCritC > 90 {
		return &FieldError{"boardCritC", "Kritická teplota desky musí být nad varováním a nejvýš 90 °C."}
	}
	if t.DiskWarnC < 35 || t.DiskWarnC > 65 {
		return &FieldError{"diskWarnC", "Varování pro disk musí být 35–65 °C."}
	}
	if t.DiskCritC <= t.DiskWarnC || t.DiskCritC > 70 {
		return &FieldError{"diskCritC", "Kritická teplota disku musí být nad varováním a nejvýš 70 °C."}
	}
	n := s.LEDs.Night
	if !hhmm.MatchString(n.Start) {
		return &FieldError{"start", "Začátek nočního režimu zadejte jako HH:MM."}
	}
	if !hhmm.MatchString(n.End) {
		return &FieldError{"end", "Konec nočního režimu zadejte jako HH:MM."}
	}
	if n.Start == n.End {
		return &FieldError{"end", "Začátek a konec nočního režimu se nesmí shodovat."}
	}
	return nil
}

// Load reads hardware.json; a missing file gives the defaults, missing
// fields keep their defaults.
func Load(path string) (Settings, error) {
	s := Defaults()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Defaults(), fmt.Errorf("parse %s: %w", path, err)
	}
	if err := Validate(s); err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Marshal is the on-disk form of hardware.json.
func Marshal(s Settings) []byte {
	b, _ := json.MarshalIndent(s, "", "  ")
	return append(b, '\n')
}

// RenderFanDefaults is /etc/default/nsa310-fan for profile p (millidegrees,
// as the lm85 sysfs files take them).
func RenderFanDefaults(p FanProfile) string {
	return fmt.Sprintf("# Rendered by nas-revival from hardware.json (profile %s). Do not edit.\n"+
		"ZONE_MIN=%d\nZONE_MAX=%d\nPWM_MIN=%d\n", p.ID, p.ZoneMinC*1000, p.ZoneMaxC*1000, p.PWMMin)
}

func compact(hm string) string { return hm[:2] + hm[3:] }

// RenderLEDDefaults is /etc/default/nsa310-leds. Times are HHMM so the script
// can compare them with date +%H%M; empty values mean night mode is off.
func RenderLEDDefaults(n NightMode) string {
	start, end := "", ""
	if n.Enabled {
		start, end = compact(n.Start), compact(n.End)
	}
	return "# Rendered by nas-revival from hardware.json. Do not edit.\n" +
		"NIGHT_START=" + start + "\nNIGHT_END=" + end + "\n"
}

// RenderLEDTimer re-runs nsa310-leds.service at both switch times; the
// script decides day or night from the clock, so a missed run (NAS off at
// 22:00) is corrected at boot.
func RenderLEDTimer(n NightMode) string {
	return "# Rendered by nas-revival from hardware.json. Do not edit.\n" +
		"[Unit]\nDescription=NSA310 LED night mode switch\n\n" +
		"[Timer]\nOnCalendar=*-*-* " + n.Start + ":00\nOnCalendar=*-*-* " + n.End + ":00\n" +
		"AccuracySec=1s\n\n[Install]\nWantedBy=timers.target\n"
}
