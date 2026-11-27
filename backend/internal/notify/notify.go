// Package notify is the in-UI notification centre (NOTIF-01, ADR 0012). It
// turns the health problems of each overview snapshot into lasting entries:
// one when a problem appears, resolved when it clears, reopened (not
// duplicated) when the same problem comes back within the dedup window.
// Read state is kept per user. The list survives restarts in a JSON file.
package notify

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
)

const (
	// Dedup: the same problem coming back within this time reopens its entry.
	Dedup = time.Hour
	// Keep resolved entries this long, and never more than MaxEntries in all.
	Retention  = 30 * 24 * time.Hour
	MaxEntries = 200
)

// Problem is one current health problem (system.Problem without the import).
type Problem struct {
	Severity string
	Code     string
	Subject  string
}

func (p Problem) key() string { return p.Code + "\x00" + p.Subject }

type Entry struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // warn | crit
	Code     string `json:"code"`
	Subject  string `json:"subject,omitempty"`
	// FirstAt is when the problem first appeared, UpdatedAt when it last
	// appeared again or changed severity (the list is sorted by it).
	FirstAt    time.Time  `json:"firstAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	// Count is how many times the problem appeared within this entry.
	Count  int      `json:"count"`
	ReadBy []string `json:"readBy,omitempty"`
}

func (e *Entry) key() string { return e.Code + "\x00" + e.Subject }

// View is an entry as one user sees it.
type View struct {
	ID         string     `json:"id"`
	Severity   string     `json:"severity"`
	Code       string     `json:"code"`
	Subject    string     `json:"subject,omitempty"`
	FirstAt    time.Time  `json:"firstAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	Count      int        `json:"count"`
	Read       bool       `json:"read"`
}

type List struct {
	Items  []View `json:"items"`
	Unread int    `json:"unread"`
	Active int    `json:"active"`
}

type file struct {
	Version int      `json:"version"`
	Entries []*Entry `json:"entries"`
}

// Center holds the entries. Path empty keeps them in memory only (tests).
type Center struct {
	Path string
	Now  func() time.Time

	mu      sync.Mutex
	entries []*Entry
}

// Open loads the saved entries; a missing file is an empty centre and a
// broken one is logged and started over (notifications are not worth
// refusing to start for).
func Open(path string, now func() time.Time) *Center {
	c := &Center{Path: path, Now: now}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c
	}
	var f file
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		slog.Warn("notifications unreadable, starting empty", "err", err)
		return c
	}
	for _, e := range f.Entries {
		if e != nil && e.ID != "" && e.Code != "" {
			c.entries = append(c.entries, e)
		}
	}
	return c
}

// Update compares the current problems with the open entries. hold reports
// open entries whose data is unknown right now (disk in standby); they are
// left open instead of being resolved.
func (c *Center) Update(problems []Problem, hold func(code, subject string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now().UTC()
	changed := false
	open := map[string]*Entry{}
	for _, e := range c.entries {
		if e.ResolvedAt == nil {
			open[e.key()] = e
		}
	}
	seen := map[string]bool{}
	for _, p := range problems {
		k := p.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		if e := open[k]; e != nil {
			if e.Severity != p.Severity {
				e.Severity, e.UpdatedAt, e.ReadBy = p.Severity, now, nil
				changed = true
			}
			continue
		}
		if e := c.recent(k, now); e != nil {
			// Same event within the dedup window: one entry. Read state stays
			// unless it got worse.
			if e.Severity != p.Severity {
				e.ReadBy = nil
			}
			e.Severity, e.UpdatedAt, e.ResolvedAt = p.Severity, now, nil
			e.Count++
		} else {
			c.entries = append(c.entries, &Entry{ID: newID(), Severity: p.Severity, Code: p.Code, Subject: p.Subject,
				FirstAt: now, UpdatedAt: now, Count: 1})
			slog.Info("notification", "code", p.Code, "subject", p.Subject, "severity", p.Severity)
		}
		changed = true
	}
	for k, e := range open {
		if seen[k] || (hold != nil && hold(e.Code, e.Subject)) {
			continue
		}
		t := now
		e.ResolvedAt = &t
		changed = true
	}
	if c.prune(now) {
		changed = true
	}
	if changed {
		c.save()
	}
}

// recent is the newest entry for key resolved within the dedup window.
func (c *Center) recent(k string, now time.Time) *Entry {
	var best *Entry
	for _, e := range c.entries {
		if e.ResolvedAt != nil && e.key() == k && now.Sub(*e.ResolvedAt) <= Dedup &&
			(best == nil || e.ResolvedAt.After(*best.ResolvedAt)) {
			best = e
		}
	}
	return best
}

// prune drops resolved entries past retention, then the oldest resolved
// ones over MaxEntries. Open entries are never dropped.
func (c *Center) prune(now time.Time) bool {
	n := len(c.entries)
	c.entries = slices.DeleteFunc(c.entries, func(e *Entry) bool {
		return e.ResolvedAt != nil && now.Sub(*e.ResolvedAt) > Retention
	})
	if len(c.entries) > MaxEntries {
		sort.SliceStable(c.entries, func(i, j int) bool { return c.entries[i].UpdatedAt.Before(c.entries[j].UpdatedAt) })
		over := len(c.entries) - MaxEntries
		c.entries = slices.DeleteFunc(c.entries, func(e *Entry) bool {
			if over > 0 && e.ResolvedAt != nil {
				over--
				return true
			}
			return false
		})
	}
	return len(c.entries) != n
}

// List returns the entries for user, newest change first.
func (c *Center) List(user string) List {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := List{Items: make([]View, 0, len(c.entries))}
	for _, e := range c.entries {
		read := slices.Contains(e.ReadBy, user)
		l.Items = append(l.Items, View{ID: e.ID, Severity: e.Severity, Code: e.Code, Subject: e.Subject,
			FirstAt: e.FirstAt, UpdatedAt: e.UpdatedAt, ResolvedAt: e.ResolvedAt, Count: e.Count, Read: read})
		if !read {
			l.Unread++
		}
		if e.ResolvedAt == nil {
			l.Active++
		}
	}
	sort.SliceStable(l.Items, func(i, j int) bool { return l.Items[i].UpdatedAt.After(l.Items[j].UpdatedAt) })
	return l
}

// MarkRead marks the given entries (all when ids is nil) read for user.
// Unknown ids are ignored: the entry may have been pruned meanwhile.
func (c *Center) MarkRead(user string, ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := false
	for _, e := range c.entries {
		if (ids == nil || slices.Contains(ids, e.ID)) && !slices.Contains(e.ReadBy, user) {
			e.ReadBy = append(e.ReadBy, user)
			changed = true
		}
	}
	if changed {
		c.save()
	}
}

func (c *Center) save() {
	if c.Path == "" {
		return
	}
	data, err := json.Marshal(file{Version: 1, Entries: c.entries})
	if err == nil {
		err = atomicfile.Write(c.Path, data, 0o600)
	}
	if err != nil {
		slog.Error("notifications not saved", "err", err)
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on Linux; keep IDs unique regardless.
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
