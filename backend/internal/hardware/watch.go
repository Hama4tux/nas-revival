package hardware

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Cached reads hardware.json at most once per TTL (the collector asks every
// snapshot). A broken file is logged and gives the defaults.
type Cached struct {
	Path string
	TTL  time.Duration
	Now  func() time.Time

	mu  sync.Mutex
	at  time.Time
	val Settings
	ok  bool
}

func (c *Cached) Get() Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	if c.ok && now.Sub(c.at) < c.TTL {
		return c.val
	}
	s, err := Load(c.Path)
	if err != nil {
		slog.Warn("hardware settings unreadable, using defaults", "err", err)
	}
	c.val, c.at, c.ok = s, now, true
	return s
}

// Watcher turns the temperature problems of each health evaluation into
// journal events (one when a problem appears or changes severity, one when
// it clears; never one per snapshot) and drives the red fault LED.
type Watcher struct {
	// SetFault blinks (true) or releases (false) the red system LED.
	SetFault func(ctx context.Context, on bool) error
	Settings func() Settings

	mu     sync.Mutex
	active map[string]string // problem key → severity
	fault  *bool             // last state sent; nil = unknown, send again
}

// Update takes the health status and the temperature problems (key such as
// "tempCrit:Dioda 1" → severity).
func (w *Watcher) Update(ctx context.Context, crit bool, problems map[string]string) {
	w.mu.Lock()
	if w.active == nil {
		w.active = map[string]string{}
	}
	keys := make([]string, 0, len(problems))
	for k := range problems {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if sev := problems[k]; w.active[k] != sev {
			slog.Warn("hardware threshold exceeded", "problem", k, "severity", sev)
			w.active[k] = sev
		}
	}
	for k := range w.active {
		if _, ok := problems[k]; !ok {
			slog.Info("hardware threshold back to normal", "problem", k)
			delete(w.active, k)
		}
	}
	want := crit && w.Settings != nil && w.Settings().LEDs.FaultBlink
	send := w.SetFault != nil && (w.fault == nil || *w.fault != want)
	w.mu.Unlock()
	if !send {
		return
	}
	// Outside the lock: the helper call may take a moment.
	if err := w.SetFault(ctx, want); err != nil {
		slog.Warn("fault LED not set", "on", want, "err", err)
		return
	}
	w.mu.Lock()
	w.fault = &want
	w.mu.Unlock()
}
