package smart

import (
	"context"
	"testing"
	"time"
)

const healthy = `{"smartctl":{"exit_status":0,"messages":[]},"model_name":"ST1000LM014-1EJ164",
"smart_status":{"passed":true},"temperature":{"current":38},"power_on_time":{"hours":1200},
"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":0}},{"id":9,"raw":{"value":1200}},{"id":197,"raw":{"value":2}},{"id":4,"raw":{"value":310}},{"id":193,"raw":{"value":35003}}]}}`

const standby = `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]}}`

func TestParseHealthy(t *testing.T) {
	d := Parse("sda", []byte(healthy))
	if d.Health != "passed" || *d.Model != "ST1000LM014-1EJ164" || *d.TemperatureCelsius != 38 ||
		*d.PowerOnHours != 1200 || *d.ReallocatedSectors != 0 || *d.PendingSectors != 2 || d.Standby ||
		*d.StartStopCount != 310 || *d.LoadCycleCount != 35003 {
		t.Fatalf("got %+v", d)
	}
}

func TestParseFailed(t *testing.T) {
	d := Parse("sdb", []byte(`{"smart_status":{"passed":false}}`))
	if d.Health != "failed" || d.TemperatureCelsius != nil {
		t.Fatalf("got %+v", d)
	}
}

func TestParseStandbyAndGarbage(t *testing.T) {
	if d := Parse("sda", []byte(standby)); !d.Standby || d.Health != "unknown" {
		t.Fatalf("standby: %+v", d)
	}
	if d := Parse("sda", []byte("not json")); d.Health != "unknown" {
		t.Fatalf("garbage: %+v", d)
	}
}

func TestServiceCachesAndReportsUnavailable(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	calls := 0
	s := &Service{
		Run: func(context.Context, string) ([]byte, error) {
			calls++
			return []byte(healthy), nil
		},
		Devices: func() []string { return []string{"sda"} },
		TTL:     10 * time.Minute,
		Now:     func() time.Time { return now },
	}
	s.Report(context.Background())
	now = now.Add(time.Minute)
	if r := s.Report(context.Background()); len(r.Disks) != 1 || calls != 1 {
		t.Fatalf("expected cached result, calls=%d", calls)
	}
	now = now.Add(10 * time.Minute)
	s.Report(context.Background())
	if calls != 2 {
		t.Fatalf("cache must expire, calls=%d", calls)
	}

	u := &Service{
		Run:     func(context.Context, string) ([]byte, error) { return nil, errUnavailable },
		Devices: func() []string { return []string{"sda"} },
		TTL:     time.Minute, Now: time.Now,
	}
	if r := u.Report(context.Background()); r.Error != "smartctl_unavailable" || r.Disks == nil || len(r.Disks) != 0 {
		t.Fatalf("got %+v", r)
	}
}
