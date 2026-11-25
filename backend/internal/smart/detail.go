package smart

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Attribute levels, worst last.
const (
	LevelOK   = "ok"
	LevelInfo = "info" // key attribute, nothing wrong
	LevelWarn = "warn"
	LevelCrit = "crit"
)

// keyAttributes are shown first and always evaluated (SMART-10).
var keyAttributes = map[int]bool{5: true, 9: true, 4: true, 10: true, 12: true, 187: true, 188: true,
	193: true, 194: true, 197: true, 198: true, 199: true}

// loadCycleWarn is a conservative share of the 300 000–600 000 cycles laptop
// drives are rated for.
const loadCycleWarn = 300_000

type Attribute struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Value      *int   `json:"value"` // normalised, higher is better
	Worst      *int   `json:"worst"`
	Thresh     *int   `json:"thresh"`
	Raw        string `json:"raw"` // smartctl's decoded text
	RawValue   int64  `json:"rawValue"`
	Count      int64  `json:"count"` // the meaningful number after vendor decoding
	Prefailure bool   `json:"prefailure"`
	WhenFailed string `json:"whenFailed"` // "", "past" or "now"
	Key        bool   `json:"key"`
	Level      string `json:"level"`
}

// SelfTestStatus is the current or last self-test state of the drive.
type SelfTestStatus struct {
	Value            int    `json:"value"` // ATA self-test execution status byte
	Text             string `json:"text"`
	Running          bool   `json:"running"`
	RemainingPercent *int   `json:"remainingPercent"`
	Passed           *bool  `json:"passed"`
}

// SelfTestEntry is one record of the drive's own self-test log (at most 21).
type SelfTestEntry struct {
	Type             string `json:"type"` // short, long, conveyance, other
	TypeText         string `json:"typeText"`
	Status           string `json:"status"`
	Passed           bool   `json:"passed"`
	RemainingPercent *int   `json:"remainingPercent"`
	LifetimeHours    int64  `json:"lifetimeHours"`
}

type Detail struct {
	CheckedAt          time.Time       `json:"checkedAt"`
	Standby            bool            `json:"standby"`
	Health             string          `json:"health"` // passed, failed, unknown
	Model              *string         `json:"model"`
	Family             *string         `json:"family"`
	TemperatureCelsius *int64          `json:"temperatureCelsius"`
	PowerOnHours       *int64          `json:"powerOnHours"`
	PowerCycles        *int64          `json:"powerCycles"`
	SelfTestsSupported bool            `json:"selfTestsSupported"`
	ShortMinutes       *int            `json:"shortMinutes"`
	LongMinutes        *int            `json:"longMinutes"`
	SelfTest           *SelfTestStatus `json:"selfTest"`
	Attributes         []Attribute     `json:"attributes"`
	History            []SelfTestEntry `json:"history"`
	Error              string          `json:"error,omitempty"`
}

type detailJSON struct {
	smartctlJSON
	ModelFamily string `json:"model_family"`
	PowerCycle  *int64 `json:"power_cycle_count"`
	ATAData     *struct {
		SelfTest *struct {
			Status *struct {
				Value            int    `json:"value"`
				String           string `json:"string"`
				Passed           *bool  `json:"passed"`
				RemainingPercent *int   `json:"remaining_percent"`
			} `json:"status"`
			Polling *struct {
				Short    *int `json:"short"`
				Extended *int `json:"extended"`
			} `json:"polling_minutes"`
		} `json:"self_test"`
		Capabilities *struct {
			SelfTests bool `json:"self_tests_supported"`
		} `json:"capabilities"`
	} `json:"ata_smart_data"`
	Attrs *struct {
		Table []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Value      *int   `json:"value"`
			Worst      *int   `json:"worst"`
			Thresh     *int   `json:"thresh"`
			WhenFailed string `json:"when_failed"`
			Flags      struct {
				Prefailure bool `json:"prefailure"`
			} `json:"flags"`
			Raw struct {
				Value  int64  `json:"value"`
				String string `json:"string"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	Log *struct {
		Standard *struct {
			Table []struct {
				Type struct {
					Value  int    `json:"value"`
					String string `json:"string"`
				} `json:"type"`
				Status struct {
					String           string `json:"string"`
					Passed           bool   `json:"passed"`
					RemainingPercent *int   `json:"remaining_percent"`
				} `json:"status"`
				LifetimeHours int64 `json:"lifetime_hours"`
			} `json:"table"`
		} `json:"standard"`
	} `json:"ata_smart_self_test_log"`
}

// ParseDetail converts `smartctl -j -a` output into a Detail.
func ParseDetail(raw []byte) Detail {
	base := Parse("", raw)
	d := Detail{Standby: base.Standby, Health: base.Health, Model: base.Model, TemperatureCelsius: base.TemperatureCelsius,
		PowerOnHours: base.PowerOnHours, Attributes: []Attribute{}, History: []SelfTestEntry{}}
	var j detailJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return d
	}
	if j.ModelFamily != "" {
		d.Family = &j.ModelFamily
	}
	d.PowerCycles = j.PowerCycle
	seagate := strings.Contains(j.ModelFamily, "Seagate") || strings.HasPrefix(j.ModelName, "ST")
	if a := j.ATAData; a != nil {
		if a.Capabilities != nil {
			d.SelfTestsSupported = a.Capabilities.SelfTests
		}
		if st := a.SelfTest; st != nil {
			if st.Polling != nil {
				d.ShortMinutes, d.LongMinutes = st.Polling.Short, st.Polling.Extended
			}
			if s := st.Status; s != nil {
				d.SelfTest = &SelfTestStatus{Value: s.Value, Text: s.String, Passed: s.Passed,
					Running: s.Value>>4 == 0xF, RemainingPercent: s.RemainingPercent}
				if d.SelfTest.Running && s.RemainingPercent == nil {
					p := (s.Value & 0xF) * 10
					d.SelfTest.RemainingPercent = &p
				}
			}
		}
	}
	if j.Attrs != nil {
		for _, a := range j.Attrs.Table {
			at := Attribute{ID: a.ID, Name: a.Name, Value: a.Value, Worst: a.Worst, Thresh: a.Thresh,
				Raw: a.Raw.String, RawValue: a.Raw.Value, Prefailure: a.Flags.Prefailure,
				WhenFailed: a.WhenFailed, Key: keyAttributes[a.ID]}
			at.Count = decodeCount(a.ID, a.Raw.Value, seagate)
			at.Level = level(at)
			d.Attributes = append(d.Attributes, at)
		}
	}
	if j.Log != nil && j.Log.Standard != nil {
		for _, e := range j.Log.Standard.Table {
			d.History = append(d.History, SelfTestEntry{Type: testType(e.Type.Value), TypeText: e.Type.String,
				Status: e.Status.String, Passed: e.Status.Passed, RemainingPercent: e.Status.RemainingPercent,
				LifetimeHours: e.LifetimeHours})
		}
	}
	return d
}

// decodeCount extracts the meaningful number from a 48-bit raw value.
func decodeCount(id int, raw int64, seagate bool) int64 {
	switch {
	case id == 190 || id == 194:
		return raw & 0xFF // current temperature; upper bytes hold min/max
	case id == 9:
		return raw & 0xFFFFFFFF // some drives keep minutes in the upper bits
	case seagate && (id == 1 || id == 7 || id == 195):
		return raw >> 32 // error count; the low 32 bits count operations
	case seagate && id == 188:
		return raw&0xFFFF + raw>>16&0xFFFF + raw>>32&0xFFFF // three timeout counters
	}
	return raw
}

func level(a Attribute) string {
	if a.WhenFailed == "now" {
		return LevelCrit
	}
	if a.Prefailure && a.Value != nil && a.Thresh != nil && *a.Thresh > 0 && *a.Value <= *a.Thresh {
		return LevelCrit
	}
	switch a.ID {
	case 5, 10, 187, 197, 198:
		if a.Count > 0 {
			return LevelCrit
		}
	case 188, 199:
		if a.Count > 0 {
			return LevelWarn
		}
	case 194:
		if a.Count >= 60 {
			return LevelCrit
		}
		if a.Count >= 50 {
			return LevelWarn
		}
	case 193:
		if a.Count >= loadCycleWarn {
			return LevelWarn
		}
	}
	if a.WhenFailed == "past" {
		return LevelWarn
	}
	if a.Key {
		return LevelInfo
	}
	return LevelOK
}

// testType maps the ATA self-test number, captive variants included.
func testType(v int) string {
	switch v & 0x7F {
	case 1:
		return "short"
	case 2:
		return "long"
	case 3:
		return "conveyance"
	}
	return "other"
}

// DetailExecRunner reads everything SMART offers without spinning up a
// sleeping disk.
func DetailExecRunner(ctx context.Context, device string) ([]byte, error) {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-j", "-n", "standby", "-a", "/dev/"+device).Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, err
	}
	return out, nil
}

// DetailService serves per-disk detail with a short cache, so a polling UI
// (progress of a running test) cannot pile up smartctl processes.
type DetailService struct {
	Run   Runner
	TTL   time.Duration
	Now   func() time.Time
	mu    sync.Mutex
	cache map[string]detailEntry
}

type detailEntry struct {
	d  Detail
	at time.Time
}

func (s *DetailService) Detail(ctx context.Context, device string) Detail {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.cache[device]; ok && s.Now().Sub(e.at) < s.TTL {
		return e.d
	}
	raw, err := s.Run(ctx, device)
	var d Detail
	switch {
	case errors.Is(err, errUnavailable):
		d = Detail{Health: "unknown", Attributes: []Attribute{}, History: []SelfTestEntry{}, Error: "smartctl_unavailable"}
	case err != nil:
		d = Detail{Health: "unknown", Attributes: []Attribute{}, History: []SelfTestEntry{}, Error: "smartctl_failed"}
	default:
		d = ParseDetail(raw)
	}
	d.CheckedAt = s.Now().UTC()
	if s.cache == nil {
		s.cache = map[string]detailEntry{}
	}
	s.cache[device] = detailEntry{d, s.Now()}
	return d
}

// Invalidate drops the cached detail, e.g. right after a self-test started.
func (s *DetailService) Invalidate(device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, device)
}
