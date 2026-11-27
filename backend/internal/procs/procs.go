// Package procs reads the process list from /proc (W3.2, MON-06) and decides
// which processes the web UI must never stop. The web process uses it to show
// the list, the root helper uses the same rules again before it sends a
// signal (process.kill), so the UI can't be talked into killing something the
// helper would refuse.
package procs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// clockTicks is USER_HZ, 100 on every Linux architecture Debian ships.
const clockTicks = 100

// pfKthread is PF_KTHREAD in the flags field of /proc/PID/stat.
const pfKthread = 0x00200000

// maxCmd limits the command line sent to the browser.
const maxCmd = 300

// Reasons a process is protected; the UI translates them.
const (
	ReasonInit    = "init"    // PID 1
	ReasonKernel  = "kernel"  // kernel thread
	ReasonSystem  = "system"  // service the NAS needs to stay reachable
	ReasonRevival = "revival" // NAS Revival itself (web, helper, tasks)
)

// protectedNames are /proc comm names (cut to 15 characters by the kernel).
// Without them the NAS loses its log, devices, D-Bus, network, login or remote
// access; a stuck one is restarted from the Services app instead.
var protectedNames = map[string]string{
	"systemd":         ReasonSystem, // PID 1 and the user managers
	"systemd-journal": ReasonSystem,
	"systemd-udevd":   ReasonSystem,
	"systemd-logind":  ReasonSystem,
	"systemd-network": ReasonSystem,
	"systemd-resolve": ReasonSystem,
	"systemd-timesyn": ReasonSystem,
	"dbus-daemon":     ReasonSystem,
	"dbus-broker":     ReasonSystem,
	"dbus-broker-lau": ReasonSystem,
	"sshd":            ReasonSystem, // the listener; single sessions (sshd-session) may be stopped
	"agetty":          ReasonSystem, // serial console login, the rescue path
	"nas-revival":     ReasonRevival,
}

// Process is one row of the list.
type Process struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	Name    string  `json:"name"`
	Cmd     string  `json:"cmd"`
	User    string  `json:"user"`
	State   string  `json:"state"`
	Threads int     `json:"threads"`
	RSS     uint64  `json:"rssBytes"`
	CPU     float64 `json:"cpuPercent"`
	// Start is the start time in clock ticks after boot; the kill request
	// sends it back so a reused PID is never hit.
	Start     uint64 `json:"start"`
	StartedAt int64  `json:"startedAt"` // Unix seconds
	Protected string `json:"protected,omitempty"`
}

// List is the response of GET /monitor/processes.
type List struct {
	Processes []Process `json:"processes"`
	MemTotal  uint64    `json:"memTotalBytes"`
	SampledAt time.Time `json:"sampledAt"`
}

// Stat is the part of /proc/PID/stat the rules and the list need.
type Stat struct {
	PID     int
	Name    string
	State   string
	PPID    int
	Flags   uint64
	CPU     uint64 // utime + stime, ticks
	Threads int
	Start   uint64
	RSS     uint64 // pages
}

// ErrNotFound: the process is gone.
var ErrNotFound = errors.New("process not found")

// ParseStat parses /proc/PID/stat. The name is in parentheses and may itself
// contain spaces and ')', so the fields start after the last ')'.
func ParseStat(data []byte) (Stat, error) {
	var s Stat
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 1 || end < open {
		return s, errors.New("bad stat")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data[:open])))
	if err != nil {
		return s, errors.New("bad stat pid")
	}
	f := strings.Fields(string(data[end+1:]))
	// f[0] is field 3 of proc(5); field n is f[n-3].
	if len(f) < 22 {
		return s, errors.New("short stat")
	}
	num := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
	// pid and thread count are small; ParseInt with bitSize 32 keeps int safe on 32-bit.
	small := func(i int) int { v, _ := strconv.ParseInt(f[i], 10, 32); return int(v) }
	s.PID = pid
	s.Name = string(data[open+1 : end])
	s.State = f[0]
	s.PPID = small(1)
	s.Flags = num(6)
	s.CPU = num(11) + num(12)
	s.Threads = small(17)
	s.Start = num(19)
	s.RSS = num(21)
	return s, nil
}

// Protection returns why the process must not be stopped, or "".
// self is the PID of the caller (the helper never stops itself).
func Protection(s Stat, self int) string {
	switch {
	case s.PID == 1:
		return ReasonInit
	case s.PID == 2 || s.PPID == 2 || s.Flags&pfKthread != 0:
		return ReasonKernel
	case s.PID == self:
		return ReasonRevival
	}
	return protectedNames[s.Name]
}

// ReadStat reads one process below root ("/" on the NAS).
func ReadStat(root string, pid int) (Stat, error) {
	data, err := os.ReadFile(filepath.Join(root, "proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return Stat{}, ErrNotFound
		}
		return Stat{}, err
	}
	return ParseStat(data)
}

// Reader builds the list. A reading is reused for MinInterval, so several open
// pages cost one walk of /proc; CPU % is the share of the whole machine since
// the previous reading.
type Reader struct {
	Root        string        // "/" on the NAS
	MinInterval time.Duration // 0 = 4 s
	// Sleep waits between the two readings of a cold start; nil = real sleep.
	Sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time

	mu       sync.Mutex
	prev     map[int]uint64 // pid → CPU ticks; keyed with start below
	prevKey  map[int]uint64 // pid → start, to drop reused PIDs
	prevTot  uint64
	prevAt   time.Time
	cached   List
	cachedAt time.Time
	users    map[uint32]string
	usersAt  time.Time
}

func (r *Reader) path(p ...string) string {
	return filepath.Join(append([]string{r.Root}, p...)...)
}

func (r *Reader) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// List returns the processes sorted by CPU, then RSS.
func (r *Reader) List(ctx context.Context) (List, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	min := r.MinInterval
	if min == 0 {
		min = 4 * time.Second
	}
	now := r.clock()
	if !r.cachedAt.IsZero() && now.Sub(r.cachedAt) < min {
		return r.cached, nil
	}
	// Without a recent baseline CPU % would be meaningless: take one, wait a
	// second and measure against it.
	if r.prevAt.IsZero() || now.Sub(r.prevAt) > time.Minute {
		if _, err := r.read(); err != nil {
			return List{}, err
		}
		sleep := r.Sleep
		if sleep == nil {
			sleep = func(ctx context.Context, d time.Duration) error {
				t := time.NewTimer(d)
				defer t.Stop()
				select {
				case <-t.C:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if err := sleep(ctx, time.Second); err != nil {
			return List{}, err
		}
	}
	l, err := r.read()
	if err != nil {
		return List{}, err
	}
	r.cached, r.cachedAt = l, r.clock()
	return l, nil
}

func (r *Reader) read() (List, error) {
	total, btime, err := r.readProcStat()
	if err != nil {
		return List{}, err
	}
	entries, err := os.ReadDir(r.path("proc"))
	if err != nil {
		return List{}, err
	}
	users := r.userNames()
	page := uint64(os.Getpagesize())
	dt := total - r.prevTot
	if r.prevTot == 0 || total <= r.prevTot {
		dt = 0
	}
	cur, curKey := map[int]uint64{}, map[int]uint64{}
	out := make([]Process, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		s, err := ReadStat(r.Root, pid)
		if err != nil {
			continue // exited meanwhile
		}
		p := Process{PID: pid, PPID: s.PPID, Name: s.Name, State: s.State, Threads: s.Threads,
			RSS: s.RSS * page, Start: s.Start, StartedAt: btime + int64(s.Start/clockTicks),
			Protected: Protection(s, -1)}
		if fi, err := os.Stat(r.path("proc", e.Name())); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				p.User = users[st.Uid]
				if p.User == "" {
					p.User = strconv.FormatUint(uint64(st.Uid), 10)
				}
			}
		}
		p.Cmd = r.cmdline(e.Name())
		if p.Cmd == "" {
			p.Cmd = "[" + s.Name + "]"
		}
		if prev, ok := r.prev[pid]; ok && dt > 0 && r.prevKey[pid] == s.Start && s.CPU >= prev {
			p.CPU = float64(int(float64(s.CPU-prev)/float64(dt)*1000+0.5)) / 10
		}
		cur[pid], curKey[pid] = s.CPU, s.Start
		out = append(out, p)
	}
	r.prev, r.prevKey, r.prevTot, r.prevAt = cur, curKey, total, r.clock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		if out[i].RSS != out[j].RSS {
			return out[i].RSS > out[j].RSS
		}
		return out[i].PID < out[j].PID
	})
	return List{Processes: out, MemTotal: r.memTotal(), SampledAt: r.clock().UTC()}, nil
}

// readProcStat returns the sum of all CPU ticks and the boot time.
func (r *Reader) readProcStat() (uint64, int64, error) {
	data, err := os.ReadFile(r.path("proc", "stat"))
	if err != nil {
		return 0, 0, err
	}
	var total uint64
	var btime int64
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "cpu":
			for _, v := range f[1:] {
				n, _ := strconv.ParseUint(v, 10, 64)
				total += n
			}
		case "btime":
			if len(f) > 1 {
				btime, _ = strconv.ParseInt(f[1], 10, 64)
			}
		}
	}
	if total == 0 {
		return 0, 0, errors.New("no cpu line in /proc/stat")
	}
	return total, btime, nil
}

func (r *Reader) cmdline(pid string) string {
	data, err := os.ReadFile(r.path("proc", pid, "cmdline"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(strings.ReplaceAll(string(bytes.TrimRight(data, "\x00")), "\x00", " "))
	if len(s) > maxCmd {
		cut := maxCmd
		for cut > 0 && s[cut]&0xC0 == 0x80 { // keep UTF-8 whole
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

func (r *Reader) memTotal() uint64 {
	f, err := os.Open(r.path("proc", "meminfo"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) >= 2 && f[0] == "MemTotal:" {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}

// userNames maps uid → name from /etc/passwd, re-read at most once a minute.
func (r *Reader) userNames() map[uint32]string {
	if r.users != nil && r.clock().Sub(r.usersAt) < time.Minute {
		return r.users
	}
	m := map[uint32]string{}
	if data, err := os.ReadFile(r.path("etc", "passwd")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Split(line, ":")
			if len(f) < 3 {
				continue
			}
			if uid, err := strconv.ParseUint(f[2], 10, 32); err == nil {
				m[uint32(uid)] = f[0]
			}
		}
	}
	r.users, r.usersAt = m, r.clock()
	return m
}
