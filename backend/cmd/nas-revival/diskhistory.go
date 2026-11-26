package main

import (
	"context"
	"log/slog"
	"sync"

	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/storage"
)

// diskHistorySampler gives the standby tracker one round of samples (PWR-04)
// and remembers which disks were present for the health warnings.
type diskHistorySampler struct {
	storage storage.Collector
	power   *diskpower.Reader
	smart   *smart.Service
	tracker *diskpower.Tracker

	mu    sync.Mutex
	names map[string]string // disk ID → kernel name, from the last round
}

func (s *diskHistorySampler) sample(ctx context.Context) []diskpower.Sample {
	rep, err := s.storage.Report()
	if err != nil {
		slog.Warn("disk power history: storage report failed", "err", err)
		return nil
	}
	now := s.tracker.Now()
	var smartRep *smart.Report
	out := []diskpower.Sample{}
	names := map[string]string{}
	for _, d := range rep.Disks {
		if !d.SMART {
			continue // USB bridges answer CHECK POWER MODE unreliably
		}
		names[d.ID] = d.Name
		smp := diskpower.Sample{ID: d.ID, Name: d.Name, System: d.System, State: s.power.Read(ctx, d.Name).State}
		if smp.State == diskpower.StateActive && s.tracker.CountersDue(d.ID, now) {
			if smartRep == nil {
				r := s.smart.Report(ctx)
				smartRep = &r
			}
			for _, sd := range smartRep.Disks {
				if sd.Name == d.Name && !sd.Standby {
					smp.LoadCycles, smp.StartStop = sd.LoadCycleCount, sd.StartStopCount
				}
			}
		}
		out = append(out, smp)
	}
	s.mu.Lock()
	s.names = names
	s.mu.Unlock()
	return out
}

func (s *diskHistorySampler) warnings() []diskpower.Warning {
	s.mu.Lock()
	names := s.names
	s.mu.Unlock()
	if names == nil {
		return nil
	}
	return s.tracker.Warnings(names)
}
