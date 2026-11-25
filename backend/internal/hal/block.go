package hal

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// BlockPartition is one partition of a BlockDisk.
type BlockPartition struct {
	Name      string
	Number    int
	DevNum    string // "major:minor"
	SizeBytes uint64
	Udev      map[string]string // E: properties from the udev database
}

// BlockDisk is a whole disk with its partitions and udev identity. It is read
// from sysfs and /run/udev/data only, so listing never wakes a sleeping disk.
type BlockDisk struct {
	Name       string
	DevNum     string
	SizeBytes  uint64
	Rotational bool
	Removable  bool
	ReadOnly   bool
	Model      string // sysfs device/model, trimmed
	Vendor     string // sysfs device/vendor, trimmed
	Firmware   string // sysfs device/rev, trimmed
	Udev       map[string]string
	ByID       []string // names under /dev/disk/by-id/ from udev symlinks
	Partitions []BlockPartition
}

// BlockDisks lists whole disks (sd*, mmcblk*, nvme*) with non-zero size.
func (r Reader) BlockDisks() ([]BlockDisk, error) {
	entries, err := os.ReadDir(r.path("sys/block"))
	if err != nil {
		return nil, err
	}
	out := []BlockDisk{}
	for _, e := range entries {
		name := e.Name()
		if !hasAnyPrefix(name, diskPrefixes) {
			continue
		}
		base := filepath.Join("sys/block", name)
		sectors, err := r.readInt(filepath.Join(base, "size"))
		if err != nil || sectors == 0 {
			continue
		}
		d := BlockDisk{Name: name, SizeBytes: uint64(sectors) * 512}
		d.DevNum, _ = r.readTrim(filepath.Join(base, "dev"))
		d.Rotational = r.flag(filepath.Join(base, "queue/rotational"))
		d.Removable = r.flag(filepath.Join(base, "removable"))
		d.ReadOnly = r.flag(filepath.Join(base, "ro"))
		d.Model, _ = r.readTrim(filepath.Join(base, "device/model"))
		d.Vendor, _ = r.readTrim(filepath.Join(base, "device/vendor"))
		d.Firmware, _ = r.readTrim(filepath.Join(base, "device/rev"))
		d.Udev, d.ByID = r.udevData(d.DevNum)
		d.Partitions = r.partitions(base, name)
		out = append(out, d)
	}
	return out, nil
}

func (r Reader) flag(p string) bool {
	v, _ := r.readTrim(p)
	return v == "1"
}

func (r Reader) partitions(base, disk string) []BlockPartition {
	entries, err := os.ReadDir(r.path(base))
	if err != nil {
		return []BlockPartition{}
	}
	out := []BlockPartition{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, disk) || name == disk {
			continue
		}
		pb := filepath.Join(base, name)
		num, err := r.readInt(filepath.Join(pb, "partition"))
		if err != nil {
			continue // not a partition directory
		}
		sectors, _ := r.readInt(filepath.Join(pb, "size"))
		p := BlockPartition{Name: name, Number: int(num), SizeBytes: uint64(sectors) * 512}
		p.DevNum, _ = r.readTrim(filepath.Join(pb, "dev"))
		p.Udev, _ = r.udevData(p.DevNum)
		out = append(out, p)
	}
	return out
}

// udevData parses /run/udev/data/b<maj:min>: "E:KEY=value" properties and
// "S:disk/by-id/<name>" symlinks. Missing data yields an empty map.
func (r Reader) udevData(devnum string) (map[string]string, []string) {
	props := map[string]string{}
	byID := []string{}
	if !validDevNum(devnum) {
		return props, byID
	}
	f, err := os.Open(r.path(filepath.Join("run/udev/data", "b"+devnum)))
	if err != nil {
		return props, byID
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if kv, ok := strings.CutPrefix(line, "E:"); ok {
			if k, v, ok := strings.Cut(kv, "="); ok {
				props[k] = v
			}
		} else if id, ok := strings.CutPrefix(line, "S:disk/by-id/"); ok && id != "" && !strings.Contains(id, "/") {
			byID = append(byID, id)
		}
	}
	return props, byID
}

func validDevNum(s string) bool {
	maj, min, ok := strings.Cut(s, ":")
	if !ok {
		return false
	}
	_, e1 := strconv.ParseUint(maj, 10, 32)
	_, e2 := strconv.ParseUint(min, 10, 32)
	return e1 == nil && e2 == nil
}

// MountEntry is one line of /proc/self/mountinfo for a block device.
type MountEntry struct {
	DevNum   string
	Root     string // path inside the filesystem; not "/" for bind mounts
	Point    string
	FSType   string
	ReadOnly bool // filesystem (superblock) is read-only
}

// MountInfo lists mounts backed by a block device (major != 0), in mount order.
func (r Reader) MountInfo() ([]MountEntry, error) {
	f, err := os.Open(r.path("proc/self/mountinfo"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []MountEntry{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		sep := -1
		for i := 6; i < len(fs); i++ {
			if fs[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 >= len(fs) || !validDevNum(fs[2]) || strings.HasPrefix(fs[2], "0:") {
			continue
		}
		// Superblock options, not per-mount ones: the service runs with
		// ProtectSystem=strict, which makes every mount read-only in its own
		// namespace. A read-only bind of a writable filesystem shows as writable.
		ro := false
		if sep+3 < len(fs) {
			for _, o := range strings.Split(fs[sep+3], ",") {
				if o == "ro" {
					ro = true
				}
			}
		}
		out = append(out, MountEntry{
			DevNum: fs[2], Root: unescapeMount(fs[3]), Point: unescapeMount(fs[4]),
			FSType: fs[sep+1], ReadOnly: ro,
		})
	}
	return out, s.Err()
}

// SwapPartitions returns kernel names of active swap partitions (swap files are skipped).
func (r Reader) SwapPartitions() ([]string, error) {
	f, err := os.Open(r.path("proc/swaps"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		if len(fs) < 2 || fs[1] != "partition" {
			continue
		}
		if name, ok := strings.CutPrefix(unescapeMount(fs[0]), "/dev/"); ok && !strings.Contains(name, "/") {
			out = append(out, name)
		}
	}
	return out, s.Err()
}
