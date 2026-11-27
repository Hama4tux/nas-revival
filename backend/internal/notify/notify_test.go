package notify

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newCenter(t *testing.T) (*Center, *clock) {
	t.Helper()
	ck := &clock{t: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)}
	return Open(filepath.Join(t.TempDir(), "notifications.json"), ck.now), ck
}

var backupFailed = Problem{Severity: "crit", Code: "backupFailed", Subject: "web"}

func TestAppearResolveAndDedup(t *testing.T) {
	c, ck := newCenter(t)
	c.Update([]Problem{backupFailed, backupFailed}, nil)
	c.Update([]Problem{backupFailed}, nil) // still there: no new entry
	l := c.List("jana")
	if len(l.Items) != 1 || l.Unread != 1 || l.Active != 1 || l.Items[0].Count != 1 || l.Items[0].ResolvedAt != nil {
		t.Fatalf("after appear: %+v", l)
	}
	c.MarkRead("jana", nil)
	ck.add(10 * time.Minute)
	c.Update(nil, nil)
	if l := c.List("jana"); l.Active != 0 || l.Items[0].ResolvedAt == nil || !l.Items[0].ResolvedAt.Equal(ck.t) {
		t.Fatalf("after resolve: %+v", l)
	}
	// Back within an hour: the same entry, still read.
	ck.add(50 * time.Minute)
	c.Update([]Problem{backupFailed}, nil)
	l = c.List("jana")
	if len(l.Items) != 1 || l.Items[0].Count != 2 || l.Items[0].ResolvedAt != nil || l.Unread != 0 {
		t.Fatalf("reopen within dedup: %+v", l)
	}
	// Resolved for more than an hour: a new, unread entry.
	c.Update(nil, nil)
	ck.add(61 * time.Minute)
	c.Update([]Problem{backupFailed}, nil)
	l = c.List("jana")
	if len(l.Items) != 2 || l.Unread != 1 || l.Items[0].Read || !l.Items[1].Read || l.Items[0].Count != 1 {
		t.Fatalf("after dedup window: %+v", l)
	}
}

func TestReadStateIsPerUserAndWorseSeverityIsUnread(t *testing.T) {
	c, _ := newCenter(t)
	c.Update([]Problem{{Severity: "warn", Code: "smartSectors", Subject: "sda"}, {Severity: "warn", Code: "fsWarn", Subject: "/"}}, nil)
	l := c.List("jana")
	c.MarkRead("jana", []string{l.Items[0].ID, "unknown"})
	if got := c.List("jana").Unread; got != 1 {
		t.Fatalf("jana unread = %d", got)
	}
	if got := c.List("petr").Unread; got != 2 {
		t.Fatalf("petr unread = %d", got)
	}
	c.MarkRead("jana", nil)
	c.Update([]Problem{{Severity: "crit", Code: "smartSectors", Subject: "sda"}, {Severity: "warn", Code: "fsWarn", Subject: "/"}}, nil)
	if l := c.List("jana"); l.Unread != 1 || l.Items[0].Code != "smartSectors" || l.Items[0].Severity != "crit" {
		t.Fatalf("after escalation: %+v", l)
	}
}

func TestHoldKeepsEntryOpen(t *testing.T) {
	c, _ := newCenter(t)
	p := Problem{Severity: "warn", Code: "smartSectors", Subject: "sda"}
	c.Update([]Problem{p}, nil)
	c.Update(nil, func(code, subject string) bool { return subject == "sda" })
	if l := c.List("x"); l.Active != 1 {
		t.Fatalf("held entry resolved: %+v", l)
	}
	c.Update([]Problem{p}, nil)
	if l := c.List("x"); len(l.Items) != 1 || l.Items[0].Count != 1 {
		t.Fatalf("held entry duplicated: %+v", l)
	}
}

func TestSurvivesRestart(t *testing.T) {
	c, ck := newCenter(t)
	c.Update([]Problem{backupFailed}, nil)
	c.MarkRead("jana", nil)
	if fi, err := os.Stat(c.Path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	r := Open(c.Path, ck.now)
	r.Update([]Problem{backupFailed}, nil)
	if l := r.List("jana"); len(l.Items) != 1 || l.Unread != 0 || l.Active != 1 {
		t.Fatalf("after restart: %+v", l)
	}
}

func TestBrokenFileStartsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.json")
	os.WriteFile(p, []byte("{"), 0o600)
	c := Open(p, time.Now)
	if l := c.List("x"); len(l.Items) != 0 {
		t.Fatalf("%+v", l)
	}
}

func TestPrune(t *testing.T) {
	c, ck := newCenter(t)
	old := Problem{Severity: "warn", Code: "fsWarn", Subject: "/old"}
	c.Update([]Problem{old}, nil)
	c.Update(nil, nil)
	ck.add(Retention + time.Hour)
	c.Update([]Problem{backupFailed}, nil)
	if l := c.List("x"); len(l.Items) != 1 || l.Items[0].Code != "backupFailed" {
		t.Fatalf("retention: %+v", l)
	}
	// Over the cap the oldest resolved go first; the open one stays.
	for i := 0; i < MaxEntries+5; i++ {
		ck.add(2 * Dedup)
		c.Update([]Problem{backupFailed, {Severity: "warn", Code: "fsWarn", Subject: "/" + string(rune('a'+i%26)) + string(rune('a'+i/26))}}, nil)
	}
	l := c.List("x")
	if len(l.Items) != MaxEntries || l.Active != 2 {
		t.Fatalf("cap: %d items, active %d", len(l.Items), l.Active)
	}
	found := false
	for _, it := range l.Items {
		if it.Code == "backupFailed" && it.ResolvedAt == nil {
			found = true
		}
	}
	if !found {
		t.Fatal("open entry pruned")
	}
}
