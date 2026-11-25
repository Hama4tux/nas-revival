package system

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/hal"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var minimal = map[string]string{
	"proc/sys/kernel/hostname":  "nas",
	"proc/sys/kernel/osrelease": "6.12.119",
	"proc/uptime":               "100.0 90.0",
	"proc/cpuinfo":              "processor : 0\nmodel name : Feroceon\n",
	"proc/loadavg":              "0.5 0.4 0.3 1/50 9",
	"proc/stat":                 "cpu 100 0 0 900 0 0 0 0 0 0\n",
	"proc/meminfo":              "MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
	"proc/self/mounts":          "/dev/sda1 / ext4 rw 0 0\n/dev/sda3 /srv/data ext4 rw 0 0\n",
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCollector(t *testing.T, files map[string]string) (*Collector, string, *clock) {
	root := writeTree(t, files)
	clk := &clock{t: time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)}
	c := &Collector{
		HAL: hal.Reader{Root: root},
		StatFS: func(p string) (FSUsage, error) {
			if p == "/srv/data" {
				return FSUsage{}, errors.New("stale handle")
			}
			return FSUsage{Total: 1000, Free: 400, Avail: 300}, nil
		},
		Addrs: func(string) []string { return []string{} },
		Now:   clk.now,
	}
	return c, root, clk
}

func TestCollectDegradesGracefully(t *testing.T) {
	c, _, _ := newTestCollector(t, minimal)
	o, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.CPU.UsagePercent != nil {
		t.Fatal("first sample must not report CPU usage")
	}
	if o.Sensors.Status != hal.SensorUnsupported {
		t.Fatalf("sensors %s", o.Sensors.Status)
	}
	if len(o.Storage.Filesystems) != 1 || o.Storage.Filesystems[0].UsedBytes != 600 {
		t.Fatalf("filesystems %+v (failed statfs must be skipped)", o.Storage.Filesystems)
	}
	// Lists must serialise as [] not null so the UI never has to guess.
	b, _ := json.Marshal(o)
	for _, k := range []string{`"network":[]`, `"disks":[]`, `"services":[]`, `"usagePercent":null`, `"boardModel":null`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("json lacks %s: %s", k, b)
		}
	}
}

func TestCPUUsageFromTwoSamples(t *testing.T) {
	c, root, clk := newTestCollector(t, minimal)
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	// +100 busy ticks, +300 idle ticks → 25 %.
	if err := os.WriteFile(filepath.Join(root, "proc/stat"), []byte("cpu 200 0 0 1200 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(500 * time.Millisecond)
	o, _ := c.Collect(context.Background())
	if o.CPU.UsagePercent != nil {
		t.Fatal("sample closer than 1 s must reuse the previous value")
	}
	clk.t = clk.t.Add(5 * time.Second)
	o, _ = c.Collect(context.Background())
	if o.CPU.UsagePercent == nil || *o.CPU.UsagePercent != 25 {
		t.Fatalf("usage %v", o.CPU.UsagePercent)
	}
}

func TestCollectFailsWithoutProc(t *testing.T) {
	c, _, _ := newTestCollector(t, map[string]string{})
	if _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestServiceChecker(t *testing.T) {
	var gotArgs []string
	sc := &ServiceChecker{
		Units: []Unit{{"ssh.service", "SSH"}, {"smbd.service", "Samba"}, {"bad.service", "Bad"}, {"nfs-server.service", "NFS"}},
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			gotArgs = args
			return []byte("Id=ssh.service\nLoadState=loaded\nActiveState=active\n\n" +
				"Id=smbd.service\nLoadState=not-found\nActiveState=inactive\n\n" +
				"Id=bad.service\nLoadState=loaded\nActiveState=failed\n\n" +
				"Id=nfs-server.service\nLoadState=loaded\nActiveState=activating\n"), nil
		},
	}
	got := sc.Check(context.Background())
	want := []Service{{"ssh.service", "SSH", "active"}, {"bad.service", "Bad", "failed"}, {"nfs-server.service", "NFS", "unknown"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if gotArgs[0] != "show" || gotArgs[2] != "--" {
		t.Fatalf("unit names must follow --: %v", gotArgs)
	}
}

func TestServiceCheckerSystemctlMissing(t *testing.T) {
	sc := &ServiceChecker{
		Units: []Unit{{"ssh.service", "SSH"}},
		Run:   func(context.Context, ...string) ([]byte, error) { return nil, errors.New("not found") },
	}
	got := sc.Check(context.Background())
	if len(got) != 1 || got[0].State != "unknown" {
		t.Fatalf("got %+v", got)
	}
}

func TestBoardProfileFiltersUnwiredFans(t *testing.T) {
	files := map[string]string{}
	for k, v := range minimal {
		files[k] = v
	}
	files["proc/device-tree/model"] = "ZyXEL NSA310\x00"
	files["sys/class/hwmon/hwmon0/name"] = "lm85"
	files["sys/class/hwmon/hwmon0/fan1_input"] = "9075"
	files["sys/class/hwmon/hwmon0/fan2_input"] = "0"
	files["sys/class/hwmon/hwmon0/temp1_input"] = "31000"
	c, _, _ := newTestCollector(t, files)
	o, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Sensors.Fans) != 1 || o.Sensors.Fans[0].Label != "Ventilátor" || o.Sensors.Fans[0].RPM != 9075 {
		t.Fatalf("fans %+v", o.Sensors.Fans)
	}

	delete(files, "proc/device-tree/model")
	c, _, _ = newTestCollector(t, files)
	o, _ = c.Collect(context.Background())
	if len(o.Sensors.Fans) != 2 {
		t.Fatalf("unknown board must report all fans: %+v", o.Sensors.Fans)
	}
}
