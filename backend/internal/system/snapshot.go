package system

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Snapshots is the shared collector (MON-01): one goroutine collects the
// overview and every client gets the latest snapshot, so three open browsers
// cost about as much as one. It collects every Active while somebody asked
// within Watch, otherwise every Idle.
type Snapshots struct {
	Collect func(ctx context.Context) (Overview, error)
	Active  time.Duration
	Idle    time.Duration
	Watch   time.Duration
	// Wait bounds how long a request waits for a refresh after an idle period.
	Wait time.Duration
	Now  func() time.Time

	mu      sync.Mutex
	cur     *Overview
	err     error
	tried   time.Time // last collection attempt
	lastReq time.Time
	updated chan struct{} // closed and replaced after every refresh
	wake    chan struct{}
}

func NewSnapshots(collect func(ctx context.Context) (Overview, error)) *Snapshots {
	return &Snapshots{
		Collect: collect, Active: 5 * time.Second, Idle: 60 * time.Second,
		Watch: 30 * time.Second, Wait: 3 * time.Second, Now: time.Now,
	}
}

var errNoSnapshot = errors.New("no overview snapshot yet")

func (s *Snapshots) init() {
	if s.updated == nil {
		s.updated = make(chan struct{})
		s.wake = make(chan struct{}, 1)
	}
}

// Run collects until ctx ends.
func (s *Snapshots) Run(ctx context.Context) {
	s.mu.Lock()
	s.init()
	wake := s.wake
	s.mu.Unlock()
	for {
		s.Refresh(ctx)
		t := time.NewTimer(s.interval())
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-wake:
			t.Stop()
		}
	}
}

func (s *Snapshots) interval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastReq.IsZero() && s.Now().Sub(s.lastReq) < s.Watch {
		return s.Active
	}
	return s.Idle
}

// Refresh collects once. A failed collection keeps the last good snapshot but
// marks its health unknown, so the UI does not show stale "all fine".
func (s *Snapshots) Refresh(ctx context.Context) {
	o, err := s.Collect(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.tried = s.Now()
	if err != nil {
		slog.Error("overview collection failed", "err", err)
		s.err = err
		if s.cur != nil {
			stale := *s.cur
			stale.Health = markUnknown(stale.Health, s.Now().UTC())
			s.cur = &stale
		}
	} else {
		s.cur, s.err = &o, nil
	}
	close(s.updated)
	s.updated = make(chan struct{})
}

func markUnknown(h Health, at time.Time) Health {
	if h.Status == HealthUnknown {
		h.CheckedAt = at
		return h
	}
	probs := append([]Problem{{Severity: HealthWarn, Code: ProblemCollectFailed}}, h.Problems...)
	return Health{Status: HealthUnknown, CheckedAt: at, SmartCheckedAt: h.SmartCheckedAt, Problems: probs}
}

// Snapshot returns the latest snapshot. After an idle period (last attempt
// older than two active intervals) it wakes the collector and waits up to
// Wait for a fresh one, then falls back to what it has.
func (s *Snapshots) Snapshot(ctx context.Context) (Overview, error) {
	s.mu.Lock()
	s.init()
	now := s.Now()
	s.lastReq = now
	if !s.tried.IsZero() && now.Sub(s.tried) <= 2*s.Active {
		defer s.mu.Unlock()
		return s.latest()
	}
	updated := s.updated
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.mu.Unlock()

	t := time.NewTimer(s.Wait)
	defer t.Stop()
	select {
	case <-updated:
	case <-t.C:
	case <-ctx.Done():
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest()
}

func (s *Snapshots) latest() (Overview, error) {
	if s.cur != nil {
		return *s.cur, nil
	}
	if s.err != nil {
		return Overview{}, s.err
	}
	return Overview{}, errNoSnapshot
}
