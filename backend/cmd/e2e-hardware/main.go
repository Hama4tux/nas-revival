// Command e2e-hardware is the W3.4 end-to-end check on the NAS (scripts/webui/e2e-hardware.sh runs it as
// nas-revival). It reads the LM85 and the LEDs the way the web process does, switches the fan profile to
// quiet and back (the LM85 limits are read back), switches night mode on for a window around now and back
// to the previous LED settings, and blinks the red system LED for 3 s. Thresholds are written back unchanged.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"nas-revival/backend/internal/hardware"
	"nas-revival/backend/internal/helper"
)

var fails int

func check(ok bool, format string, a ...any) {
	status := "ok  "
	if !ok {
		status, fails = "FAIL", fails+1
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, a...))
}

func trigger(leds []hardware.LED, name string) string {
	for _, l := range leds {
		if l.Name == name {
			return l.Trigger
		}
	}
	return "missing"
}

func main() {
	ctx := helper.WithUser(context.Background(), "w34-e2e")
	c := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock"}
	orig, err := hardware.Load(hardware.DefaultConfigPath)
	check(err == nil, "hardware.json readable by nas-revival: %+v, err %v", orig, err)

	f := hardware.ReadFan("/")
	check(f.Mode == hardware.FanAuto && f.PWM != nil, "fan mode %s, pwm %v %%, profile %q", f.Mode, deref(f.PWMPercent), f.Profile)
	leds := hardware.ReadLEDs("/")
	check(len(leds) >= 9, "%d LEDs, red:sys trigger %s", len(leds), trigger(leds, "nsa310:red:sys"))

	_, err = c.FanProfile(ctx, hardware.ProfileQuiet)
	f = hardware.ReadFan("/")
	check(err == nil && f.Profile == hardware.ProfileQuiet && f.Mode == hardware.FanAuto,
		"quiet profile: err %v, chip %d–%d °C, pwm_min %d", err, deref(f.ZoneMinC), deref(f.ZoneMaxC), deref(f.PWMMin))
	_, err = c.FanProfile(ctx, orig.FanProfile)
	f = hardware.ReadFan("/")
	check(err == nil && f.Profile == orig.FanProfile, "back to %s: err %v, chip profile %q", orig.FanProfile, err, f.Profile)

	now := time.Now()
	on := orig.LEDs
	on.Night = hardware.NightMode{Enabled: true, Start: now.Add(-time.Minute).Format("15:04"), End: now.Add(10 * time.Minute).Format("15:04")}
	_, err = c.LEDSettings(ctx, on)
	leds = hardware.ReadLEDs("/")
	check(err == nil && trigger(leds, "nsa310:green:sys") == "none" && trigger(leds, "nsa310:red:sys") == "panic",
		"night now (%s–%s): err %v, green:sys %s, red:sys %s", on.Night.Start, on.Night.End, err,
		trigger(leds, "nsa310:green:sys"), trigger(leds, "nsa310:red:sys"))
	_, statErr := os.Stat(hardware.LEDTimerPath)
	check(statErr == nil, "switch timer written: %v", statErr)
	_, err = c.LEDSettings(ctx, orig.LEDs)
	leds = hardware.ReadLEDs("/")
	check(err == nil && trigger(leds, "nsa310:green:sys") == "default-on", "LED settings restored: err %v, green:sys %s", err, trigger(leds, "nsa310:green:sys"))

	err = c.LEDFault(ctx, true)
	leds = hardware.ReadLEDs("/")
	check(err == nil && trigger(leds, "nsa310:red:sys") == "timer", "fault blink on: err %v, red:sys %s", err, trigger(leds, "nsa310:red:sys"))
	time.Sleep(3 * time.Second)
	err = c.LEDFault(ctx, false)
	leds = hardware.ReadLEDs("/")
	check(err == nil && trigger(leds, "nsa310:red:sys") == "panic", "fault blink off: err %v, red:sys %s", err, trigger(leds, "nsa310:red:sys"))

	_, err = c.HardwareThresholds(ctx, orig.Thresholds)
	check(err == nil, "thresholds written back: %v", err)
	bad := orig.Thresholds
	bad.DiskCritC = bad.DiskWarnC
	_, err = c.HardwareThresholds(ctx, bad)
	he, _ := helper.AsError(err)
	check(he != nil && he.Code == helper.CodeInvalid, "crit = warn refused by the helper: %v", err)

	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}

func deref(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}
