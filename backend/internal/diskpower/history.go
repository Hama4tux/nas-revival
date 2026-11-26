package diskpower

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
)

// Standby history and wear counters (PWR-04). The web process asks every
// internal disk with hdparm -C (never wakes it) every few minutes, records
// each change of the power state and, while the disk is awake, the SMART
// counters 4 (Start_Stop_Count) and 193 (Load_Cycle_Count) once an hour.
//
// The file lives on the system disk, so it is written only while that disk is
// awake: writing it right after the disk went to sleep would wake it again.
// Changes recorded during standby are kept in RAM until the next wake.

// Wear limits per day. A drive rated for 300 000 load cycles reaches them in
// eight years at 100 a day; 25 spin-ups a day keep a 50 000 start/stop rating
// for five years. More points at aggressive head parking or a disk that is
// woken again shortly after each spin-down.
const (
	LoadCycleWarnPerDay = 100
	StartStopWarnPerDay = 25
)

// Warning codes (shown as health problems).
const (
	WarnLoadCycles = "loadCycles"
	WarnStartStop  = "startStop"
)

const (
	historyKeep     = 30 * 24 * time.Hour
	historyMaxEvent = 3000
	counterEvery    = time.Hour
	// rateMinSpan is the shortest counter span a daily rate is computed from.
	rateMinSpan = 20 * time.Hour
	viewWindow  = 7 * 24 * time.Hour
)

// Event is a change of the power state seen by the sampler.
type Event struct {
	T     int64  `json:"t"` // Unix ms
	State string `json:"state"`
}

// Counter is one reading of the SMART wear counters (nil when absent).
type Counter struct {
	T          int64  `json:"t"`
	LoadCycles *int64 `json:"loadCycles"`
	StartStop  *int64 `json:"startStop"`
}

type diskHistory struct {
	Events   []Event   `json:"events"`
	Counters []Counter `json:"counters"`
}

type historyFile struct {
	Version int                     `json:"version"`
	Disks   map[string]*diskHistory `json:"disks"`
}

// Sample is one round for one disk, given by the caller.
type Sample struct {
	ID         string
	Name       string
	System     bool
	State      string
	LoadCycles *int64
	StartStop  *int64
}

// Warning is a wear counter growing faster than its limit.
type Warning struct {
	DiskID string
	Name   string
	Code   string
}

// Growth is the daily rate of the wear counters over about the last day.
type Growth struct {
	LoadCyclesPerDay *float64 `json:"loadCyclesPerDay"`
	StartStopPerDay  *float64 `json:"startStopPerDay"`
	SpanHours        float64  `json:"spanHours"` // 0 until 20 h of counters exist
	LoadCycleLimit   int      `json:"loadCycleLimit"`
	StartStopLimit   int      `json:"startStopLimit"`
	LoadCyclesWarn   bool     `json:"loadCyclesWarn"`
	StartStopWarn    bool     `json:"startStopWarn"`
	LatestLoadCycles *int64   `json:"latestLoadCycles"`
	LatestStartStop  *int64   `json:"latestStartStop"`
	LatestCounterAt  *int64   `json:"latestCounterAt"`
}

// HistoryView is what GET /storage/disks/{id}/power/history returns.
type HistoryView struct {
	// Events of the last 7 days, oldest first; the first may be older and
	// gives the state at the start of the window.
	Events   []Event   `json:"events"`
	Counters []Counter `json:"counters"`
	// Wakes counts standby → active changes the sampler saw; a disk woken and
	// put to sleep again between two samples is only in the SMART counters.
	Wakes24h int `json:"wakes24h"`
	Wakes7d  int `json:"wakes7d"`
	// StandbyPercent24h is the share of the tracked part of the last 24 h
	// spent in standby; nil until something was tracked.
	StandbyPercent24h *float64 `json:"standbyPercent24h"`
	TrackedSince      *int64   `json:"trackedSince"`
	SampleSeconds     int      `json:"sampleSeconds"`
	Growth            Growth   `json:"growth"`
}

// Tracker samples, keeps and stores the history of all internal disks.
type Tracker struct {
	Path     string
	Interval time.Duration
	Now      func() time.Time
	Sample   func(ctx context.Context) []Sample

	mu     sync.Mutex
	data   historyFile
	loaded bool
	dirty  bool
}

func NewTracker(path string, sample func(ctx context.Context) []Sample) *Tracker {
	return &Tracker{Path: path, Interval: 5 * time.Minute, Now: time.Now, Sample: sample}
}

func (t *Tracker) load() {
	if t.loaded {
		return
	}
	t.loaded = true
	t.data = historyFile{Version: 1, Disks: map[string]*diskHistory{}}
	raw, err := os.ReadFile(t.Path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var f historyFile
	if err == nil {
		err = json.Unmarshal(raw, &f)
	}
	if err != nil {
		slog.Warn("disk power history unreadable, starting empty", "err", err)
		return
	}
	if f.Disks != nil {
		t.data.Disks = f.Disks
	}
	for _, d := range t.data.Disks {
		if d == nil {
			continue
		}
		if d.Events == nil {
			d.Events = []Event{}
		}
		if d.Counters == nil {
			d.Counters = []Counter{}
		}
	}
}

// Run samples until ctx ends and then saves what is pending.
func (t *Tracker) Run(ctx context.Context) {
	t.round(ctx)
	tick := time.NewTicker(t.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.save()
			return
		case <-tick.C:
			t.round(ctx)
		}
	}
}

func (t *Tracker) round(ctx context.Context) {
	samples := t.Sample(ctx)
	if t.Record(t.Now(), samples) {
		t.save()
	}
}

// Record folds one round into the history. It reports whether the file should
// be written now: something changed and the system disk is awake.
func (t *Tracker) Record(at time.Time, samples []Sample) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	ms := at.UnixMilli()
	awake := true
	for _, s := range samples {
		if s.System && isAsleep(s.State) {
			awake = false
		}
		if s.ID == "" {
			continue
		}
		d := t.data.Disks[s.ID]
		if d == nil {
			d = &diskHistory{Events: []Event{}, Counters: []Counter{}}
			t.data.Disks[s.ID] = d
		}
		if s.State != StateUnknown && (len(d.Events) == 0 || d.Events[len(d.Events)-1].State != s.State) {
			d.Events = append(d.Events, Event{T: ms, State: s.State})
			t.dirty = true
		}
		if s.LoadCycles != nil || s.StartStop != nil {
			n := len(d.Counters)
			if n == 0 || ms-d.Counters[n-1].T >= counterEvery.Milliseconds() {
				d.Counters = append(d.Counters, Counter{T: ms, LoadCycles: s.LoadCycles, StartStop: s.StartStop})
				t.dirty = true
			}
		}
		t.prune(d, ms)
	}
	return t.dirty && awake
}

func (t *Tracker) prune(d *diskHistory, now int64) {
	cut := now - historyKeep.Milliseconds()
	i := 0
	// Keep the last event before the cut: it gives the state at the cut.
	for i+1 < len(d.Events) && d.Events[i+1].T <= cut {
		i++
	}
	if len(d.Events)-i > historyMaxEvent {
		i = len(d.Events) - historyMaxEvent
	}
	if i > 0 {
		d.Events = append([]Event{}, d.Events[i:]...)
	}
	j := 0
	for j < len(d.Counters) && d.Counters[j].T < cut {
		j++
	}
	if j > 0 {
		d.Counters = append([]Counter{}, d.Counters[j:]...)
	}
}

func (t *Tracker) save() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.dirty {
		return
	}
	raw, err := json.Marshal(t.data)
	if err == nil {
		err = atomicfile.Write(t.Path, raw, 0o600)
	}
	if err != nil {
		slog.Warn("disk power history not saved", "err", err)
		return
	}
	t.dirty = false
}

// CountersDue reports whether the SMART counters of the disk should be read
// in this round: SMART commands may restart the drive's idle timer, so they
// are asked at most once an hour.
func (t *Tracker) CountersDue(diskID string, at time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	d := t.data.Disks[diskID]
	if d == nil || len(d.Counters) == 0 {
		return true
	}
	return at.UnixMilli()-d.Counters[len(d.Counters)-1].T >= counterEvery.Milliseconds()
}

// View returns the history of one disk at time at.
func (t *Tracker) View(diskID string, at time.Time) HistoryView {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	v := HistoryView{Events: []Event{}, Counters: []Counter{}, SampleSeconds: int(t.Interval / time.Second)}
	d := t.data.Disks[diskID]
	if d == nil {
		v.Growth = growth(nil)
		return v
	}
	now := at.UnixMilli()
	start := now - viewWindow.Milliseconds()
	for i, e := range d.Events {
		if e.T >= start || (i+1 < len(d.Events) && d.Events[i+1].T > start) || i == len(d.Events)-1 {
			v.Events = append(v.Events, e)
		}
	}
	for _, c := range d.Counters {
		if c.T >= start {
			v.Counters = append(v.Counters, c)
		}
	}
	if len(d.Events) > 0 {
		first := d.Events[0].T
		v.TrackedSince = &first
	}
	v.Wakes24h = wakes(d.Events, now-24*time.Hour.Milliseconds())
	v.Wakes7d = wakes(d.Events, start)
	v.StandbyPercent24h = standbyShare(d.Events, now-24*time.Hour.Milliseconds(), now)
	v.Growth = growth(d.Counters)
	return v
}

// Warnings lists the disks whose counters grow faster than the limits.
func (t *Tracker) Warnings(names map[string]string) []Warning {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	var out []Warning
	for id, d := range t.data.Disks {
		if d == nil {
			continue
		}
		name, ok := names[id]
		if !ok {
			continue // disk no longer present
		}
		g := growth(d.Counters)
		if g.LoadCyclesWarn {
			out = append(out, Warning{DiskID: id, Name: name, Code: WarnLoadCycles})
		}
		if g.StartStopWarn {
			out = append(out, Warning{DiskID: id, Name: name, Code: WarnStartStop})
		}
	}
	return out
}

func isAsleep(state string) bool { return state == StateStandby || state == StateSleep }

func wakes(events []Event, since int64) int {
	n := 0
	for i := 1; i < len(events); i++ {
		if events[i].T >= since && events[i].State == StateActive && isAsleep(events[i-1].State) {
			n++
		}
	}
	return n
}

// standbyShare counts each state until the next change (or now); the time
// before the first event is not tracked.
func standbyShare(events []Event, from, to int64) *float64 {
	var tracked, asleep int64
	for i, e := range events {
		end := to
		if i+1 < len(events) {
			end = events[i+1].T
		}
		s := max(e.T, from)
		if end <= s {
			continue
		}
		tracked += end - s
		if isAsleep(e.State) {
			asleep += end - s
		}
	}
	if tracked == 0 {
		return nil
	}
	p := float64(asleep) / float64(tracked) * 100
	return &p
}

// growth compares the newest counters with the latest reading at least 20 h
// older, so the rate describes about the last day.
func growth(cs []Counter) Growth {
	g := Growth{LoadCycleLimit: LoadCycleWarnPerDay, StartStopLimit: StartStopWarnPerDay}
	if len(cs) == 0 {
		return g
	}
	last := cs[len(cs)-1]
	g.LatestLoadCycles, g.LatestStartStop = last.LoadCycles, last.StartStop
	lt := last.T
	g.LatestCounterAt = &lt
	base := -1
	for i := len(cs) - 2; i >= 0; i-- {
		if last.T-cs[i].T >= rateMinSpan.Milliseconds() {
			base = i
			break
		}
	}
	if base < 0 {
		return g
	}
	b := cs[base]
	days := float64(last.T-b.T) / float64(24*time.Hour.Milliseconds())
	g.SpanHours = days * 24
	rate := func(newV, oldV *int64) *float64 {
		if newV == nil || oldV == nil || *newV < *oldV {
			return nil // missing or reset (e.g. a firmware quirk)
		}
		r := float64(*newV-*oldV) / days
		return &r
	}
	g.LoadCyclesPerDay = rate(last.LoadCycles, b.LoadCycles)
	g.StartStopPerDay = rate(last.StartStop, b.StartStop)
	g.LoadCyclesWarn = g.LoadCyclesPerDay != nil && *g.LoadCyclesPerDay > LoadCycleWarnPerDay
	g.StartStopWarn = g.StartStopPerDay != nil && *g.StartStopPerDay > StartStopWarnPerDay
	return g
}
