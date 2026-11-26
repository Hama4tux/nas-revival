package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const meminfo = "MemTotal: 250000 kB\nMemFree: 20000 kB\nMemAvailable: 150000 kB\nBuffers: 10000 kB\n" +
	"Cached: 100000 kB\nSReclaimable: 10000 kB\nSwapTotal: 1000000 kB\nSwapFree: 990000 kB\n"

// first reading, NSA310 layout (diskstats with partitions, no PSI)
var reading1 = map[string]string{
	"proc/stat":    "cpu  1000 0 500 8000 400 0 100 0 0 0\n",
	"proc/meminfo": meminfo,
	"proc/vmstat":  "pgfault 1\npswpin 100\npswpout 200\n",
	"proc/diskstats": "   8       0 sda 1000 0 20000 3000 2000 0 40000 5000 0 10000 8000 0 0 0 0 0 0\n" +
		"   8       1 sda1 500 0 10000 1500 1000 0 20000 2500 0 5000 4000 0 0 0 0 0 0\n" +
		"   7       0 loop0 1 0 2 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n",
	"sys/class/net/enp1s0/statistics/rx_bytes":   "1000000\n",
	"sys/class/net/enp1s0/statistics/tx_bytes":   "500000\n",
	"sys/class/net/enp1s0/statistics/rx_errors":  "1\n",
	"sys/class/net/enp1s0/statistics/tx_errors":  "0\n",
	"sys/class/net/enp1s0/statistics/rx_dropped": "2\n",
	"sys/class/net/enp1s0/statistics/tx_dropped": "0\n",
	"sys/class/net/lo/statistics/rx_bytes":       "999\n",
}

// ten seconds later
var reading2 = map[string]string{
	"proc/stat":      "cpu  1300 100 700 9500 200 0 200 0 0 0\n", // iowait went backwards
	"proc/vmstat":    "pswpin 110\npswpout 200\n",
	"proc/diskstats": "   8       0 sda 1100 0 22048 3100 2100 0 50240 5900 0 15000 8000 0 0 0 0 0 0\n",
	"sys/class/net/enp1s0/statistics/rx_bytes":   "1100000\n",
	"sys/class/net/enp1s0/statistics/tx_bytes":   "520000\n",
	"sys/class/net/enp1s0/statistics/rx_errors":  "3\n",
	"sys/class/net/enp1s0/statistics/rx_dropped": "2\n",
}

func newTest(t *testing.T) (*Monitor, *time.Time) {
	root := t.TempDir()
	write(t, root, reading1)
	now := time.UnixMilli(1_000_000)
	m := &Monitor{Root: root, Interval: 10 * time.Second, Size: 3, Now: func() time.Time { return now }}
	return m, &now
}

func TestFirstTickStoresNoSample(t *testing.T) {
	m, _ := newTest(t)
	m.Tick()
	h := m.History(0)
	if len(h.Samples) != 0 {
		t.Fatalf("samples %+v", h.Samples)
	}
	if h.MemTotalBytes != 250000*1024 || h.PSIAvailable || h.IntervalSeconds != 10 {
		t.Fatalf("history %+v", h)
	}
	if len(h.NetTotals) != 1 || h.NetTotals[0].Name != "enp1s0" || h.NetTotals[0].Drops != 2 {
		t.Fatalf("totals %+v", h.NetTotals)
	}
}

func TestRates(t *testing.T) {
	m, now := newTest(t)
	m.Tick()
	write(t, m.Root, reading2)
	*now = now.Add(10 * time.Second)
	m.Tick()
	h := m.History(0)
	if len(h.Samples) != 1 {
		t.Fatalf("samples %d", len(h.Samples))
	}
	s := h.Samples[0]
	// total delta = 400 user+nice, 200 system, 1500 idle, 0 iowait (clamped), 100 irq = 2200
	if s.CPU.User != 18.2 || s.CPU.System != 9.1 || s.CPU.IOWait != 0 || s.CPU.IRQ != 4.5 {
		t.Fatalf("cpu %+v", s.CPU)
	}
	if s.Memory.CacheBytes != 120000*1024 || s.Memory.UsedBytes != 110000*1024 ||
		s.Memory.SwapUsedBytes != 10000*1024 || s.Memory.SwapInBps != 4096 || s.Memory.SwapOutBps != 0 {
		t.Fatalf("memory %+v", s.Memory)
	}
	if len(s.Disks) != 1 {
		t.Fatalf("disks %+v (partitions and loop must be skipped)", s.Disks)
	}
	d := s.Disks[0]
	if d.Name != "sda" || d.ReadBps != 104858 || d.WriteBps != 10240*512/10 ||
		d.ReadIOPS != 10 || d.WriteIOPS != 10 || d.BusyPercent != 50 || d.AwaitMs != 5 {
		t.Fatalf("disk %+v", d)
	}
	if len(s.Net) != 1 || s.Net[0].RxBps != 10000 || s.Net[0].TxBps != 2000 || s.Net[0].Errors != 2 || s.Net[0].Drops != 0 {
		t.Fatalf("net %+v", s.Net)
	}
	if s.PSI != nil {
		t.Fatalf("psi %+v", s.PSI)
	}
}

func TestRingKeepsNewestAndSince(t *testing.T) {
	m, now := newTest(t)
	for i := 0; i < 6; i++ {
		m.Tick()
		*now = now.Add(10 * time.Second)
	}
	h := m.History(0)
	if len(h.Samples) != 3 {
		t.Fatalf("samples %d", len(h.Samples))
	}
	if h.Samples[0].T >= h.Samples[1].T || h.Samples[1].T >= h.Samples[2].T {
		t.Fatalf("not oldest first: %d %d %d", h.Samples[0].T, h.Samples[1].T, h.Samples[2].T)
	}
	since := m.History(h.Samples[1].T)
	if len(since.Samples) != 1 || since.Samples[0].T != h.Samples[2].T {
		t.Fatalf("since %+v", since.Samples)
	}
}

func TestPSI(t *testing.T) {
	m, now := newTest(t)
	write(t, m.Root, map[string]string{
		"proc/pressure/cpu":    "some avg10=1.50 avg60=1.00 avg300=0.50 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"proc/pressure/memory": "some avg10=2.25 avg60=0 avg300=0 total=1\nfull avg10=0.75 avg60=0 avg300=0 total=1\n",
		"proc/pressure/io":     "some avg10=10.00 avg60=0 avg300=0 total=1\nfull avg10=5.00 avg60=0 avg300=0 total=1\n",
	})
	m.Tick()
	*now = now.Add(10 * time.Second)
	m.Tick()
	h := m.History(0)
	p := h.Samples[0].PSI
	if !h.PSIAvailable || p == nil || p.CPUSome != 1.5 || p.MemorySome != 2.25 || p.MemoryFull != 0.75 || p.IOSome != 10 || p.IOFull != 5 {
		t.Fatalf("psi %+v %+v", h.PSIAvailable, p)
	}
}

func TestMissingStatFails(t *testing.T) {
	m := &Monitor{Root: t.TempDir(), Interval: time.Second, Size: 2, Now: time.Now}
	m.Tick()
	m.Tick()
	if h := m.History(0); len(h.Samples) != 0 || h.MemTotalBytes != 0 {
		t.Fatalf("history %+v", h)
	}
}
