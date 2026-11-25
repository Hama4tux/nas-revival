package diskactivity

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const (
	MinSeconds     = 10
	MaxSeconds     = 45
	maxScanEntries = 200_000
	maxScanTime    = 5 * time.Second
	maxFiles       = 100
	maxProcs       = 50
	// The kernel stamps times from a coarse clock that may lag a few ms.
	clockSlack = 20 * time.Millisecond
)

// Result is the outcome of one measurement (op disk.activity).
type Result struct {
	StartedAt time.Time `json:"startedAt"`
	Seconds   int       `json:"seconds"`
	// ProcessIO is true when the kernel reports bytes per process.
	ProcessIO bool              `json:"processIo"`
	Devices   []DeviceActivity  `json:"devices"`
	SwapIn    uint64            `json:"swapInPages"`
	SwapOut   uint64            `json:"swapOutPages"`
	Processes []ProcessActivity `json:"processes"`
	Files     []FileActivity    `json:"files"`
	// FilesTruncated: more changed files than listed.
	FilesTruncated bool `json:"filesTruncated"`
	// NotScanned are mount points not searched for changed files: the disk
	// was idle (searching could wake it) or the search limit was reached.
	NotScanned []string `json:"notScanned"`
}

type DeviceActivity struct {
	Name   string `json:"name"`
	Parent string `json:"parent"` // whole disk of a partition, "" for a disk
	Mount  string `json:"mount"`
	FSType string `json:"fsType"`
	Swap   bool   `json:"swap"`
	DevStatDelta
	// JournalCommits are ext4 journal transactions in the window, nil without a journal.
	JournalCommits *uint64 `json:"journalCommits"`
}

type DevStatDelta struct {
	ReadOps    uint64 `json:"readOps"`
	ReadBytes  uint64 `json:"readBytes"`
	WriteOps   uint64 `json:"writeOps"`
	WriteBytes uint64 `json:"writeBytes"`
}

type ProcessActivity struct {
	PID  int    `json:"pid"`
	Comm string `json:"comm"`
	Unit string `json:"unit"`
	// ReadBytes and WriteBytes are nil without task I/O accounting.
	ReadBytes  *uint64  `json:"readBytes"`
	WriteBytes *uint64  `json:"writeBytes"`
	Files      []string `json:"files"` // changed files it holds open for writing
}

type FileActivity struct {
	Path      string   `json:"path"`
	Device    string   `json:"device"`
	SizeDelta *int64   `json:"sizeDelta"` // nil when the size before is unknown
	Processes []string `json:"processes"` // "comm (pid)" holding it open for writing
}

// Measure samples, waits seconds and samples again.
func (r Reader) Measure(ctx context.Context, seconds int) (Result, error) {
	mounts, err := r.Mounts()
	if err != nil {
		return Result{}, err
	}
	a, err := r.Take(mounts)
	if err != nil {
		return Result{}, err
	}
	t := time.NewTimer(time.Duration(seconds) * time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-t.C:
	}
	b, err := r.Take(mounts)
	if err != nil {
		return Result{}, err
	}
	res := r.compare(a, b, mounts, seconds)
	changed, truncated, skipped := scanChanged(ctx, mounts, idleDevices(res.Devices), a.Time.Add(-clockSlack))
	attribute(&res, a, b, mounts, changed)
	res.FilesTruncated = res.FilesTruncated || truncated
	res.NotScanned = skipped
	return res, nil
}

func sub(b, a uint64) uint64 {
	if b < a {
		return 0 // counter reset (device re-added)
	}
	return b - a
}

func (r Reader) compare(a, b Sample, mounts []Mount, seconds int) Result {
	res := Result{StartedAt: a.Time.UTC(), Seconds: seconds, SwapIn: sub(b.SwapIn, a.SwapIn),
		SwapOut: sub(b.SwapOut, a.SwapOut), Devices: []DeviceActivity{}, Processes: []ProcessActivity{},
		Files: []FileActivity{}, NotScanned: []string{}}
	swaps := r.SwapDevices()
	byDev := map[string]Mount{}
	for _, m := range mounts {
		byDev[m.Device] = m
	}
	for name, sb := range b.Devices {
		sa, ok := a.Devices[name]
		if !ok {
			continue
		}
		d := DeviceActivity{Name: name, Parent: r.Parent(name), Swap: swaps[name], DevStatDelta: DevStatDelta{
			ReadOps: sub(sb.ReadOps, sa.ReadOps), ReadBytes: sub(sb.ReadSectors, sa.ReadSectors) * sectorBytes,
			WriteOps: sub(sb.WriteOps, sa.WriteOps), WriteBytes: sub(sb.WriteSectors, sa.WriteSectors) * sectorBytes}}
		if m, ok := byDev[name]; ok {
			d.Mount, d.FSType = m.Point, m.FSType
		}
		if ja, ok := a.Journal[name]; ok {
			if jb, ok := b.Journal[name]; ok {
				n := sub(jb, ja)
				d.JournalCommits = &n
			}
		}
		res.Devices = append(res.Devices, d)
	}
	sort.Slice(res.Devices, func(i, j int) bool { return res.Devices[i].Name < res.Devices[j].Name })
	for _, p := range b.Procs {
		if p.IO != nil {
			res.ProcessIO = true
			break
		}
	}
	return res
}

// idleDevices: a disk with no completed I/O in the window is not searched,
// so a sleeping disk is never woken by the search.
func idleDevices(devs []DeviceActivity) map[string]bool {
	busy := map[string]bool{}
	for _, d := range devs {
		disk := d.Name
		if d.Parent != "" {
			disk = d.Parent
		}
		if d.ReadOps+d.WriteOps > 0 {
			busy[disk] = true
		}
	}
	idle := map[string]bool{}
	for _, d := range devs {
		disk := d.Name
		if d.Parent != "" {
			disk = d.Parent
		}
		if !busy[disk] {
			idle[d.Name] = true
		}
	}
	return idle
}

type fileKey struct{ dev, ino uint64 }

type changedFile struct {
	path string
	dev  string
	size int64
}

// scanChanged finds regular files whose change time is after since.
func scanChanged(ctx context.Context, mounts []Mount, idle map[string]bool, since time.Time) (map[fileKey]changedFile, bool, []string) {
	out := map[fileKey]changedFile{}
	skipped := []string{}
	truncated := false
	deadline := time.Now().Add(maxScanTime)
	entries := 0
	for _, m := range mounts {
		if idle[m.Device] {
			skipped = append(skipped, m.Point)
			continue
		}
		limited := false
		filepath.WalkDir(m.Point, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			entries++
			if entries > maxScanEntries || time.Now().After(deadline) || ctx.Err() != nil {
				limited = true
				return filepath.SkipAll
			}
			if !d.Type().IsRegular() && !d.IsDir() {
				return nil
			}
			var st syscall.Stat_t
			if syscall.Lstat(p, &st) != nil {
				return nil
			}
			if uint64(st.Dev) != m.Dev {
				if d.IsDir() {
					return filepath.SkipDir // another file system mounted here
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec)).Before(since) {
				if len(out) >= maxFiles {
					truncated = true
					return nil
				}
				out[fileKey{uint64(st.Dev), uint64(st.Ino)}] = changedFile{path: p, dev: m.Device, size: st.Size}
			}
			return nil
		})
		if limited {
			skipped = append(skipped, m.Point)
		}
	}
	return out, truncated, skipped
}

func label(p Proc) string { return p.Comm + " (" + strconv.Itoa(p.PID) + ")" }

// attribute joins changed files with the processes holding them open for
// writing, and adds open files whose size or modification time changed.
func attribute(res *Result, a, b Sample, mounts []Mount, changed map[fileKey]changedFile) {
	devName := map[uint64]string{}
	for _, m := range mounts {
		devName[m.Dev] = m.Device
	}
	before := map[fileKey]OpenFile{}
	for _, p := range a.Procs {
		for _, f := range p.Files {
			before[fileKey{f.Dev, f.Ino}] = f
		}
	}
	// Open files that changed are found even where the search did not run.
	for _, p := range b.Procs {
		for _, f := range p.Files {
			k := fileKey{f.Dev, f.Ino}
			if o, ok := before[k]; ok && (o.Size != f.Size || !o.MTime.Equal(f.MTime)) {
				if _, ok := changed[k]; !ok {
					changed[k] = changedFile{path: f.Path, dev: devName[f.Dev], size: f.Size}
				}
			}
		}
	}
	holders := map[fileKey][]Proc{}
	for _, snap := range []Sample{a, b} {
		for _, p := range snap.Procs {
			for _, f := range p.Files {
				k := fileKey{f.Dev, f.Ino}
				if _, ok := changed[k]; ok && !hasProc(holders[k], p) {
					holders[k] = append(holders[k], p)
				}
			}
		}
	}
	procFiles := map[int][]string{}
	for k, c := range changed {
		fa := FileActivity{Path: c.path, Device: c.dev, Processes: []string{}}
		if o, ok := before[k]; ok {
			d := c.size - o.Size
			fa.SizeDelta = &d
		}
		for _, p := range holders[k] {
			fa.Processes = append(fa.Processes, label(p))
			procFiles[p.PID] = append(procFiles[p.PID], c.path)
		}
		sort.Strings(fa.Processes)
		res.Files = append(res.Files, fa)
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	if len(res.Files) > maxFiles {
		res.Files, res.FilesTruncated = res.Files[:maxFiles], true
	}

	for pid, pb := range b.Procs {
		pa, ok := a.Procs[pid]
		if ok && pa.Start != pb.Start {
			ok = false // PID reused by a new process
		}
		pr := ProcessActivity{PID: pid, Comm: pb.Comm, Unit: pb.Unit, Files: procFiles[pid]}
		if pb.IO != nil {
			var rb, wb uint64
			if ok && pa.IO != nil {
				rb, wb = sub(pb.IO.ReadBytes, pa.IO.ReadBytes), sub(pb.IO.WriteBytes, pa.IO.WriteBytes)
			} else {
				rb, wb = pb.IO.ReadBytes, pb.IO.WriteBytes // started during the window
			}
			pr.ReadBytes, pr.WriteBytes = &rb, &wb
		}
		if len(pr.Files) == 0 && (pr.WriteBytes == nil || *pr.WriteBytes == 0) {
			continue
		}
		sort.Strings(pr.Files)
		if pr.Files == nil {
			pr.Files = []string{}
		}
		res.Processes = append(res.Processes, pr)
	}
	sort.Slice(res.Processes, func(i, j int) bool {
		wi, wj := val(res.Processes[i].WriteBytes), val(res.Processes[j].WriteBytes)
		if wi != wj {
			return wi > wj
		}
		if li, lj := len(res.Processes[i].Files), len(res.Processes[j].Files); li != lj {
			return li > lj
		}
		return res.Processes[i].PID < res.Processes[j].PID
	})
	if len(res.Processes) > maxProcs {
		res.Processes = res.Processes[:maxProcs]
	}
}

func hasProc(ps []Proc, p Proc) bool {
	for _, q := range ps {
		if q.PID == p.PID && q.Start == p.Start {
			return true
		}
	}
	return false
}

func val(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
