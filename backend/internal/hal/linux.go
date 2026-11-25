// Package hal reads hardware and kernel state from standard Linux interfaces
// (procfs, sysfs, hwmon). It never writes and never accepts paths from callers.
package hal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Reader reads from a file-system root. Production uses "/"; tests point it
// at a fixture tree so behaviour can be verified without the device.
type Reader struct {
	Root string
}

func (r Reader) path(p string) string { return filepath.Join(r.Root, p) }

func (r Reader) readTrim(p string) (string, error) {
	b, err := os.ReadFile(r.path(p))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (r Reader) readInt(p string) (int64, error) {
	s, err := r.readTrim(p)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// HostInfo is static identification of the running system.
type HostInfo struct {
	Hostname      string
	OS            string
	Kernel        string
	BoardModel    *string
	UptimeSeconds int64
}

func (r Reader) Host() (HostInfo, error) {
	var h HostInfo
	var err error
	if h.Hostname, err = r.readTrim("proc/sys/kernel/hostname"); err != nil {
		return h, err
	}
	if h.Kernel, err = r.readTrim("proc/sys/kernel/osrelease"); err != nil {
		return h, err
	}
	h.OS = r.osPrettyName()
	if m, err := r.readTrim("proc/device-tree/model"); err == nil {
		m = strings.TrimRight(m, "\x00")
		h.BoardModel = &m
	}
	up, err := r.readTrim("proc/uptime")
	if err != nil {
		return h, err
	}
	fields := strings.Fields(up)
	if len(fields) == 0 {
		return h, errors.New("empty /proc/uptime")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return h, fmt.Errorf("parse uptime: %w", err)
	}
	h.UptimeSeconds = int64(secs)
	return h, nil
}

func (r Reader) osPrettyName() string {
	for _, p := range []string{"etc/os-release", "usr/lib/os-release"} {
		f, err := os.Open(r.path(p))
		if err != nil {
			continue
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if v, ok := strings.CutPrefix(s.Text(), "PRETTY_NAME="); ok {
				return strings.Trim(v, `"'`)
			}
		}
	}
	return "Linux"
}

// CPUInfo describes the processor and load.
type CPUInfo struct {
	Model  string
	Cores  int
	Load1  float64
	Load5  float64
	Load15 float64
}

func (r Reader) CPU() (CPUInfo, error) {
	var c CPUInfo
	f, err := os.Open(r.path("proc/cpuinfo"))
	if err != nil {
		return c, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		key, val, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "processor":
			c.Cores++
		case "model name", "Processor":
			if c.Model == "" {
				c.Model = val
			}
		}
	}
	if c.Cores == 0 {
		c.Cores = 1
	}
	la, err := r.readTrim("proc/loadavg")
	if err != nil {
		return c, err
	}
	fs := strings.Fields(la)
	if len(fs) < 3 {
		return c, errors.New("short /proc/loadavg")
	}
	for i, dst := range []*float64{&c.Load1, &c.Load5, &c.Load15} {
		if *dst, err = strconv.ParseFloat(fs[i], 64); err != nil {
			return c, fmt.Errorf("parse loadavg: %w", err)
		}
	}
	return c, nil
}

// CPUTimes is the aggregate line of /proc/stat in clock ticks.
type CPUTimes struct {
	Idle  uint64 // idle + iowait
	Total uint64
}

func (r Reader) CPUTimes() (CPUTimes, error) {
	var t CPUTimes
	f, err := os.Open(r.path("proc/stat"))
	if err != nil {
		return t, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return t, errors.New("empty /proc/stat")
	}
	fs := strings.Fields(s.Text())
	if len(fs) < 5 || fs[0] != "cpu" {
		return t, errors.New("unexpected /proc/stat format")
	}
	// Fields after "cpu": user nice system idle iowait irq softirq steal guest guest_nice.
	// guest times are already included in user/nice, so stop at steal.
	for i, v := range fs[1:min(len(fs), 9)] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return t, fmt.Errorf("parse /proc/stat: %w", err)
		}
		t.Total += n
		if i == 3 || i == 4 {
			t.Idle += n
		}
	}
	return t, nil
}

// MemInfo holds values from /proc/meminfo in bytes.
type MemInfo struct {
	TotalBytes     uint64
	AvailableBytes uint64
	SwapTotalBytes uint64
	SwapFreeBytes  uint64
}

func (r Reader) Memory() (MemInfo, error) {
	var m MemInfo
	f, err := os.Open(r.path("proc/meminfo"))
	if err != nil {
		return m, err
	}
	defer f.Close()
	want := map[string]*uint64{
		"MemTotal": &m.TotalBytes, "MemAvailable": &m.AvailableBytes,
		"SwapTotal": &m.SwapTotalBytes, "SwapFree": &m.SwapFreeBytes,
	}
	found := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		key, rest, ok := strings.Cut(s.Text(), ":")
		dst, wanted := want[key]
		if !ok || !wanted {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fs[0], 10, 64)
		if err != nil {
			return m, fmt.Errorf("parse meminfo %s: %w", key, err)
		}
		*dst = kb * 1024
		found++
	}
	if m.TotalBytes == 0 || found < len(want) {
		return m, errors.New("incomplete /proc/meminfo")
	}
	return m, nil
}

// Sensor status values shared with the API.
const (
	SensorOK          = "ok"
	SensorUnknown     = "unknown"
	SensorUnsupported = "unsupported"
	SensorFault       = "fault"
)

type Temperature struct {
	ID           string
	Label        string
	MilliCelsius int64
}

type Fan struct {
	ID    string
	Label string
	RPM   int64
}

type Sensors struct {
	Status       string
	Temperatures []Temperature
	Fans         []Fan
}

// Sensors enumerates hwmon devices. IDs are "<driver name>/<channel>", so they
// stay stable when the kernel renumbers hwmonN directories.
func (r Reader) Sensors() Sensors {
	out := Sensors{Status: SensorUnsupported, Temperatures: []Temperature{}, Fans: []Fan{}}
	devs, err := filepath.Glob(r.path("sys/class/hwmon/hwmon*"))
	if err != nil || len(devs) == 0 {
		return out
	}
	faults := 0
	for _, dev := range devs {
		rel, _ := filepath.Rel(r.Root, dev)
		name, err := r.readTrim(filepath.Join(rel, "name"))
		if err != nil {
			faults++
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(dev, "*_input"))
		sort.Strings(inputs)
		for _, in := range inputs {
			channel := strings.TrimSuffix(filepath.Base(in), "_input")
			isTemp := strings.HasPrefix(channel, "temp")
			isFan := strings.HasPrefix(channel, "fan")
			if !isTemp && !isFan {
				continue
			}
			val, err := r.readInt(filepath.Join(rel, channel+"_input"))
			if err != nil {
				faults++
				continue
			}
			label, err := r.readTrim(filepath.Join(rel, channel+"_label"))
			if err != nil || label == "" {
				label = channel
			}
			id := name + "/" + channel
			if isTemp {
				out.Temperatures = append(out.Temperatures, Temperature{ID: id, Label: label, MilliCelsius: val})
			} else {
				out.Fans = append(out.Fans, Fan{ID: id, Label: label, RPM: val})
			}
		}
	}
	switch {
	case len(out.Temperatures)+len(out.Fans) == 0 && faults > 0:
		out.Status = SensorFault
	case len(out.Temperatures)+len(out.Fans) == 0:
		out.Status = SensorUnknown
	case faults > 0:
		out.Status = SensorFault
	default:
		out.Status = SensorOK
	}
	return out
}

// NetInterface is one non-loopback network interface from sysfs.
type NetInterface struct {
	Name      string
	Up        bool
	SpeedMbps *int64
	RxBytes   uint64
	TxBytes   uint64
}

func (r Reader) NetInterfaces() ([]NetInterface, error) {
	entries, err := os.ReadDir(r.path("sys/class/net"))
	if err != nil {
		return nil, err
	}
	out := []NetInterface{}
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join("sys/class/net", name)
		state, _ := r.readTrim(filepath.Join(base, "operstate"))
		ni := NetInterface{Name: name, Up: state == "up"}
		// speed reads fail with EINVAL while the link is down; -1 means unknown.
		if ni.Up {
			if sp, err := r.readInt(filepath.Join(base, "speed")); err == nil && sp > 0 {
				ni.SpeedMbps = &sp
			}
		}
		if v, err := r.readInt(filepath.Join(base, "statistics/rx_bytes")); err == nil {
			ni.RxBytes = uint64(v)
		}
		if v, err := r.readInt(filepath.Join(base, "statistics/tx_bytes")); err == nil {
			ni.TxBytes = uint64(v)
		}
		out = append(out, ni)
	}
	return out, nil
}

// Disk is a whole block device (not a partition).
type Disk struct {
	Name       string
	Model      *string
	SizeBytes  uint64
	Rotational bool
	Removable  bool
}

var diskPrefixes = []string{"sd", "mmcblk", "nvme"}

func (r Reader) Disks() ([]Disk, error) {
	entries, err := os.ReadDir(r.path("sys/block"))
	if err != nil {
		return nil, err
	}
	out := []Disk{}
	for _, e := range entries {
		name := e.Name()
		if !hasAnyPrefix(name, diskPrefixes) {
			continue
		}
		base := filepath.Join("sys/block", name)
		sectors, err := r.readInt(filepath.Join(base, "size"))
		if err != nil || sectors == 0 {
			continue // empty card reader slots report size 0
		}
		d := Disk{Name: name, SizeBytes: uint64(sectors) * 512}
		if m, err := r.readTrim(filepath.Join(base, "device/model")); err == nil && m != "" {
			d.Model = &m
		}
		rot, _ := r.readTrim(filepath.Join(base, "queue/rotational"))
		d.Rotational = rot == "1"
		rem, _ := r.readTrim(filepath.Join(base, "removable"))
		d.Removable = rem == "1"
		out = append(out, d)
	}
	return out, nil
}

// Mount is a block-device mount from /proc/self/mounts.
type Mount struct {
	Device string
	Point  string
	FSType string
}

func (r Reader) Mounts() ([]Mount, error) {
	f, err := os.Open(r.path("proc/self/mounts"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	out := []Mount{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		// Loop devices are images (snaps, ISOs), not storage the owner manages.
		if len(fs) < 3 || !strings.HasPrefix(fs[0], "/dev/") || strings.HasPrefix(fs[0], "/dev/loop") {
			continue
		}
		point := unescapeMount(fs[1])
		if seen[point] {
			continue
		}
		seen[point] = true
		out = append(out, Mount{Device: unescapeMount(fs[0]), Point: point, FSType: fs[2]})
	}
	return out, s.Err()
}

// unescapeMount decodes the octal escapes (\040 etc.) used in /proc/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
