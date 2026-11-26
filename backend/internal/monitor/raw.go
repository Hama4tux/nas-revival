// Package monitor keeps the Resource Monitor history (W3.1, MON-02..05): one
// sample every Interval from /proc and /sys, the last hour in a RAM ring.
// Every source is a counter file that never touches a disk, so sampling does
// not wake a sleeping drive.
package monitor

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// raw holds one reading of the cumulative counters.
type raw struct {
	cpu      cpuTicks
	mem      memInfo
	swapIn   uint64 // pages
	swapOut  uint64
	psi      *PSI // nil when the kernel has no CONFIG_PSI
	disks    map[string]diskCounters
	diskList []string
	nets     map[string]netCounters
	netList  []string
}

type cpuTicks struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (c cpuTicks) total() uint64 {
	return c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
}

type memInfo struct {
	total, available, free, buffers, cached, sreclaimable, swapTotal, swapFree uint64 // bytes
}

type diskCounters struct {
	reads, readSectors, readMs, writes, writeSectors, writeMs, ioMs uint64
}

type netCounters struct {
	rxBytes, txBytes, rxErrors, txErrors, rxDropped, txDropped uint64
}

// wholeDisk matches block devices worth showing; partitions, loop and ram
// devices only repeat or hide what the whole disk does.
var wholeDisk = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|mmcblk[0-9]+|nvme[0-9]+n[0-9]+|md[0-9]+)$`)

func (m *Monitor) path(p string) string { return filepath.Join(m.Root, p) }

func (m *Monitor) read() (raw, error) {
	var r raw
	var err error
	if r.cpu, err = m.readCPU(); err != nil {
		return r, err
	}
	if r.mem, err = m.readMem(); err != nil {
		return r, err
	}
	r.swapIn, r.swapOut = m.readVMStat()
	r.psi = m.readPSI()
	r.disks, r.diskList = m.readDisks()
	r.nets, r.netList = m.readNets()
	return r, nil
}

func (m *Monitor) readCPU() (cpuTicks, error) {
	var c cpuTicks
	f, err := os.Open(m.path("proc/stat"))
	if err != nil {
		return c, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return c, errors.New("empty /proc/stat")
	}
	fs := strings.Fields(s.Text())
	if len(fs) < 5 || fs[0] != "cpu" {
		return c, errors.New("unexpected /proc/stat format")
	}
	v := make([]uint64, 8)
	for i := range v {
		if i+1 < len(fs) {
			v[i], _ = strconv.ParseUint(fs[i+1], 10, 64)
		}
	}
	return cpuTicks{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]}, nil
}

func (m *Monitor) readMem() (memInfo, error) {
	var mi memInfo
	f, err := os.Open(m.path("proc/meminfo"))
	if err != nil {
		return mi, err
	}
	defer f.Close()
	fields := map[string]*uint64{
		"MemTotal": &mi.total, "MemAvailable": &mi.available, "MemFree": &mi.free, "Buffers": &mi.buffers,
		"Cached": &mi.cached, "SReclaimable": &mi.sreclaimable, "SwapTotal": &mi.swapTotal, "SwapFree": &mi.swapFree,
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, rest, ok := strings.Cut(s.Text(), ":")
		dst, want := fields[k]
		if !ok || !want {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fs[0], 10, 64)
		if err == nil {
			*dst = kb * 1024
		}
	}
	if mi.total == 0 {
		return mi, errors.New("MemTotal missing in /proc/meminfo")
	}
	return mi, nil
}

func (m *Monitor) readVMStat() (in, out uint64) {
	f, err := os.Open(m.path("proc/vmstat"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, _ := strings.Cut(s.Text(), " ")
		switch k {
		case "pswpin":
			in, _ = strconv.ParseUint(v, 10, 64)
		case "pswpout":
			out, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	return in, out
}

// readPSI reads avg10 of /proc/pressure; nil when the kernel lacks it (the
// NSA310 kernel 6.12 today is built without CONFIG_PSI).
func (m *Monitor) readPSI() *PSI {
	read := func(name string) (some, full float64, ok bool) {
		b, err := os.ReadFile(m.path("proc/pressure/" + name))
		if err != nil {
			return 0, 0, false
		}
		for _, line := range strings.Split(string(b), "\n") {
			fs := strings.Fields(line)
			if len(fs) < 2 {
				continue
			}
			for _, kv := range fs[1:] {
				if v, found := strings.CutPrefix(kv, "avg10="); found {
					x, _ := strconv.ParseFloat(v, 64)
					if fs[0] == "some" {
						some = x
					} else if fs[0] == "full" {
						full = x
					}
				}
			}
		}
		return some, full, true
	}
	cpu, _, ok1 := read("cpu")
	memSome, memFull, ok2 := read("memory")
	ioSome, ioFull, ok3 := read("io")
	if !ok1 && !ok2 && !ok3 {
		return nil
	}
	return &PSI{CPUSome: cpu, MemorySome: memSome, MemoryFull: memFull, IOSome: ioSome, IOFull: ioFull}
}

func (m *Monitor) readDisks() (map[string]diskCounters, []string) {
	out := map[string]diskCounters{}
	f, err := os.Open(m.path("proc/diskstats"))
	if err != nil {
		return out, nil
	}
	defer f.Close()
	var names []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		if len(fs) < 14 || !wholeDisk.MatchString(fs[2]) {
			continue
		}
		u := func(i int) uint64 { v, _ := strconv.ParseUint(fs[i], 10, 64); return v }
		out[fs[2]] = diskCounters{
			reads: u(3), readSectors: u(5), readMs: u(6),
			writes: u(7), writeSectors: u(9), writeMs: u(10), ioMs: u(12),
		}
		names = append(names, fs[2])
	}
	sort.Strings(names)
	return out, names
}

func (m *Monitor) readNets() (map[string]netCounters, []string) {
	out := map[string]netCounters{}
	base := m.path("sys/class/net")
	ents, err := os.ReadDir(base)
	if err != nil {
		return out, nil
	}
	var names []string
	for _, e := range ents {
		name := e.Name()
		if name == "lo" {
			continue
		}
		st := filepath.Join(base, name, "statistics")
		u := func(f string) uint64 {
			b, err := os.ReadFile(filepath.Join(st, f))
			if err != nil {
				return 0
			}
			v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
			return v
		}
		if _, err := os.Stat(st); err != nil {
			continue
		}
		out[name] = netCounters{
			rxBytes: u("rx_bytes"), txBytes: u("tx_bytes"), rxErrors: u("rx_errors"), txErrors: u("tx_errors"),
			rxDropped: u("rx_dropped"), txDropped: u("tx_dropped"),
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return out, names
}
