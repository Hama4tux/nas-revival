package smart

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// testdata/st1000lm014.json is real `smartctl -j -a` output of the NAS disk
// (serial number and WWN removed).
func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/st1000lm014.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func attr(t *testing.T, d Detail, id int) Attribute {
	t.Helper()
	for _, a := range d.Attributes {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("attribute %d missing", id)
	return Attribute{}
}

func TestParseDetailRealDisk(t *testing.T) {
	d := ParseDetail(loadFixture(t))
	if d.Health != "passed" || d.Standby || *d.Family != "Seagate Laptop SSHD" || *d.PowerCycles != 10115 {
		t.Fatalf("summary: %+v", d)
	}
	if !d.SelfTestsSupported || *d.ShortMinutes != 1 || *d.LongMinutes != 206 {
		t.Fatalf("capabilities: %+v", d)
	}
	if d.SelfTest == nil || d.SelfTest.Running || d.SelfTest.RemainingPercent != nil {
		t.Fatalf("self-test: %+v", d.SelfTest)
	}
	if len(d.Attributes) != 21 || len(d.History) != 4 {
		t.Fatalf("attributes %d history %d", len(d.Attributes), len(d.History))
	}
	// Seagate packs the error count into the upper 16 bits: 68992411861 = 16<<32 + seeks.
	if a := attr(t, d, 7); a.Count != 16 || a.Level != LevelOK || a.Key {
		t.Fatalf("seek errors: %+v", a)
	}
	if a := attr(t, d, 194); a.Count != 42 || a.Level != LevelInfo || !a.Key {
		t.Fatalf("temperature: %+v", a)
	}
	if a := attr(t, d, 5); a.Count != 0 || a.Level != LevelInfo || !a.Prefailure {
		t.Fatalf("reallocated: %+v", a)
	}
	h := d.History[0]
	if h.Type != "long" || h.Passed || *h.RemainingPercent != 80 || h.LifetimeHours != 6866 {
		t.Fatalf("history[0]: %+v", h)
	}
	if h := d.History[3]; h.Type != "short" || !h.Passed {
		t.Fatalf("history[3]: %+v", h)
	}
}

func TestParseDetailRunningTest(t *testing.T) {
	var j map[string]any
	if err := json.Unmarshal(loadFixture(t), &j); err != nil {
		t.Fatal(err)
	}
	st := j["ata_smart_data"].(map[string]any)["self_test"].(map[string]any)
	st["status"] = map[string]any{"value": 249, "string": "in progress, 90% remaining"}
	raw, _ := json.Marshal(j)
	d := ParseDetail(raw)
	if d.SelfTest == nil || !d.SelfTest.Running || d.SelfTest.RemainingPercent == nil || *d.SelfTest.RemainingPercent != 90 {
		t.Fatalf("running: %+v", d.SelfTest)
	}
}

func TestLevels(t *testing.T) {
	v, th := 5, 10
	cases := []struct {
		name string
		a    Attribute
		want string
	}{
		{"pending sectors", Attribute{ID: 197, Count: 1, Key: true}, LevelCrit},
		{"crc errors", Attribute{ID: 199, Count: 3, Key: true}, LevelWarn},
		{"hot", Attribute{ID: 194, Count: 52, Key: true}, LevelWarn},
		{"too hot", Attribute{ID: 194, Count: 61, Key: true}, LevelCrit},
		{"load cycles", Attribute{ID: 193, Count: 310_000, Key: true}, LevelWarn},
		{"below threshold", Attribute{ID: 3, Value: &v, Thresh: &th, Prefailure: true}, LevelCrit},
		{"failed in the past", Attribute{ID: 3, WhenFailed: "past"}, LevelWarn},
		{"failing now", Attribute{ID: 3, WhenFailed: "now"}, LevelCrit},
		{"plain", Attribute{ID: 3}, LevelOK},
	}
	for _, c := range cases {
		if got := level(c.a); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDecodeCount(t *testing.T) {
	if got := decodeCount(188, 1<<32+2<<16+3, true); got != 6 {
		t.Errorf("seagate 188 = %d", got)
	}
	if got := decodeCount(188, 5, false); got != 5 {
		t.Errorf("other 188 = %d", got)
	}
	if got := decodeCount(9, 7<<32+6870, false); got != 6870 {
		t.Errorf("hours = %d", got)
	}
}

func TestParseDetailStandbyAndGarbage(t *testing.T) {
	if d := ParseDetail([]byte(standby)); !d.Standby || d.Attributes == nil || d.History == nil {
		t.Fatalf("standby: %+v", d)
	}
	if d := ParseDetail([]byte("nope")); d.Health != "unknown" || d.Attributes == nil {
		t.Fatalf("garbage: %+v", d)
	}
}

func TestDetailServiceCacheAndInvalidate(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	calls := 0
	s := &DetailService{TTL: 10 * time.Second, Now: func() time.Time { return now },
		Run: func(context.Context, string) ([]byte, error) { calls++; return []byte(healthy), nil }}
	s.Detail(context.Background(), "sda")
	s.Detail(context.Background(), "sda")
	if calls != 1 {
		t.Fatalf("expected cache, calls=%d", calls)
	}
	s.Invalidate("sda")
	s.Detail(context.Background(), "sda")
	if calls != 2 {
		t.Fatalf("invalidate must drop cache, calls=%d", calls)
	}
	u := &DetailService{TTL: time.Second, Now: time.Now,
		Run: func(context.Context, string) ([]byte, error) { return nil, errUnavailable }}
	if d := u.Detail(context.Background(), "sda"); d.Error != "smartctl_unavailable" {
		t.Fatalf("unavailable: %+v", d)
	}
}
