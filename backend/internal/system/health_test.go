package system

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/smart"
)

var t0 = time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)

func healthy() Overview {
	return Overview{
		Sensors: Sensors{Status: hal.SensorOK, Fans: []Fan{{ID: "f", Label: "Ventilátor", RPM: 1800}}},
		Storage: Storage{Filesystems: []Filesystem{{Mount: "/", TotalBytes: 100, UsedBytes: 50}}},
		Services: []Service{{Name: "ssh.service", Label: "SSH", State: "active"}},
	}
}

func i64(v int64) *int64 { return &v }

func codes(h Health) []string {
	out := []string{}
	for _, p := range h.Problems {
		out = append(out, p.Severity+":"+p.Code+":"+p.Subject)
	}
	return out
}

func TestEvaluateHealthy(t *testing.T) {
	h := Evaluate(healthy(), &smart.Report{CheckedAt: t0, Disks: []smart.Disk{{Name: "sda", Health: "passed"}}}, t0)
	if h.Status != HealthOK || len(h.Problems) != 0 || !h.CheckedAt.Equal(t0) || h.SmartCheckedAt == nil {
		t.Fatalf("%+v", h)
	}
	if h := Evaluate(healthy(), nil, t0); h.SmartCheckedAt != nil || h.Problems == nil {
		t.Fatalf("without SMART: %+v (problems must be [] not null)", h)
	}
}

func TestEvaluateFilesystemThresholds(t *testing.T) {
	for used, want := range map[uint64]string{85: HealthOK, 86: HealthWarn, 95: HealthWarn, 96: HealthCrit} {
		o := healthy()
		o.Storage.Filesystems[0].UsedBytes = used
		if got := Evaluate(o, nil, t0).Status; got != want {
			t.Errorf("used %d%%: %s, want %s", used, got, want)
		}
	}
	o := healthy()
	o.Storage.Filesystems[0].TotalBytes = 0
	if Evaluate(o, nil, t0).Status != HealthOK {
		t.Error("empty filesystem must not divide by zero")
	}
}

func TestEvaluateOrdersCritFirst(t *testing.T) {
	o := healthy()
	o.Sensors.Fans[0].RPM = 0
	o.Services[0].State = "failed"
	o.Sensors.Status = hal.SensorUnsupported
	rep := &smart.Report{Disks: []smart.Disk{
		{Name: "sda", Health: "passed", PendingSectors: i64(2)},
		{Name: "sdb", Health: "failed"},
		{Name: "sdc", Health: "failed", Standby: true},
	}}
	h := Evaluate(o, rep, t0)
	want := []string{
		"crit:smartFailed:sdb", "crit:serviceFailed:SSH",
		"warn:sensorsUnknown:", "warn:fanStopped:Ventilátor", "warn:smartSectors:sda",
	}
	if h.Status != HealthCrit || len(codes(h)) != len(want) {
		t.Fatalf("%v", codes(h))
	}
	for i, c := range codes(h) {
		if c != want[i] {
			t.Fatalf("got %v, want %v", codes(h), want)
		}
	}
	o = healthy()
	o.Sensors.Status = hal.SensorFault
	if got := codes(Evaluate(o, nil, t0)); len(got) != 1 || got[0] != "crit:sensorsFault:" {
		t.Fatalf("fault: %v", got)
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newSnaps(collect func(context.Context) (Overview, error)) (*Snapshots, *fakeClock) {
	clk := &fakeClock{t: t0}
	s := NewSnapshots(collect)
	s.Now, s.Wait = clk.now, 50*time.Millisecond
	return s, clk
}

func TestSnapshotsShareOneCollection(t *testing.T) {
	var n atomic.Int32
	s, clk := newSnaps(func(context.Context) (Overview, error) {
		n.Add(1)
		return Overview{Host: Host{Hostname: "nas"}, Health: Health{Status: HealthOK}}, nil
	})
	s.Refresh(context.Background())
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if o, err := s.Snapshot(context.Background()); err != nil || o.Host.Hostname != "nas" {
				t.Errorf("%v %v", o.Host, err)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatalf("3 clients caused %d collections, want 1", n.Load())
	}
	if s.interval() != s.Active {
		t.Fatal("a watched collector must run at the active interval")
	}
	clk.add(s.Watch)
	if s.interval() != s.Idle {
		t.Fatal("nobody watching: idle interval")
	}
}

func TestSnapshotsWakeAfterIdle(t *testing.T) {
	var n atomic.Int32
	s, clk := newSnaps(func(context.Context) (Overview, error) {
		n.Add(1)
		return Overview{GeneratedAt: t0, Health: Health{Status: HealthOK}}, nil
	})
	s.Idle = time.Hour // only a wake-up can trigger the second collection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	for n.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	clk.add(time.Minute)
	s.Wait = 2 * time.Second
	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("collections %d, want a fresh one after idle", n.Load())
	}
}

func TestSnapshotsFailureMarksUnknown(t *testing.T) {
	fail := false
	s, _ := newSnaps(func(context.Context) (Overview, error) {
		if fail {
			return Overview{}, errors.New("proc unreadable")
		}
		return Overview{Host: Host{Hostname: "nas"}, Health: Health{Status: HealthCrit,
			Problems: []Problem{{Severity: HealthCrit, Code: ProblemServiceFailed, Subject: "SSH"}}}}, nil
	})
	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Fatal("no snapshot yet must be an error")
	}
	s.Refresh(context.Background())
	fail = true
	s.Refresh(context.Background())
	s.Refresh(context.Background())
	o, err := s.Snapshot(context.Background())
	if err != nil || o.Host.Hostname != "nas" {
		t.Fatalf("last good snapshot expected: %v", err)
	}
	if got := codes(o.Health); o.Health.Status != HealthUnknown || len(got) != 2 ||
		got[0] != "warn:collectFailed:" || got[1] != "crit:serviceFailed:SSH" {
		t.Fatalf("%s %v", o.Health.Status, got)
	}
	fail = false
	s.Refresh(context.Background())
	if o, _ := s.Snapshot(context.Background()); o.Health.Status != HealthCrit {
		t.Fatalf("recovered: %s", o.Health.Status)
	}
}

func TestCollectPowerStateCached(t *testing.T) {
	files := map[string]string{}
	for k, v := range minimal {
		files[k] = v
	}
	files["sys/block/sda/size"] = "100"
	files["sys/block/sda/removable"] = "0"
	files["sys/block/sda/queue/rotational"] = "1"
	files["sys/block/sdb/size"] = "100"
	files["sys/block/sdb/removable"] = "1"
	files["sys/block/sdb/queue/rotational"] = "1"
	c, _, clk := newTestCollector(t, files)
	asked := map[string]int{}
	c.DiskPower = func(_ context.Context, dev string) string { asked[dev]++; return "standby" }
	c.PowerTTL = time.Minute
	for range 2 {
		o, err := c.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range o.Storage.Disks {
			if d.Name == "sda" && (d.PowerState == nil || *d.PowerState != "standby") {
				t.Fatalf("sda %v", d.PowerState)
			}
			if d.Name == "sdb" && d.PowerState != nil {
				t.Fatal("removable disk must not be asked")
			}
		}
	}
	if asked["sda"] != 1 || asked["sdb"] != 0 {
		t.Fatalf("asked %v, want sda once within TTL", asked)
	}
	clk.t = clk.t.Add(time.Minute)
	if _, err := c.Collect(context.Background()); err != nil || asked["sda"] != 2 {
		t.Fatalf("after TTL asked %v", asked)
	}
}
