package hardware

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Fan control modes from pwm1_enable (lm85: 0 full speed, 1 manual, 2 automatic).
const (
	FanAuto    = "auto"
	FanManual  = "manual"
	FanFull    = "full"
	FanUnknown = "unknown"
)

// FanState is what the LM85 does now, read back from sysfs, so the page shows
// the chip's real limits even if somebody changed them by hand.
type FanState struct {
	Mode       string `json:"mode"`
	PWM        *int   `json:"pwm"`        // 0–255
	PWMPercent *int   `json:"pwmPercent"` // rounded
	ZoneMinC   *int   `json:"zoneMinC"`
	ZoneMaxC   *int   `json:"zoneMaxC"`
	PWMMin     *int   `json:"pwmMin"`
	// Profile is the profile whose limits match the chip, "" when none does
	// (set by hand, or the boot script failed).
	Profile string `json:"profile"`
}

// LED is one LED class device; Trigger is the active one ("none" = manual).
type LED struct {
	Name    string `json:"name"`
	Trigger string `json:"trigger"`
	On      bool   `json:"on"`
	Red     bool   `json:"red"`
}

func readInt(p string) (int, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return n, err == nil
}

// lm85Dir finds the LM85 by driver name, never by hwmon index.
func lm85Dir(root string) string {
	devs, _ := filepath.Glob(filepath.Join(root, "sys/class/hwmon/hwmon*"))
	for _, d := range devs {
		if b, err := os.ReadFile(filepath.Join(d, "name")); err == nil && strings.TrimSpace(string(b)) == "lm85" {
			return d
		}
	}
	return ""
}

// ReadFan reads pwm1 of the LM85; Mode is unknown without the chip.
func ReadFan(root string) FanState {
	st := FanState{Mode: FanUnknown}
	d := lm85Dir(root)
	if d == "" {
		return st
	}
	if e, ok := readInt(filepath.Join(d, "pwm1_enable")); ok {
		switch e {
		case 0:
			st.Mode = FanFull
		case 1:
			st.Mode = FanManual
		case 2:
			st.Mode = FanAuto
		}
	}
	ptr := func(n int, ok bool) *int {
		if !ok {
			return nil
		}
		return &n
	}
	if v, ok := readInt(filepath.Join(d, "pwm1")); ok {
		st.PWM = &v
		pct := (v*100 + 127) / 255
		st.PWMPercent = &pct
	}
	if v, ok := readInt(filepath.Join(d, "temp1_auto_temp_min")); ok {
		st.ZoneMinC = ptr(v/1000, true)
	}
	if v, ok := readInt(filepath.Join(d, "temp1_auto_temp_max")); ok {
		st.ZoneMaxC = ptr(v/1000, true)
	}
	st.PWMMin = ptr(readInt(filepath.Join(d, "pwm1_auto_pwm_min")))
	if st.Mode == FanAuto && st.ZoneMinC != nil && st.ZoneMaxC != nil && st.PWMMin != nil {
		for _, p := range Profiles {
			if p.ZoneMinC == *st.ZoneMinC && p.ZoneMaxC == *st.ZoneMaxC && p.PWMMin == *st.PWMMin {
				st.Profile = p.ID
			}
		}
	}
	return st
}

// ReadLEDs lists the LED class devices with their active trigger.
func ReadLEDs(root string) []LED {
	out := []LED{}
	dirs, _ := filepath.Glob(filepath.Join(root, "sys/class/leds/*"))
	for _, d := range dirs {
		name := filepath.Base(d)
		l := LED{Name: name, Trigger: "none", Red: strings.Contains(name, ":red:")}
		if b, err := os.ReadFile(filepath.Join(d, "trigger")); err == nil {
			for _, f := range strings.Fields(string(b)) {
				if strings.HasPrefix(f, "[") && strings.HasSuffix(f, "]") {
					l.Trigger = strings.Trim(f, "[]")
				}
			}
		}
		// A netdev or disk trigger flashes the LED; brightness tells whether it
		// is lit right now, the trigger whether it is in use at all.
		if v, ok := readInt(filepath.Join(d, "brightness")); ok {
			l.On = v > 0
		}
		out = append(out, l)
	}
	return out
}

// Sensor kinds of State.Sensors.
const (
	KindBoard = "board"
	KindDisk  = "disk"
)

// Sensor is one temperature: a hwmon channel ("lm85/temp1") or a disk from
// the cached SMART report ("disk/sda"; C is null while the disk sleeps).
type Sensor struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	C       *float64 `json:"c"`
	Standby bool     `json:"standby,omitempty"`
}

// State is the response of GET /hardware.
type State struct {
	Settings  Settings     `json:"settings"`
	Profiles  []FanProfile `json:"profiles"`
	ZoneCritC int          `json:"zoneCritC"`
	Fan       FanState     `json:"fan"`
	FanRPM    *int64       `json:"fanRpm"`
	Sensors   []Sensor     `json:"sensors"`
	LEDs      []LED        `json:"leds"`
}
