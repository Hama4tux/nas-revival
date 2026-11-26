package diskpower

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	return NewTracker(filepath.Join(t.TempDir(), "disk-power-history.json"), nil)
}

var t0 = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

func TestRecordEventsOnChangeOnly(t *testing.T) {
	tr := newTestTracker(t)
	states := []string{StateActive, StateActive, StateStandby, StateUnknown, StateStandby, StateActive}
	for i, st := range states {
		tr.Record(t0.Add(time.Duration(i)*5*time.Minute), []Sample{{ID: "d1", Name: "sda", State: st}})
	}
	v := tr.View("d1", t0.Add(30*time.Minute))
	if len(v.Events) != 3 || v.Events[1].State != StateStandby || v.Events[2].State != StateActive {
		t.Fatalf("events %+v", v.Events)
	}
	if v.Wakes24h != 1 || v.Wakes7d != 1 {
		t.Fatalf("wakes %d %d", v.Wakes24h, v.Wakes7d)
	}
	// active 10 min, standby 15 min, active 5 min (until now)
	if v.StandbyPercent24h == nil || *v.StandbyPercent24h != 50 {
		t.Fatalf("standby share %v", v.StandbyPercent24h)
	}
}

func TestSaveOnlyWhileSystemDiskAwake(t *testing.T) {
	tr := newTestTracker(t)
	if !tr.Record(t0, []Sample{{ID: "d1", System: true, State: StateActive}}) {
		t.Fatal("change on an awake system disk must be saved")
	}
	tr.save()
	if tr.Record(t0.Add(5*time.Minute), []Sample{{ID: "d1", System: true, State: StateStandby}}) {
		t.Fatal("saving now would wake the sleeping system disk")
	}
	if _, err := os.Stat(tr.Path); err != nil {
		t.Fatal(err)
	}
	if !tr.Record(t0.Add(10*time.Minute), []Sample{{ID: "d1", System: true, State: StateActive}}) {
		t.Fatal("pending change must be saved after the wake")
	}
	tr.save()
	again := NewTracker(tr.Path, nil)
	if v := again.View("d1", t0.Add(15*time.Minute)); len(v.Events) != 3 {
		t.Fatalf("reloaded events %+v", v.Events)
	}
}

func TestCountersHourlyAndGrowthWarning(t *testing.T) {
	tr := newTestTracker(t)
	lc, ss := int64(1000), int64(50)
	for m := 0; m <= 24*60; m += 5 {
		at := t0.Add(time.Duration(m) * time.Minute)
		s := Sample{ID: "d1", Name: "sda", State: StateActive}
		if tr.CountersDue("d1", at) {
			s.LoadCycles, s.StartStop = i64(lc), i64(ss)
		}
		tr.Record(at, []Sample{s})
		lc += 1 // 288 a day
	}
	v := tr.View("d1", t0.Add(24*time.Hour))
	if len(v.Counters) != 25 {
		t.Fatalf("want hourly counters, got %d", len(v.Counters))
	}
	g := v.Growth
	if g.SpanHours != 20 || g.LoadCyclesPerDay == nil || *g.LoadCyclesPerDay != 288 || !g.LoadCyclesWarn {
		t.Fatalf("growth %+v", g)
	}
	if g.StartStopPerDay == nil || *g.StartStopPerDay != 0 || g.StartStopWarn {
		t.Fatalf("start/stop %+v", g)
	}
	w := tr.Warnings(map[string]string{"d1": "sda"})
	if len(w) != 1 || w[0].Code != WarnLoadCycles || w[0].Name != "sda" {
		t.Fatalf("warnings %+v", w)
	}
	if w := tr.Warnings(map[string]string{}); len(w) != 0 {
		t.Fatalf("removed disk still warned: %+v", w)
	}
}

func TestGrowthNeedsSpanAndIgnoresReset(t *testing.T) {
	g := growth([]Counter{{T: 0, LoadCycles: i64(10)}, {T: time.Hour.Milliseconds(), LoadCycles: i64(500)}})
	if g.LoadCyclesPerDay != nil || g.LoadCyclesWarn || g.SpanHours != 0 {
		t.Fatalf("rate from 1 h: %+v", g)
	}
	g = growth([]Counter{{T: 0, LoadCycles: i64(900)}, {T: 30 * time.Hour.Milliseconds(), LoadCycles: i64(10)}})
	if g.LoadCyclesPerDay != nil || g.LoadCyclesWarn {
		t.Fatalf("reset counted: %+v", g)
	}
}

func TestPruneKeepsStateAtCut(t *testing.T) {
	tr := newTestTracker(t)
	tr.Record(t0, []Sample{{ID: "d1", State: StateStandby}})
	tr.Record(t0.Add(time.Hour), []Sample{{ID: "d1", State: StateActive}})
	later := t0.Add(40 * 24 * time.Hour)
	tr.Record(later, []Sample{{ID: "d1", State: StateActive, LoadCycles: i64(1)}})
	v := tr.View("d1", later)
	if len(v.Events) != 1 || v.Events[0].State != StateActive {
		t.Fatalf("events after prune %+v", v.Events)
	}
	if v.StandbyPercent24h == nil || *v.StandbyPercent24h != 0 {
		t.Fatalf("share %v", v.StandbyPercent24h)
	}
}
