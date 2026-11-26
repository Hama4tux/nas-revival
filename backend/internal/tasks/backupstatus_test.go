package tasks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupStatusSource(t *testing.T) {
	dir := t.TempDir()
	cfg, hist := filepath.Join(dir, "tasks.json"), filepath.Join(dir, "h.json")
	os.WriteFile(cfg, []byte(`{"version":1,"tasks":[
 {"id":"a","name":"Server A","type":"backup-pull","enabled":true,"schedule":{"kind":"weekly","time":"02:00","weekdays":["sun"]},"backup":{"host":"srv.example","user":"bkp","paths":["data"],"keepDays":7}},
 {"id":"b","name":"Server B","type":"backup-pull","enabled":false,"schedule":{"kind":"daily","time":"02:00"},"backup":{"host":"srv.example","user":"bkp","paths":["data"],"keepDays":7}},
 {"id":"c","name":"Koš","type":"recycle-cleanup","enabled":true,"schedule":{"kind":"daily","time":"03:30"}}]}`), 0o600)
	os.WriteFile(hist, []byte(`{"version":1,"runs":{"a":[
 {"start":"2026-11-25T02:00:00Z","end":"2026-11-25T02:10:00Z","result":"failed","message":"boom","usedBytes":5},
 {"start":"2026-11-18T02:00:00Z","end":"2026-11-18T02:10:00Z","result":"ok","message":""}]}}`), 0o600)
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	s := &BackupStatusSource{ConfigPath: cfg, HistoryPath: hist, TTL: time.Minute, Now: func() time.Time { return now }}

	got := s.Statuses()
	if len(got) != 2 {
		t.Fatalf("want 2 backup tasks, got %+v", got)
	}
	a, b := got[0], got[1]
	if a.ID != "a" || a.LastResult != "failed" || a.Message != "boom" || a.UsedBytes != 5 || a.LastEnd == nil || a.StaleAfter != staleWeekly || !a.Enabled {
		t.Errorf("a: %+v", a)
	}
	if a.LastOKEnd == nil || !a.LastOKEnd.Equal(time.Date(2026, 11, 18, 2, 10, 0, 0, time.UTC)) {
		t.Errorf("a: last ok end %v, want the 2026-11-18 run", a.LastOKEnd)
	}
	if b.LastResult != "" || b.LastEnd != nil || b.Enabled || b.StaleAfter != staleDaily {
		t.Errorf("b: %+v", b)
	}

	// Cached within the TTL, re-read after it.
	os.WriteFile(cfg, []byte(`{"version":1,"tasks":[]}`), 0o600)
	now = now.Add(30 * time.Second)
	if len(s.Statuses()) != 2 {
		t.Error("expected cached result")
	}
	now = now.Add(31 * time.Second)
	if len(s.Statuses()) != 0 {
		t.Error("expected re-read after TTL")
	}
}

func TestBackupStatusSourceErrorsGiveEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tasks.json")
	os.WriteFile(cfg, []byte(`{broken`), 0o600)
	s := &BackupStatusSource{ConfigPath: cfg, HistoryPath: filepath.Join(dir, "h.json"), TTL: time.Minute, Now: time.Now}
	if got := s.Statuses(); got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}
