// Package system assembles the read-only overview snapshot served by the API.
package system

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/hardware"
	"nas-revival/backend/internal/smart"
)

type Overview struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Host        Host           `json:"host"`
	CPU         CPU            `json:"cpu"`
	Memory      Memory         `json:"memory"`
	Sensors     Sensors        `json:"sensors"`
	Network     []NetIface     `json:"network"`
	Storage     Storage        `json:"storage"`
	Services    []Service      `json:"services"`
	Backups     []BackupStatus `json:"backups"`
	Health      Health         `json:"health"`
}

// BackupStatus is the last result of one backup-pull task.
type BackupStatus struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// LastResult is ok|failed|skipped|stopped, empty when it never ran.
	LastResult string     `json:"lastResult"`
	LastEnd    *time.Time `json:"lastEnd"`
	Message    string     `json:"message"`
	UsedBytes  int64      `json:"usedBytes"`
	// LastOKEnd is the end of the last successful run (or of the oldest
	// recorded run when none succeeded); staleness counts from it.
	LastOKEnd *time.Time `json:"-"`
	// StaleAfter is how old LastOKEnd may get before the backup counts as
	// overdue (from the schedule); zero disables the check.
	StaleAfter time.Duration `json:"-"`
}

type Host struct {
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Kernel        string  `json:"kernel"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
	BoardModel    *string `json:"boardModel"`
}

type CPU struct {
	Model        string   `json:"model"`
	Cores        int      `json:"cores"`
	Load1        float64  `json:"load1"`
	Load5        float64  `json:"load5"`
	Load15       float64  `json:"load15"`
	UsagePercent *float64 `json:"usagePercent"`
}

type Memory struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	SwapTotalBytes uint64 `json:"swapTotalBytes"`
	SwapFreeBytes  uint64 `json:"swapFreeBytes"`
}

type Temperature struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	MilliCelsius int64  `json:"milliCelsius"`
}

type Fan struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	RPM   int64  `json:"rpm"`
}

type Sensors struct {
	Status       string        `json:"status"`
	Temperatures []Temperature `json:"temperatures"`
	Fans         []Fan         `json:"fans"`
}

type NetIface struct {
	Name      string   `json:"name"`
	Up        bool     `json:"up"`
	SpeedMbps *int64   `json:"speedMbps"`
	RxBytes   uint64   `json:"rxBytes"`
	TxBytes   uint64   `json:"txBytes"`
	Addresses []string `json:"addresses"`
}

type Disk struct {
	Name       string  `json:"name"`
	Model      *string `json:"model"`
	SizeBytes  uint64  `json:"sizeBytes"`
	Rotational bool    `json:"rotational"`
	Removable  bool    `json:"removable"`
	// PowerState is active, standby, sleeping or unknown (hdparm -C, never
	// wakes the drive); null for removable disks, which are not asked.
	PowerState *string `json:"powerState"`
}

type Filesystem struct {
	Mount          string `json:"mount"`
	Device         string `json:"device"`
	FSType         string `json:"fsType"`
	TotalBytes     uint64 `json:"totalBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type Storage struct {
	Disks       []Disk       `json:"disks"`
	Filesystems []Filesystem `json:"filesystems"`
}

// FSUsage is the subset of statfs needed for a filesystem usage bar.
type FSUsage struct {
	Total, Free, Avail uint64
}

// Collector produces Overview snapshots. Dependencies are fields so tests can
// replace the parts that cannot come from a fixture tree.
type Collector struct {
	HAL      hal.Reader
	StatFS   func(path string) (FSUsage, error)
	Addrs    func(iface string) []string
	Services *ServiceChecker
	Now      func() time.Time
	// Smart supplies the cached SMART summary for health; nil skips SMART.
	Smart SmartReporter
	// Backups returns the backup-pull task states; nil means none.
	Backups func() []BackupStatus
	// DiskPower returns a disk's power state; nil leaves powerState null.
	// Answers are cached for PowerTTL so a watched dashboard does not keep
	// querying the drive.
	DiskPower func(ctx context.Context, device string) string
	PowerTTL  time.Duration
	// Thresholds returns the temperature thresholds; nil uses the defaults.
	Thresholds func() hardware.Thresholds

	mu      sync.Mutex
	prev    *hal.CPUTimes
	prevAt  time.Time
	lastUse *float64
	power   map[string]powerSample
}

// SmartReporter is the cached SMART service (smart.Service).
type SmartReporter interface {
	Report(ctx context.Context) smart.Report
}

type powerSample struct {
	state string
	at    time.Time
}

func NewCollector(root string, services *ServiceChecker) *Collector {
	return &Collector{
		HAL:      hal.Reader{Root: root},
		StatFS:   statFS,
		Addrs:    interfaceAddrs,
		Services: services,
		Now:      time.Now,
	}
}

// minCPUSampleInterval stops concurrent clients from producing noisy usage
// figures over a few milliseconds of ticks.
const minCPUSampleInterval = time.Second

// Collect builds a snapshot. Host, CPU and memory are required; everything else
// degrades to empty lists so one broken subsystem does not blank the dashboard.
func (c *Collector) Collect(ctx context.Context) (Overview, error) {
	o := Overview{GeneratedAt: c.Now().UTC()}

	h, err := c.HAL.Host()
	if err != nil {
		return o, err
	}
	o.Host = Host{Hostname: h.Hostname, OS: h.OS, Kernel: h.Kernel, UptimeSeconds: h.UptimeSeconds, BoardModel: h.BoardModel}

	cpu, err := c.HAL.CPU()
	if err != nil {
		return o, err
	}
	o.CPU = CPU{Model: cpu.Model, Cores: cpu.Cores, Load1: cpu.Load1, Load5: cpu.Load5, Load15: cpu.Load15}
	o.CPU.UsagePercent = c.cpuUsage()

	mem, err := c.HAL.Memory()
	if err != nil {
		return o, err
	}
	o.Memory = Memory(mem)

	s := c.HAL.Sensors()
	o.Sensors = Sensors{Status: s.Status, Temperatures: []Temperature{}, Fans: []Fan{}}
	prof := profileFor(h.BoardModel)
	for _, t := range s.Temperatures {
		if label, ok := prof.Temps[t.ID]; ok {
			t.Label = label
		}
		o.Sensors.Temperatures = append(o.Sensors.Temperatures, Temperature(t))
	}
	for _, f := range s.Fans {
		if len(prof.Fans) > 0 {
			label, wired := prof.Fans[f.ID]
			if !wired {
				continue
			}
			f.Label = label
		}
		o.Sensors.Fans = append(o.Sensors.Fans, Fan(f))
	}

	o.Network = []NetIface{}
	if ifs, err := c.HAL.NetInterfaces(); err != nil {
		slog.Warn("network interfaces unavailable", "err", err)
	} else {
		for _, i := range ifs {
			o.Network = append(o.Network, NetIface{
				Name: i.Name, Up: i.Up, SpeedMbps: i.SpeedMbps,
				RxBytes: i.RxBytes, TxBytes: i.TxBytes, Addresses: c.Addrs(i.Name),
			})
		}
	}

	o.Storage = Storage{Disks: []Disk{}, Filesystems: []Filesystem{}}
	if ds, err := c.HAL.Disks(); err != nil {
		slog.Warn("disks unavailable", "err", err)
	} else {
		for _, d := range ds {
			disk := Disk{Name: d.Name, Model: d.Model, SizeBytes: d.SizeBytes, Rotational: d.Rotational, Removable: d.Removable}
			disk.PowerState = c.powerState(ctx, d)
			o.Storage.Disks = append(o.Storage.Disks, disk)
		}
	}
	if ms, err := c.HAL.Mounts(); err != nil {
		slog.Warn("mounts unavailable", "err", err)
	} else {
		for _, m := range ms {
			u, err := c.StatFS(m.Point)
			if err != nil {
				slog.Warn("statfs failed", "mount", m.Point, "err", err)
				continue
			}
			o.Storage.Filesystems = append(o.Storage.Filesystems, Filesystem{
				Mount: m.Point, Device: m.Device, FSType: m.FSType,
				TotalBytes: u.Total, UsedBytes: u.Total - u.Free, AvailableBytes: u.Avail,
			})
		}
	}

	o.Services = []Service{}
	if c.Services != nil {
		o.Services = c.Services.Check(ctx)
	}

	o.Backups = []BackupStatus{}
	if c.Backups != nil {
		if b := c.Backups(); b != nil {
			o.Backups = b
		}
	}

	var rep *smart.Report
	if c.Smart != nil {
		r := c.Smart.Report(ctx)
		rep = &r
	}
	th := hardware.Defaults().Thresholds
	if c.Thresholds != nil {
		th = c.Thresholds()
	}
	o.Health = Evaluate(o, rep, th, o.GeneratedAt)
	return o, nil
}

// powerState asks internal sd* disks only: USB bridges often answer
// CHECK POWER MODE with garbage.
func (c *Collector) powerState(ctx context.Context, d hal.Disk) *string {
	if c.DiskPower == nil || d.Removable || !strings.HasPrefix(d.Name, "sd") {
		return nil
	}
	now := c.Now()
	c.mu.Lock()
	ps, ok := c.power[d.Name]
	c.mu.Unlock()
	if !ok || now.Sub(ps.at) >= c.PowerTTL {
		ps = powerSample{state: c.DiskPower(ctx, d.Name), at: now}
		c.mu.Lock()
		if c.power == nil {
			c.power = map[string]powerSample{}
		}
		c.power[d.Name] = ps
		c.mu.Unlock()
	}
	st := ps.state
	return &st
}

func (c *Collector) cpuUsage() *float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	if c.prev != nil && now.Sub(c.prevAt) < minCPUSampleInterval {
		return c.lastUse
	}
	cur, err := c.HAL.CPUTimes()
	if err != nil {
		slog.Warn("cpu times unavailable", "err", err)
		return nil
	}
	if c.prev != nil && cur.Total > c.prev.Total {
		dt := float64(cur.Total - c.prev.Total)
		di := float64(cur.Idle - c.prev.Idle)
		u := (dt - di) / dt * 100
		u = float64(int(u*10+0.5)) / 10
		c.lastUse = &u
	}
	c.prev, c.prevAt = &cur, now
	return c.lastUse
}

func statFS(path string) (FSUsage, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return FSUsage{}, err
	}
	bs := uint64(s.Bsize)
	return FSUsage{Total: uint64(s.Blocks) * bs, Free: uint64(s.Bfree) * bs, Avail: uint64(s.Bavail) * bs}, nil
}

func interfaceAddrs(name string) []string {
	out := []string{}
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, a.String())
	}
	return out
}
