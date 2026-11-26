package monitor

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

// pageSize converts pswpin/pswpout pages to bytes; 4 KiB on every ARM and
// x86 kernel this project targets.
const pageSize = 4096

// Sample is one point of the history: rates are averages since the previous
// reading, memory is the state at T.
type Sample struct {
	T      int64    `json:"t"` // Unix milliseconds
	CPU    CPUShare `json:"cpu"`
	Memory Memory   `json:"memory"`
	// PSI is avg10 pressure in percent; null when the kernel has no PSI.
	PSI   *PSI       `json:"psi"`
	Disks []DiskRate `json:"disks"`
	Net   []NetRate  `json:"net"`
}

// CPUShare is percent of all CPU time; user includes nice, irq includes softirq.
type CPUShare struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	IOWait float64 `json:"iowait"`
	IRQ    float64 `json:"irq"`
	Steal  float64 `json:"steal"`
}

type Memory struct {
	UsedBytes      uint64  `json:"usedBytes"`
	CacheBytes     uint64  `json:"cacheBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	SwapUsedBytes  uint64  `json:"swapUsedBytes"`
	SwapInBps      float64 `json:"swapInBps"`
	SwapOutBps     float64 `json:"swapOutBps"`
}

type PSI struct {
	CPUSome    float64 `json:"cpuSome"`
	MemorySome float64 `json:"memorySome"`
	MemoryFull float64 `json:"memoryFull"`
	IOSome     float64 `json:"ioSome"`
	IOFull     float64 `json:"ioFull"`
}

type DiskRate struct {
	Name      string  `json:"name"`
	ReadBps   float64 `json:"readBps"`
	WriteBps  float64 `json:"writeBps"`
	ReadIOPS  float64 `json:"readIops"`
	WriteIOPS float64 `json:"writeIops"`
	// BusyPercent is the share of the interval with I/O in flight.
	BusyPercent float64 `json:"busyPercent"`
	// AwaitMs is the mean time per completed request; 0 without requests.
	AwaitMs float64 `json:"awaitMs"`
}

type NetRate struct {
	Name  string  `json:"name"`
	RxBps float64 `json:"rxBps"`
	TxBps float64 `json:"txBps"`
	// Errors and Drops are counts within the interval (rx + tx).
	Errors uint64 `json:"errors"`
	Drops  uint64 `json:"drops"`
}

// History is the response of GET /monitor.
type History struct {
	IntervalSeconds int    `json:"intervalSeconds"`
	PSIAvailable    bool   `json:"psiAvailable"`
	MemTotalBytes   uint64 `json:"memTotalBytes"`
	SwapTotalBytes  uint64 `json:"swapTotalBytes"`
	// Totals since boot per interface, for the totals line.
	NetTotals []NetTotal `json:"netTotals"`
	Samples   []Sample   `json:"samples"`
}

type NetTotal struct {
	Name     string `json:"name"`
	RxBytes  uint64 `json:"rxBytes"`
	TxBytes  uint64 `json:"txBytes"`
	RxErrors uint64 `json:"rxErrors"`
	TxErrors uint64 `json:"txErrors"`
	Drops    uint64 `json:"drops"`
}

// Monitor samples every Interval and keeps Size samples.
type Monitor struct {
	Root     string
	Interval time.Duration
	Size     int
	Now      func() time.Time

	mu     sync.Mutex
	prev   *raw
	prevAt time.Time
	ring   []Sample
	next   int // ring index of the next write
	full   bool
}

// New keeps one hour at 10 s (360 samples, roughly 100 KiB).
func New(root string) *Monitor {
	return &Monitor{Root: root, Interval: 10 * time.Second, Size: 360, Now: time.Now}
}

// Run samples until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	m.Tick()
	t := time.NewTicker(m.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Tick()
		}
	}
}

// Tick takes one reading and, from the second one on, stores a sample.
func (m *Monitor) Tick() {
	r, err := m.read()
	now := m.Now()
	if err != nil {
		slog.Warn("resource monitor reading failed", "err", err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prev != nil {
		if dt := now.Sub(m.prevAt).Seconds(); dt > 0 {
			m.push(compute(*m.prev, r, dt, now))
		}
	}
	m.prev, m.prevAt = &r, now
}

func (m *Monitor) push(s Sample) {
	if m.ring == nil {
		m.ring = make([]Sample, m.Size)
	}
	m.ring[m.next] = s
	m.next = (m.next + 1) % m.Size
	if m.next == 0 {
		m.full = true
	}
}

// History returns the samples newer than sinceMs (0 = all), oldest first.
func (m *Monitor) History(sinceMs int64) History {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := History{IntervalSeconds: int(m.Interval / time.Second), NetTotals: []NetTotal{}, Samples: []Sample{}}
	if m.prev != nil {
		h.PSIAvailable = m.prev.psi != nil
		h.MemTotalBytes, h.SwapTotalBytes = m.prev.mem.total, m.prev.mem.swapTotal
		for _, n := range m.prev.netList {
			c := m.prev.nets[n]
			h.NetTotals = append(h.NetTotals, NetTotal{Name: n, RxBytes: c.rxBytes, TxBytes: c.txBytes,
				RxErrors: c.rxErrors, TxErrors: c.txErrors, Drops: c.rxDropped + c.txDropped})
		}
	}
	start, count := 0, m.next
	if m.full {
		start, count = m.next, m.Size
	}
	for i := 0; i < count; i++ {
		s := m.ring[(start+i)%m.Size]
		if s.T > sinceMs {
			h.Samples = append(h.Samples, s)
		}
	}
	return h
}

// delta treats a counter that went backwards (wrap of a 32-bit counter,
// device reset) as no activity rather than a huge spike.
func delta(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func compute(p, c raw, dt float64, at time.Time) Sample {
	s := Sample{T: at.UnixMilli(), PSI: c.psi, Disks: []DiskRate{}, Net: []NetRate{}}

	// Sum of per-field deltas: a field that went backwards (iowait can on
	// some kernels) must not shrink the total and inflate the others.
	dc := cpuTicks{delta(c.cpu.user, p.cpu.user), delta(c.cpu.nice, p.cpu.nice), delta(c.cpu.system, p.cpu.system),
		delta(c.cpu.idle, p.cpu.idle), delta(c.cpu.iowait, p.cpu.iowait), delta(c.cpu.irq, p.cpu.irq),
		delta(c.cpu.softirq, p.cpu.softirq), delta(c.cpu.steal, p.cpu.steal)}
	if tot := float64(dc.total()); tot > 0 {
		pct := func(d uint64) float64 { return round1(float64(d) / tot * 100) }
		s.CPU = CPUShare{
			User:   pct(dc.user + dc.nice),
			System: pct(dc.system),
			IOWait: pct(dc.iowait),
			IRQ:    pct(dc.irq + dc.softirq),
			Steal:  pct(dc.steal),
		}
	}

	mi := c.mem
	cache := mi.buffers + mi.cached + mi.sreclaimable
	used := uint64(0)
	if mi.total > mi.free+cache {
		used = mi.total - mi.free - cache
	}
	swapUsed := uint64(0)
	if mi.swapTotal > mi.swapFree {
		swapUsed = mi.swapTotal - mi.swapFree
	}
	s.Memory = Memory{
		UsedBytes: used, CacheBytes: cache, AvailableBytes: mi.available, SwapUsedBytes: swapUsed,
		SwapInBps:  math.Round(float64(delta(c.swapIn, p.swapIn)*pageSize) / dt),
		SwapOutBps: math.Round(float64(delta(c.swapOut, p.swapOut)*pageSize) / dt),
	}

	for _, n := range c.diskList {
		cd := c.disks[n]
		pd, ok := p.disks[n]
		if !ok {
			continue // appeared this interval; rates start with the next sample
		}
		reads, writes := delta(cd.reads, pd.reads), delta(cd.writes, pd.writes)
		d := DiskRate{
			Name:        n,
			ReadBps:     math.Round(float64(delta(cd.readSectors, pd.readSectors)*512) / dt),
			WriteBps:    math.Round(float64(delta(cd.writeSectors, pd.writeSectors)*512) / dt),
			ReadIOPS:    round1(float64(reads) / dt),
			WriteIOPS:   round1(float64(writes) / dt),
			BusyPercent: round1(math.Min(100, float64(delta(cd.ioMs, pd.ioMs))/(dt*1000)*100)),
		}
		if ios := reads + writes; ios > 0 {
			d.AwaitMs = round1(float64(delta(cd.readMs, pd.readMs)+delta(cd.writeMs, pd.writeMs)) / float64(ios))
		}
		s.Disks = append(s.Disks, d)
	}

	for _, n := range c.netList {
		cn := c.nets[n]
		pn, ok := p.nets[n]
		if !ok {
			continue
		}
		s.Net = append(s.Net, NetRate{
			Name:   n,
			RxBps:  math.Round(float64(delta(cn.rxBytes, pn.rxBytes)) / dt),
			TxBps:  math.Round(float64(delta(cn.txBytes, pn.txBytes)) / dt),
			Errors: delta(cn.rxErrors, pn.rxErrors) + delta(cn.txErrors, pn.txErrors),
			Drops:  delta(cn.rxDropped, pn.rxDropped) + delta(cn.txDropped, pn.txDropped),
		})
	}
	return s
}
