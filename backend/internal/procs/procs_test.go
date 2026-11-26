package procs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// statLine builds /proc/PID/stat with the fields the parser reads.
func statLine(pid int, name, state string, ppid int, flags, utime, stime uint64, threads int, start, rss uint64) string {
	f := make([]string, 50)
	for i := range f {
		f[i] = "0"
	}
	// f[n-3] is field n of proc(5).
	f[0], f[1], f[6] = state, fmt.Sprint(ppid), fmt.Sprint(flags)
	f[11], f[12], f[17], f[19], f[21] = fmt.Sprint(utime), fmt.Sprint(stime), fmt.Sprint(threads), fmt.Sprint(start), fmt.Sprint(rss)
	return fmt.Sprintf("%d (%s) %s\n", pid, name, strings.Join(f, " "))
}

type fakeProc struct {
	pid  int
	stat string
	cmd  string
}

func writeTree(t *testing.T, root string, cpuTotal uint64, ps []fakeProc) {
	t.Helper()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "proc"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	// user nice system idle → total cpuTotal
	must(os.WriteFile(filepath.Join(root, "proc", "stat"),
		[]byte(fmt.Sprintf("cpu  %d 0 0 0 0 0 0 0\ncpu0 1 0 0 0\nbtime 1700000000\n", cpuTotal)), 0o644))
	must(os.WriteFile(filepath.Join(root, "proc", "meminfo"), []byte("MemTotal:  262144 kB\nMemFree: 1 kB\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte(fmt.Sprintf("tester:x:%d:%d::/:/bin/sh\n", os.Getuid(), os.Getgid())), 0o644))
	for _, p := range ps {
		d := filepath.Join(root, "proc", fmt.Sprint(p.pid))
		must(os.MkdirAll(d, 0o755))
		must(os.WriteFile(filepath.Join(d, "stat"), []byte(p.stat), 0o644))
		must(os.WriteFile(filepath.Join(d, "cmdline"), []byte(p.cmd), 0o644))
	}
}

func TestParseStatNameWithParens(t *testing.T) {
	s, err := ParseStat([]byte(statLine(42, "a) (b", "S", 7, 0, 5, 3, 2, 900, 10)))
	if err != nil {
		t.Fatal(err)
	}
	if s.PID != 42 || s.Name != "a) (b" || s.PPID != 7 || s.CPU != 8 || s.Threads != 2 || s.Start != 900 || s.RSS != 10 || s.State != "S" {
		t.Fatalf("parsed %+v", s)
	}
	if _, err := ParseStat([]byte("garbage")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestProtection(t *testing.T) {
	cases := []struct {
		s    Stat
		want string
	}{
		{Stat{PID: 1, Name: "systemd"}, ReasonInit},
		{Stat{PID: 2, Name: "kthreadd"}, ReasonKernel},
		{Stat{PID: 30, PPID: 2, Name: "kworker/0:1"}, ReasonKernel},
		{Stat{PID: 31, PPID: 1, Flags: pfKthread, Name: "odd"}, ReasonKernel},
		{Stat{PID: 300, PPID: 1, Name: "systemd-journal"}, ReasonSystem},
		{Stat{PID: 301, PPID: 1, Name: "sshd"}, ReasonSystem},
		{Stat{PID: 302, PPID: 1, Name: "nas-revival"}, ReasonRevival},
		{Stat{PID: 303, PPID: 1, Name: "anything"}, ReasonRevival}, // self
		{Stat{PID: 400, PPID: 301, Name: "sshd-session"}, ""},
		{Stat{PID: 401, PPID: 1, Name: "rsync"}, ""},
	}
	for _, c := range cases {
		if got := Protection(c.s, 303); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.s, got, c.want)
		}
	}
}

func TestReaderListCPUAndCache(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, 1000, []fakeProc{
		{1, statLine(1, "systemd", "S", 0, 0, 10, 10, 1, 1, 100), "/sbin/init\x00"},
		{2, statLine(2, "kthreadd", "S", 0, pfKthread, 0, 0, 1, 1, 0), ""},
		{500, statLine(500, "rsync", "R", 1, 0, 100, 0, 1, 5000, 50), "rsync\x00-a\x00/srv\x00"},
	})
	now := time.Unix(1700001000, 0)
	slept := false
	r := &Reader{Root: root, now: func() time.Time { return now }, Sleep: func(ctx context.Context, d time.Duration) error {
		// The second reading of the cold start: rsync used 50 of 200 ticks.
		slept = true
		writeTree(t, root, 1200, []fakeProc{
			{1, statLine(1, "systemd", "S", 0, 0, 10, 10, 1, 1, 100), "/sbin/init\x00"},
			{2, statLine(2, "kthreadd", "S", 0, pfKthread, 0, 0, 1, 1, 0), ""},
			{500, statLine(500, "rsync", "R", 1, 0, 150, 0, 1, 5000, 50), "rsync\x00-a\x00/srv\x00"},
		})
		now = now.Add(d)
		return nil
	}}
	l, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slept || len(l.Processes) != 3 {
		t.Fatalf("slept %v, list %+v", slept, l)
	}
	top := l.Processes[0]
	page := uint64(os.Getpagesize())
	if top.PID != 500 || top.CPU != 25 || top.Cmd != "rsync -a /srv" || top.RSS != 50*page || top.Protected != "" || top.User != "tester" {
		t.Fatalf("top %+v", top)
	}
	if top.StartedAt != 1700000000+50 {
		t.Fatalf("startedAt %d", top.StartedAt)
	}
	if l.MemTotal != 262144*1024 {
		t.Fatalf("memTotal %d", l.MemTotal)
	}
	byPID := map[int]Process{}
	for _, p := range l.Processes {
		byPID[p.PID] = p
	}
	if byPID[1].Protected != ReasonInit || byPID[2].Protected != ReasonKernel || byPID[2].Cmd != "[kthreadd]" {
		t.Fatalf("protected/cmd: %+v %+v", byPID[1], byPID[2])
	}

	// Within MinInterval the same reading is returned, no new walk.
	slept = false
	writeTree(t, root, 1400, nil)
	if l2, _ := r.List(context.Background()); l2.SampledAt != l.SampledAt || slept {
		t.Fatal("cache not used")
	}

	// A reused PID (other start time) does not inherit the old CPU ticks.
	now = now.Add(5 * time.Second)
	writeTree(t, root, 1400, []fakeProc{
		{500, statLine(500, "other", "R", 1, 0, 400, 0, 1, 9999, 1), "other\x00"},
	})
	l3, err := r.List(context.Background())
	if err != nil || slept {
		t.Fatalf("warm reading slept %v err %v", slept, err)
	}
	for _, p := range l3.Processes {
		if p.PID == 500 && p.CPU != 0 {
			t.Fatalf("reused pid got cpu %v", p.CPU)
		}
	}
}

func TestReadStatMissing(t *testing.T) {
	if _, err := ReadStat(t.TempDir(), 12345); err != ErrNotFound {
		t.Fatalf("err %v", err)
	}
}
