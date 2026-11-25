package diskactivity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fixture struct {
	t    *testing.T
	root string // fake /proc and /sys
	data string // real directory standing in for the mounted file system
	dev  uint64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir(), data: t.TempDir()}
	var st syscall.Stat_t
	if err := syscall.Stat(f.data, &st); err != nil {
		t.Fatal(err)
	}
	f.dev = uint64(st.Dev)
	major := ((f.dev >> 8) & 0xfff) | ((f.dev >> 32) &^ 0xfff)
	minor := (f.dev & 0xff) | ((f.dev >> 12) &^ 0xff)
	f.write("proc/self/mountinfo", fmt.Sprintf(
		"22 1 %d:%d / %s rw,noatime shared:1 - ext4 /dev/sda1 rw\n"+
			"30 22 %d:%d /sub %s/bind rw shared:1 - ext4 /dev/sda1 rw\n"+
			"23 1 0:5 / /proc rw - proc proc rw\n", major, minor, f.data, major, minor, f.data))
	f.write("proc/swaps", "Filename Type Size Used Priority\n/dev/sda2 partition 1048572 0 -2\n")
	f.write("proc/vmstat", "nr_free_pages 100\npswpin 10\npswpout 20\n")
	f.write("proc/fs/jbd2/sda1-8/info", "100 transactions (50 requested), each up to 10922 blocks\n")
	f.diskstats(1000, 2000, 0)
	for _, p := range []string{"sda1", "sda2"} {
		f.write("sys/devices/ata1/block/sda/"+p+"/partition", "1\n")
		f.link("../../devices/ata1/block/sda/"+p, "sys/class/block/"+p)
	}
	f.write("sys/devices/ata1/block/sda/size", "1\n")
	f.link("../../devices/ata1/block/sda", "sys/class/block/sda")
	return f
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) link(target, rel string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.Symlink(target, p); err != nil {
		f.t.Fatal(err)
	}
}

// diskstats: sda1 carries all writes; sda2 (swap) gets swapWrites sectors.
func (f *fixture) diskstats(writeOps, writeSectors, swapWrites uint64) {
	f.write("proc/diskstats", fmt.Sprintf(
		"   8 0 sda 50 0 400 0 %d 0 %d 0 0 0 0\n"+
			"   8 1 sda1 40 0 300 0 %d 0 %d 0 0 0 0\n"+
			"   8 2 sda2 10 0 100 0 0 0 %d 0 0 0 0\n"+
			"   7 0 loop0 1 0 1 0 1 0 1 0 0 0 0\n",
		writeOps, writeSectors+swapWrites, writeOps, writeSectors, swapWrites))
}

func (f *fixture) proc(pid int, comm, unit string, openFile string) {
	base := fmt.Sprintf("proc/%d/", pid)
	f.write(base+"comm", comm+"\n")
	f.write(base+"cgroup", "0::/system.slice/"+unit+"\n")
	f.write(base+"stat", fmt.Sprintf("%d (%s) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 4242 0 0\n", pid, comm))
	if openFile != "" {
		f.write(base+"fdinfo/3", "pos:\t0\nflags:\t0102002\nmnt_id:\t22\n")
		f.link(openFile, base+"fd/3")
		f.write(base+"fdinfo/4", "pos:\t0\nflags:\t0100000\n") // read-only, ignored
		f.link(openFile, base+"fd/4")
	}
}

func TestMountsPrefersFileSystemRoot(t *testing.T) {
	f := newFixture(t)
	ms, err := Reader{Root: f.root}.Mounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Point != f.data || ms[0].Device != "sda1" || ms[0].Dev != f.dev || ms[0].FSType != "ext4" {
		t.Fatalf("mounts = %+v", ms)
	}
}

func TestHelpers(t *testing.T) {
	if u := unitOf("0::/system.slice/systemd-journald.service\n"); u != "systemd-journald.service" {
		t.Errorf("unit = %q", u)
	}
	if u := unitOf("0::/user.slice/user-0.slice/session-3.scope\n"); u != "session-3.scope" {
		t.Errorf("unit = %q", u)
	}
	if u := unitOf("0::/\n"); u != "" {
		t.Errorf("kernel thread unit = %q", u)
	}
	if got := unescape(`/srv/my\040data`); got != "/srv/my data" {
		t.Errorf("unescape = %q", got)
	}
	if mkdev(8, 1) != 0x801 || mkdev(259, 300) != (300&0xff)|(259<<8)|((300&^0xff)<<12) {
		t.Error("mkdev")
	}
}

func TestMeasureAttributesOpenFileAndDevices(t *testing.T) {
	f := newFixture(t)
	journal := filepath.Join(f.data, "system.journal")
	os.WriteFile(journal, []byte("old"), 0o644)
	os.WriteFile(filepath.Join(f.data, "untouched"), []byte("x"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(f.data, "untouched"), old, old)
	f.proc(100, "systemd-journal", "systemd-journald.service", journal)
	f.proc(200, "bash", "session-1.scope", "")
	r := Reader{Root: f.root}

	mounts, _ := r.Mounts()
	time.Sleep(2 * clockSlack) // the files above predate the window
	a, err := r.Take(mounts)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(journal, []byte("old and new"), 0o644)
	os.WriteFile(filepath.Join(f.data, "fresh.log"), []byte("n"), 0o644)
	f.diskstats(1012, 2096, 8)
	f.write("proc/vmstat", "pswpin 10\npswpout 21\n")
	f.write("proc/fs/jbd2/sda1-8/info", "103 transactions\n")
	b, err := r.Take(mounts)
	if err != nil {
		t.Fatal(err)
	}
	res := r.compare(a, b, mounts, 15)
	changed, truncated, skipped := scanChanged(context.Background(), mounts, idleDevices(res.Devices), a.Time.Add(-clockSlack))
	attribute(&res, a, b, mounts, changed)

	if res.ProcessIO || res.SwapOut != 1 || res.SwapIn != 0 || truncated || len(skipped) != 0 {
		t.Fatalf("res = %+v truncated=%v skipped=%v", res, truncated, skipped)
	}
	if len(res.Devices) != 3 { // loop0 excluded
		t.Fatalf("devices = %+v", res.Devices)
	}
	sda1 := res.Devices[1]
	if sda1.Name != "sda1" || sda1.Parent != "sda" || sda1.WriteOps != 12 || sda1.WriteBytes != 96*512 ||
		sda1.Mount != f.data || sda1.JournalCommits == nil || *sda1.JournalCommits != 3 {
		t.Fatalf("sda1 = %+v", sda1)
	}
	if !res.Devices[2].Swap || res.Devices[2].WriteBytes != 8*512 || res.Devices[0].Parent != "" {
		t.Fatalf("devices = %+v", res.Devices)
	}
	if len(res.Files) != 2 || res.Files[0].Path != filepath.Join(f.data, "fresh.log") || res.Files[1].Path != journal {
		t.Fatalf("files = %+v", res.Files)
	}
	jf := res.Files[1]
	if jf.SizeDelta == nil || *jf.SizeDelta != 8 || len(jf.Processes) != 1 || jf.Processes[0] != "systemd-journal (100)" || jf.Device != "sda1" {
		t.Fatalf("journal = %+v", jf)
	}
	if res.Files[0].SizeDelta != nil || len(res.Files[0].Processes) != 0 {
		t.Fatalf("fresh = %+v", res.Files[0])
	}
	if len(res.Processes) != 1 || res.Processes[0].PID != 100 || res.Processes[0].Unit != "systemd-journald.service" ||
		res.Processes[0].WriteBytes != nil || len(res.Processes[0].Files) != 1 {
		t.Fatalf("processes = %+v", res.Processes)
	}
}

func TestIdleDiskIsNotSearched(t *testing.T) {
	f := newFixture(t)
	os.WriteFile(filepath.Join(f.data, "fresh"), []byte("n"), 0o644)
	r := Reader{Root: f.root}
	mounts, _ := r.Mounts()
	a, _ := r.Take(mounts)
	b, _ := r.Take(mounts) // counters unchanged: the disk did nothing
	res := r.compare(a, b, mounts, 10)
	changed, _, skipped := scanChanged(context.Background(), mounts, idleDevices(res.Devices), a.Time.Add(-time.Hour))
	if len(changed) != 0 || len(skipped) != 1 || skipped[0] != f.data {
		t.Fatalf("changed=%v skipped=%v", changed, skipped)
	}
}

func TestProcessIOWhenKernelHasAccounting(t *testing.T) {
	f := newFixture(t)
	f.proc(300, "rsync", "backup.service", "")
	f.write("proc/300/io", "rchar: 1\nwchar: 2\nread_bytes: 4096\nwrite_bytes: 8192\ncancelled_write_bytes: 0\n")
	f.proc(301, "sleep", "x.service", "")
	f.write("proc/301/io", "read_bytes: 0\nwrite_bytes: 0\n")
	r := Reader{Root: f.root}
	mounts, _ := r.Mounts()
	a, _ := r.Take(mounts)
	f.write("proc/300/io", "read_bytes: 4096\nwrite_bytes: 1056768\n")
	b, _ := r.Take(mounts)
	res := r.compare(a, b, mounts, 10)
	attribute(&res, a, b, mounts, map[fileKey]changedFile{})
	if !res.ProcessIO || len(res.Processes) != 1 || *res.Processes[0].WriteBytes != 1048576 || *res.Processes[0].ReadBytes != 0 {
		t.Fatalf("res = %+v", res.Processes)
	}
}

func TestMeasureHonoursContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Reader{Root: f.root}).Measure(ctx, 10); err == nil {
		t.Fatal("cancelled measurement returned no error")
	}
}

func ptr[T any](v T) *T { return &v }

func codes(h []Hint) []string {
	out := []string{}
	for _, x := range h {
		out = append(out, x.Code+":"+x.Value)
	}
	return out
}

func TestExplain(t *testing.T) {
	r := Result{
		Devices: []DeviceActivity{
			{Name: "sda", DevStatDelta: DevStatDelta{WriteOps: 20, WriteBytes: 81920}},
			{Name: "sda1", Parent: "sda", Mount: "/", DevStatDelta: DevStatDelta{WriteOps: 12}, JournalCommits: ptr[uint64](2)},
			{Name: "sda2", Parent: "sda", Swap: true, DevStatDelta: DevStatDelta{WriteOps: 8}},
			{Name: "sda3", Parent: "sda", Mount: "/srv/data", JournalCommits: ptr[uint64](0)},
			{Name: "sdb"},
			{Name: "sdb1", Parent: "sdb", Mount: "/boot"},
		},
		Files: []FileActivity{{Path: "/var/log/journal/x/system.journal", Device: "sda1"}},
	}
	got := Explain(r, []DiskInfo{
		{Name: "sda", ID: "ata-X", System: true, SharedWithData: true, StandbyMinutes: ptr(0)},
		{Name: "sdb", ID: "usb-Y"},
	})
	if len(got) != 2 || got[0].ID != "ata-X" || len(got[0].Partitions) != 3 || got[0].WriteBytes != 81920 {
		t.Fatalf("summary = %+v", got)
	}
	want := []string{"no_standby:", "journald:", "swap:", "shared_data:"}
	if fmt.Sprint(codes(got[0].Hints)) != fmt.Sprint(want) {
		t.Errorf("sda hints = %v, want %v", codes(got[0].Hints), want)
	}
	if fmt.Sprint(codes(got[1].Hints)) != "[idle:]" {
		t.Errorf("sdb hints = %v", codes(got[1].Hints))
	}

	// Only journal commits, standby set: the writes block the timeout.
	r2 := Result{Devices: []DeviceActivity{
		{Name: "sda", DevStatDelta: DevStatDelta{WriteOps: 3}},
		{Name: "sda1", Parent: "sda", DevStatDelta: DevStatDelta{WriteOps: 3}, JournalCommits: ptr[uint64](1)},
	}}
	got = Explain(r2, []DiskInfo{{Name: "sda", StandbyMinutes: ptr(20)}})
	if fmt.Sprint(codes(got[0].Hints)) != "[writes_wake:20 fs_journal:sda1]" {
		t.Errorf("hints = %v", codes(got[0].Hints))
	}
	r3 := Result{Devices: []DeviceActivity{{Name: "sdc", DevStatDelta: DevStatDelta{WriteOps: 1}}}}
	got = Explain(r3, nil)
	if got[0].ID != "" || fmt.Sprint(codes(got[0].Hints)) != "[unattributed:]" {
		t.Errorf("unknown disk = %+v", got[0])
	}
}
