package tasks

import (
	"log/slog"
	"sync"
	"time"

	"nas-revival/backend/internal/system"
)

// Backups are considered overdue after two missed daily or one missed weekly
// run (plus a day of slack); monthly ones are not watched.
const (
	staleDaily  = 48 * time.Hour
	staleWeekly = 8 * 24 * time.Hour
)

// BackupStatusSource reads the backup-pull tasks and their history, at most
// once per TTL, because the overview is collected every few seconds.
type BackupStatusSource struct {
	ConfigPath  string
	HistoryPath string
	TTL         time.Duration
	Now         func() time.Time

	mu     sync.Mutex
	at     time.Time
	cached []system.BackupStatus
	warned bool
}

// Statuses is system.Collector.Backups.
func (s *BackupStatusSource) Statuses() []system.BackupStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if !s.at.IsZero() && now.Sub(s.at) < s.TTL {
		return s.cached
	}
	s.at = now
	s.cached = s.read()
	return s.cached
}

func (s *BackupStatusSource) read() []system.BackupStatus {
	out := []system.BackupStatus{}
	list, err := Load(s.ConfigPath)
	if err == nil {
		var h History
		if h, err = LoadHistory(s.HistoryPath); err == nil {
			s.warned = false
			for _, t := range list {
				if t.Type != TypeBackup {
					continue
				}
				out = append(out, backupStatus(t, h.Runs[t.ID]))
			}
			return out
		}
	}
	if !s.warned {
		slog.Warn("backup status unavailable", "err", err)
		s.warned = true
	}
	return out
}

func backupStatus(t Task, runs []Run) system.BackupStatus {
	b := system.BackupStatus{ID: t.ID, Name: t.Name, Enabled: t.Enabled}
	switch t.Schedule.Kind {
	case KindDaily:
		b.StaleAfter = staleDaily
	case KindWeekly:
		b.StaleAfter = staleWeekly
	}
	if len(runs) > 0 {
		end := runs[0].End
		b.LastResult, b.LastEnd, b.Message, b.UsedBytes = runs[0].Result, &end, runs[0].Message, runs[0].UsedBytes
		// Staleness counts from the last successful run, so a row of skipped
		// or stopped runs does not hide an overdue backup. Without any ok run
		// in the history, it counts from the oldest recorded one.
		since := runs[len(runs)-1].End
		for _, r := range runs {
			if r.Result == ResultOK {
				since = r.End
				break
			}
		}
		b.LastOKEnd = &since
	}
	return b
}
