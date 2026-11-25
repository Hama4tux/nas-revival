// Package diskactivity measures, on demand, which processes and files keep a
// disk busy (W1.9, DIAG-01, PWR-06). It reads only /proc and /sys and stats
// files; it never opens a block device, so it does not wake a sleeping disk.
//
// The NSA310 kernel is built without CONFIG_TASK_IO_ACCOUNTING, so bytes per
// process are often unavailable. Attribution then rests on files held open for
// writing and on files whose change time falls into the measurement window.
package diskactivity

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sectorBytes   = 512
	maxFDsPerProc = 1024
	maxOpenFiles  = 500
)

// Reader reads kernel interfaces below Root ("/" in production).
type Reader struct {
	Root string
}

func (r Reader) path(p string) string { return filepath.Join(r.Root, p) }

// DevStat holds the counters of one block device from /proc/diskstats.
type DevStat struct {
	ReadOps, ReadSectors, WriteOps, WriteSectors uint64
}

// Mount is a block-device file system from /proc/self/mountinfo.
type Mount struct {
	Dev    uint64 // st_dev of files on it
	Device string // kernel name, e.g. sda1
	Point  string
	FSType string
}

// OpenFile is a regular file a process holds open for writing.
type OpenFile struct {
	Path  string
	Dev   uint64
	Ino   uint64
	Size  int64
	MTime time.Time
}

// Proc is one process with what it has open for writing.
type Proc struct {
	PID   int
	Start uint64 // start time in clock ticks; detects PID reuse
	Comm  string
	Unit  string // systemd unit from the cgroup path, "" for none
	IO    *IOCounters
	Files []OpenFile
}

// IOCounters come from /proc/<pid>/io when the kernel has task I/O accounting.
type IOCounters struct {
	ReadBytes, WriteBytes uint64
}

// Sample is one snapshot of all counters.
type Sample struct {
	Time    time.Time
	Devices map[string]DevStat
	SwapIn  uint64 // pages, /proc/vmstat pswpin
	SwapOut uint64
	Journal map[string]uint64 // ext4 jbd2 transactions per device
	Procs   map[int]Proc
}

// blockName accepts whole disks and partitions that NAS storage can be on.
var blockName = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|md[0-9]+|mmcblk[0-9]+|nvme[0-9]+n[0-9]+)(p?[0-9]+)?$`)

// Take reads one snapshot. Missing optional interfaces leave fields empty.
func (r Reader) Take(mounts []Mount) (Sample, error) {
	s := Sample{Time: time.Now(), Journal: map[string]uint64{}, Procs: map[int]Proc{}}
	var err error
	if s.Devices, err = r.diskstats(); err != nil {
		return s, err
	}
	s.SwapIn, s.SwapOut = r.swapPages()
	r.journal(s.Journal)
	devs := map[uint64]bool{}
	for _, m := range mounts {
		devs[m.Dev] = true
	}
	r.procs(s.Procs, devs)
	return s, nil
}

func (r Reader) diskstats() (map[string]DevStat, error) {
	b, err := os.ReadFile(r.path("proc/diskstats"))
	if err != nil {
		return nil, err
	}
	out := map[string]DevStat{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || !blockName.MatchString(f[2]) {
			continue
		}
		n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		out[f[2]] = DevStat{ReadOps: n(3), ReadSectors: n(5), WriteOps: n(7), WriteSectors: n(9)}
	}
	return out, nil
}

func (r Reader) swapPages() (in, out uint64) {
	b, err := os.ReadFile(r.path("proc/vmstat"))
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		switch k {
		case "pswpin":
			in = n
		case "pswpout":
			out = n
		}
	}
	return in, out
}

// journal reads "N transactions" from /proc/fs/jbd2/<dev>-<ino>/info.
func (r Reader) journal(out map[string]uint64) {
	dirs, _ := filepath.Glob(r.path("proc/fs/jbd2/*/info"))
	for _, p := range dirs {
		dev, _, _ := strings.Cut(filepath.Base(filepath.Dir(p)), "-")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f := strings.Fields(string(b))
		if len(f) >= 2 && f[1] == "transactions" {
			if n, err := strconv.ParseUint(f[0], 10, 64); err == nil {
				out[dev] = n
			}
		}
	}
}

// Mounts lists block-device file systems once per device (bind mounts and
// sub-volumes are skipped in favour of the mount of the file-system root).
func (r Reader) Mounts() ([]Mount, error) {
	b, err := os.ReadFile(r.path("proc/self/mountinfo"))
	if err != nil {
		return nil, err
	}
	seen := map[uint64]int{}
	var out []Mount
	for _, line := range strings.Split(string(b), "\n") {
		pre, post, ok := strings.Cut(line, " - ")
		f, g := strings.Fields(pre), strings.Fields(post)
		if !ok || len(f) < 5 || len(g) < 2 || !strings.HasPrefix(g[1], "/dev/") {
			continue
		}
		name := strings.TrimPrefix(g[1], "/dev/")
		if name == "root" {
			name = r.rootDevice(f[2])
		}
		if !blockName.MatchString(name) {
			continue
		}
		maj, min, ok := strings.Cut(f[2], ":")
		if !ok {
			continue
		}
		ma, _ := strconv.ParseUint(maj, 10, 32)
		mi, _ := strconv.ParseUint(min, 10, 32)
		m := Mount{Dev: mkdev(ma, mi), Device: name, Point: unescape(f[4]), FSType: g[0]}
		if i, dup := seen[m.Dev]; dup {
			if f[3] == "/" && len(m.Point) < len(out[i].Point) {
				out[i] = m
			}
			continue
		}
		seen[m.Dev] = len(out)
		out = append(out, m)
	}
	return out, nil
}

// rootDevice resolves /dev/root through /sys/dev/block/<maj:min>.
func (r Reader) rootDevice(majmin string) string {
	p, err := filepath.EvalSymlinks(r.path("sys/dev/block/" + majmin))
	if err != nil {
		return "root"
	}
	return filepath.Base(p)
}

// mkdev matches glibc's makedev, i.e. syscall.Stat_t.Dev on Linux.
func mkdev(major, minor uint64) uint64 {
	return (minor & 0xff) | ((major & 0xfff) << 8) | ((minor &^ 0xff) << 12) | ((major &^ 0xfff) << 32)
}

// unescape decodes the octal escapes mountinfo uses for spaces and the like.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (r Reader) procs(out map[int]Proc, devs map[uint64]bool) {
	entries, err := os.ReadDir(r.path("proc"))
	if err != nil {
		return
	}
	files := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		base := r.path("proc/" + e.Name())
		p := Proc{PID: pid, Comm: readTrim(base + "/comm"), Unit: unitOf(readTrim(base + "/cgroup"))}
		if p.Comm == "" {
			continue // exited meanwhile
		}
		p.Start = startTime(base + "/stat")
		p.IO = readIO(base + "/io")
		if files < maxOpenFiles {
			p.Files = openForWrite(base, devs, maxOpenFiles-files)
			files += len(p.Files)
		}
		out[pid] = p
	}
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// unitOf takes the systemd unit (last *.service or *.scope element) from a
// cgroup v2 line "0::/system.slice/foo.service".
func unitOf(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "0::"), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.HasSuffix(parts[i], ".service") || strings.HasSuffix(parts[i], ".scope") {
				return parts[i]
			}
		}
	}
	return ""
}

// startTime is field 22 of /proc/<pid>/stat (after the parenthesised comm).
func startTime(p string) uint64 {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 20 {
		return 0
	}
	v, _ := strconv.ParseUint(f[19], 10, 64)
	return v
}

func readIO(p string) *IOCounters {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var c IOCounters
	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ": ")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(v, 10, 64)
		switch k {
		case "read_bytes":
			c.ReadBytes, found = n, true
		case "write_bytes":
			c.WriteBytes, found = n, true
		}
	}
	if !found {
		return nil
	}
	return &c
}

// openForWrite lists regular files on the watched devices that the process
// holds open with O_WRONLY or O_RDWR.
func openForWrite(base string, devs map[uint64]bool, limit int) []OpenFile {
	fds, err := os.ReadDir(base + "/fd")
	if err != nil {
		return nil
	}
	var out []OpenFile
	for i, fd := range fds {
		if i >= maxFDsPerProc || len(out) >= limit {
			break
		}
		if !writeMode(base + "/fdinfo/" + fd.Name()) {
			continue
		}
		target, err := os.Readlink(base + "/fd/" + fd.Name())
		if err != nil || !strings.HasPrefix(target, "/") {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat(base+"/fd/"+fd.Name(), &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG || !devs[uint64(st.Dev)] {
			continue
		}
		out = append(out, OpenFile{Path: target, Dev: uint64(st.Dev), Ino: uint64(st.Ino), Size: st.Size,
			MTime: time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec))})
	}
	return out
}

func writeMode(fdinfo string) bool {
	b, err := os.ReadFile(fdinfo)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "flags:"); ok {
			fl, err := strconv.ParseUint(strings.TrimSpace(v), 8, 64)
			return err == nil && fl&3 != 0
		}
	}
	return false
}

// SwapDevices lists kernel names of active swap partitions (/proc/swaps).
func (r Reader) SwapDevices() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(r.path("proc/swaps"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], "/dev/") {
			out[strings.TrimPrefix(f[0], "/dev/")] = true
		}
	}
	return out
}

// Parent returns the whole disk of a partition ("" for a whole disk).
func (r Reader) Parent(name string) string {
	if _, err := os.Stat(r.path("sys/class/block/" + name + "/partition")); err != nil {
		return ""
	}
	p, err := filepath.EvalSymlinks(r.path("sys/class/block/" + name))
	if err != nil {
		return ""
	}
	return filepath.Base(filepath.Dir(p))
}
