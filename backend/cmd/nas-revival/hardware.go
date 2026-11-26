package main

import (
	"context"
	"math"
	"sort"
	"strings"

	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/hardware"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/system"
)

// hardwareSource joins hwmon, the LM85 fan state, the cached SMART report and
// hardware.json for the W3.4 hardware page and its 24 h history.
type hardwareSource struct {
	root     string
	hal      hal.Reader
	profile  system.Profile
	smart    *smart.Service
	settings *hardware.Cached
	history  *hardware.History
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func (h *hardwareSource) sensors(ctx context.Context) ([]hardware.Sensor, *int64) {
	out := []hardware.Sensor{}
	s := h.hal.Sensors()
	for _, t := range s.Temperatures {
		label := t.Label
		if l, ok := h.profile.Temps[t.ID]; ok {
			label = l
		}
		c := round1(float64(t.MilliCelsius) / 1000)
		out = append(out, hardware.Sensor{ID: t.ID, Label: label, Kind: hardware.KindBoard, C: &c})
	}
	var rpm *int64
	for _, f := range s.Fans {
		if _, wired := h.profile.Fans[f.ID]; wired || len(h.profile.Fans) == 0 {
			v := f.RPM
			rpm = &v
			break
		}
	}
	if h.smart != nil {
		rep := h.smart.Report(ctx) // cached; the collector refreshes it anyway
		disks := append([]smart.Disk(nil), rep.Disks...)
		sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
		for _, d := range disks {
			sn := hardware.Sensor{ID: "disk/" + d.Name, Label: d.Name, Kind: hardware.KindDisk, Standby: d.Standby}
			if d.Model != nil {
				sn.Label = d.Name + " · " + strings.TrimSpace(*d.Model)
			}
			if d.TemperatureCelsius != nil && !d.Standby {
				c := float64(*d.TemperatureCelsius)
				sn.C = &c
			}
			out = append(out, sn)
		}
	}
	return out, rpm
}

func (h *hardwareSource) State(ctx context.Context) hardware.State {
	sensors, rpm := h.sensors(ctx)
	return hardware.State{
		Settings: h.settings.Get(), Profiles: hardware.Profiles, ZoneCritC: hardware.ZoneCritC,
		Fan: hardware.ReadFan(h.root), FanRPM: rpm, Sensors: sensors, LEDs: hardware.ReadLEDs(h.root),
	}
}

func (h *hardwareSource) History(sinceMs int64) []hardware.Point { return h.history.Since(sinceMs) }

// reading is one history sample.
func (h *hardwareSource) reading(ctx context.Context) hardware.Reading {
	sensors, rpm := h.sensors(ctx)
	r := hardware.Reading{Temps: map[string]float64{}, FanRPM: rpm, PWMPercent: hardware.ReadFan(h.root).PWMPercent}
	for _, s := range sensors {
		if s.C != nil {
			r.Temps[s.ID] = *s.C
		}
	}
	return r
}

// temperatureProblems picks the HW-01 problems for the watcher (events, fault LED).
func temperatureProblems(hl system.Health) map[string]string {
	out := map[string]string{}
	for _, p := range hl.Problems {
		switch p.Code {
		case system.ProblemTempWarn, system.ProblemTempCrit, system.ProblemDiskTempWarn, system.ProblemDiskTempCrit:
			out[p.Code+":"+p.Subject] = p.Severity
		}
	}
	return out
}
