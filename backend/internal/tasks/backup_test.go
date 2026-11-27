package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nas-revival/backend/internal/shares"
)

func goodBackup() *BackupParams {
	return &BackupParams{Dest: BackupDest{Share: "data", Folder: "daily-backup"}, Host: "192.0.2.10", Port: 22, User: "root", Paths: []string{"nas-export", "hrcm"},
		KeepDays: 35, Fresh: &FreshParams{File: "nas-export/last-ok", MaxAgeHours: 26}, Excludes: []string{"*.tmp"}}
}

func backupTask(b *BackupParams) Task {
	return Task{ID: "pull", Name: "Záloha", Type: TypeBackup, Enabled: true,
		Schedule: Schedule{Kind: KindDaily, Time: "06:45"}, Backup: b}
}

func TestBackupValidationGood(t *testing.T) {
	l, err := Validate([]Task{backupTask(goodBackup())})
	if err != nil || l[0].Backup.Port != 22 {
		t.Fatalf("%v", err)
	}
	b := goodBackup()
	b.Port = 0
	b.Host = "2001:db8::1"
	b.Fresh = nil
	l, err = Validate([]Task{backupTask(b)})
	if err != nil || l[0].Backup.Port != 22 {
		t.Fatalf("%v", err)
	}
	b = goodBackup()
	b.Host = "backup.example.lan"
	if _, err := Validate([]Task{backupTask(b)}); err != nil {
		t.Fatal(err)
	}
}

func TestBackupValidationBad(t *testing.T) {
	mut := map[string]func(b *BackupParams){
		"host":          func(b *BackupParams) { b.Host = "-oProxyCommand=x" },
		"host space":    func(b *BackupParams) { b.Host = "a b" },
		"host zone":     func(b *BackupParams) { b.Host = "fe80::1%eth0" },
		"host empty":    func(b *BackupParams) { b.Host = "" },
		"port":          func(b *BackupParams) { b.Port = 70000 },
		"port neg":      func(b *BackupParams) { b.Port = -1 },
		"user":          func(b *BackupParams) { b.User = "-oFoo" },
		"user upper":    func(b *BackupParams) { b.User = "Root" },
		"path dotdot":   func(b *BackupParams) { b.Paths = []string{"../x"} },
		"path mid":      func(b *BackupParams) { b.Paths = []string{"a/../x"} },
		"path dot":      func(b *BackupParams) { b.Paths = []string{"a/./x"} },
		"path abs":      func(b *BackupParams) { b.Paths = []string{"/etc"} },
		"path space":    func(b *BackupParams) { b.Paths = []string{"a b"} },
		"path colon":    func(b *BackupParams) { b.Paths = []string{"a:b"} },
		"path none":     func(b *BackupParams) { b.Paths = nil },
		"path dup":      func(b *BackupParams) { b.Paths = []string{"a", "a"} },
		"path many":     func(b *BackupParams) { b.Paths = strings.Split("a b c d e f g h i", " ") },
		"keep 0":        func(b *BackupParams) { b.KeepDays = 0 },
		"keep 91":       func(b *BackupParams) { b.KeepDays = 91 },
		"dest share":    func(b *BackupParams) { b.Dest.Share = "a/b" },
		"dest folder":   func(b *BackupParams) { b.Dest.Folder = "a/b" },
		"dest dotdot":   func(b *BackupParams) { b.Dest.Folder = ".." },
		"dest orphan":   func(b *BackupParams) { b.Dest = BackupDest{Folder: "x"} },
		"bw":            func(b *BackupParams) { b.BwlimitKiB = 1000001 },
		"bw neg":        func(b *BackupParams) { b.BwlimitKiB = -1 },
		"fresh outside": func(b *BackupParams) { b.Fresh.File = "other/x" },
		"fresh prefix":  func(b *BackupParams) { b.Fresh.File = "nas-exportx/x" },
		"fresh age":     func(b *BackupParams) { b.Fresh.MaxAgeHours = 721 },
		"fresh dotdot":  func(b *BackupParams) { b.Fresh.File = "nas-export/../x" },
		"exclude opt":   func(b *BackupParams) { b.Excludes = []string{"--delete"} },
		"exclude chars": func(b *BackupParams) { b.Excludes = []string{"a b"} },
		"exclude empty": func(b *BackupParams) { b.Excludes = []string{""} },
		"exclude long":  func(b *BackupParams) { b.Excludes = []string{strings.Repeat("a", 101)} },
		"exclude many": func(b *BackupParams) {
			b.Excludes = make([]string, 17)
			for i := range b.Excludes {
				b.Excludes[i] = "a"
			}
		},
	}
	for name, f := range mut {
		b := goodBackup()
		f(b)
		if _, err := Validate([]Task{backupTask(b)}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// missing params, wrong type combos, too many tasks
	tk := backupTask(nil)
	if _, err := Validate([]Task{tk}); err == nil {
		t.Error("nil params")
	}
	rc := Default()[0]
	rc.Backup = goodBackup()
	if _, err := Validate([]Task{rc}); err == nil {
		t.Error("recycle with backup")
	}
	sm := Task{Name: "s", Type: TypeSmart, Schedule: Schedule{Kind: KindDaily, Time: "01:00"},
		Smart: &SmartParams{DiskID: "d", Test: "short"}, Backup: goodBackup()}
	if _, err := Validate([]Task{sm}); err == nil {
		t.Error("smart with backup")
	}
	bs := backupTask(goodBackup())
	bs.Smart = &SmartParams{DiskID: "d", Test: "short"}
	if _, err := Validate([]Task{bs}); err == nil {
		t.Error("backup with smart")
	}
	var many []Task
	for i := 0; i < 9; i++ {
		x := backupTask(goodBackup())
		x.ID = string(rune('a' + i))
		many = append(many, x)
	}
	if _, err := Validate(many); err == nil {
		t.Error("9 backups")
	}
}

func TestTimerUnitByType(t *testing.T) {
	if u := TimerUnit(backupTask(goodBackup())); !strings.Contains(u, "Unit=nas-revival-backup@pull.service\n") {
		t.Fatal(u)
	}
	if u := TimerUnit(Default()[0]); !strings.Contains(u, "Unit=nas-revival-task@recycle.service\n") {
		t.Fatal(u)
	}
}

func TestImportPull(t *testing.T) {
	conf := "# c\nhrcm backup@192.0.2.10 nas-export hrcm/\n\nsilo root@backup.example.lan data\n"
	exist := Default()
	l, err := ImportPull(strings.NewReader(conf), exist, "")
	if err != nil || len(l) != 3 {
		t.Fatalf("%v %d", err, len(l))
	}
	if l[1].Backup.Fresh == nil || l[1].Backup.Paths[1] != "hrcm" || l[2].Backup.Fresh != nil || l[1].Schedule.Time != "06:45" {
		t.Fatalf("%+v", l[1].Backup)
	}
	if _, err := Validate(l); err != nil {
		t.Fatal(err)
	}
	again, _ := ImportPull(strings.NewReader(conf), l, "05:00")
	if len(again) != 3 || again[1].Schedule.Time != "05:00" {
		t.Fatal("not idempotent")
	}
	for _, bad := range []string{"", "x user@h", "Bad_Name u@h p", "x nohost p"} {
		if _, err := ImportPull(strings.NewReader(bad), nil, ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// --- worker ---

type fakeExec struct {
	calls  [][]string
	code   int
	stderr string
	stdout string
	onRun  func()
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.onRun != nil {
		f.onRun()
	}
	return []byte(f.stdout), []byte(f.stderr), f.code, nil
}

func newWorker(t *testing.T, f *fakeExec) (*BackupWorker, string) {
	t.Helper()
	vol := t.TempDir()
	os.Mkdir(filepath.Join(vol, "data"), 0o750)
	sp := filepath.Join(t.TempDir(), "shares.json")
	if err := shares.Save(sp, shares.Config{Shares: []shares.Share{{Name: "data", Volume: BackupVolume, Folder: "data",
		ReadOnly: true, BackupTarget: true, Permissions: []shares.Permission{}}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 6, 45, 0, 0, time.Local)
	return &BackupWorker{Volume: vol, SharesPath: sp, KeyPath: "/k/id", KnownHosts: "/k/kh", Exec: f.run,
		VolumeMounted: func() bool { return true }, Now: func() time.Time { return now }}, vol
}

func TestBackupRsyncArgv(t *testing.T) {
	f := &fakeExec{stdout: "Total transferred file size: 1,048,576 bytes\n"}
	w, vol := newWorker(t, f)
	b := goodBackup()
	b.Fresh = nil
	b.BwlimitKiB = 500
	b.Host = "2001:db8::1"
	b.Port = 2222
	b.Excludes = []string{"*.tmp", "cache/"}
	o := w.Run(context.Background(), backupTask(b))
	if o.Result != ResultOK || o.Bytes != 2*1048576 || o.Message != "Staženo 2,0 MB, verzí 1." {
		t.Fatalf("%+v", o)
	}
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
	want := []string{"rsync", "-rlt", "--delete", "--chmod=D750,F640", "--timeout=600", "--stats", "--bwlimit=500",
		"--exclude=*.tmp", "--exclude=cache/", "-e",
		"ssh -i /k/id -p 2222 -o BatchMode=yes -o ConnectTimeout=20 -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/k/kh",
		"--", "root@[2001:db8::1]:nas-export/", vol + "/data/daily-backup/pull/current/nas-export/"}
	if strings.Join(f.calls[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%q", f.calls[0])
	}
	for _, c := range f.calls {
		if c[0] != "rsync" {
			t.Fatal("not rsync", c)
		}
	}
}

func TestBackupExitCodes(t *testing.T) {
	cases := []struct {
		code int
		err  string
		msg  string
	}{{23, "", MsgPartial}, {24, "", MsgPartial}, {255, "", MsgHostKey},
		{12, "Host key verification failed.", MsgHostKey}, {12, "", "Stažení selhalo (rsync 12)."}}
	for _, c := range cases {
		f := &fakeExec{code: c.code, stderr: c.err}
		w, vol := newWorker(t, f)
		b := goodBackup()
		b.Fresh = nil
		o := w.Run(context.Background(), backupTask(b))
		if o.Result != ResultFailed || o.Message != c.msg {
			t.Errorf("%d: %+v", c.code, o)
		}
		// snapshot still taken after a failure
		if _, err := os.Stat(filepath.Join(vol, "data", "daily-backup", "pull", "versions", "2026-11-26")); err != nil {
			t.Errorf("no snapshot: %v", err)
		}
	}
}

func TestBackupNoVolumeAndStopped(t *testing.T) {
	f := &fakeExec{}
	w, _ := newWorker(t, f)
	w.VolumeMounted = func() bool { return false }
	if o := w.Run(context.Background(), backupTask(goodBackup())); o.Message != MsgNoVolume || len(f.calls) != 0 {
		t.Fatalf("%+v", o)
	}
	w.VolumeMounted = func() bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	f.onRun = cancel
	if o := w.Run(ctx, backupTask(goodBackup())); o.Result != ResultStopped {
		t.Fatalf("%+v", o)
	}
}

func TestBackupFreshness(t *testing.T) {
	f := &fakeExec{}
	w, vol := newWorker(t, f)
	marker := filepath.Join(jobOf(vol), "current", "nas-export", "last-ok")
	f.onRun = func() {
		os.MkdirAll(filepath.Dir(marker), 0o700)
	}
	// missing
	if o := w.Run(context.Background(), backupTask(goodBackup())); o.Result != ResultFailed || o.Message != "Export na serveru je starší než 26 h." {
		t.Fatalf("%+v", o)
	}
	// old
	os.WriteFile(marker, nil, 0o600)
	old := w.Now().Add(-27 * time.Hour)
	os.Chtimes(marker, old, old)
	if o := w.Run(context.Background(), backupTask(goodBackup())); o.Result != ResultFailed {
		t.Fatalf("%+v", o)
	}
	// fresh
	fresh := w.Now().Add(-2 * time.Hour)
	os.Chtimes(marker, fresh, fresh)
	if o := w.Run(context.Background(), backupTask(goodBackup())); o.Result != ResultOK {
		t.Fatalf("%+v", o)
	}
}

func ino(t *testing.T, p string) uint64 {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Sys().(*syscall.Stat_t).Ino
}

func jobOf(vol string) string { return filepath.Join(vol, "data", "daily-backup", "pull") }

func TestSnapshotHardlinksAndRetention(t *testing.T) {
	f := &fakeExec{}
	w, vol := newWorker(t, f)
	job := jobOf(vol)
	src := filepath.Join(job, "current")
	os.MkdirAll(filepath.Join(src, "d", "e"), 0o750)
	os.WriteFile(filepath.Join(src, "d", "f.txt"), []byte("hello"), 0o640)
	os.WriteFile(filepath.Join(src, "d", "e", "g.txt"), []byte("world!"), 0o640)
	os.Symlink("f.txt", filepath.Join(src, "d", "ln"))
	for _, d := range []string{"2026-11-20", "2026-11-21", "2026-11-22", "2026-01-02"} {
		os.MkdirAll(filepath.Join(job, "versions", d), 0o750)
	}
	os.MkdirAll(filepath.Join(job, "versions", "not-a-date"), 0o750)
	n, err := w.snapshot(job, "2026-11-26", 3)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	snap := filepath.Join(job, "versions", "2026-11-26")
	if ino(t, filepath.Join(snap, "d", "f.txt")) != ino(t, filepath.Join(src, "d", "f.txt")) ||
		ino(t, filepath.Join(snap, "d", "e", "g.txt")) != ino(t, filepath.Join(src, "d", "e", "g.txt")) {
		t.Fatal("files are not hardlinks")
	}
	if l, err := os.Readlink(filepath.Join(snap, "d", "ln")); err != nil || l != "f.txt" {
		t.Fatalf("symlink %q %v", l, err)
	}
	st, _ := os.Stat(filepath.Join(snap, "d"))
	if st.Mode().Perm() != 0o750 {
		t.Fatal(st.Mode())
	}
	// retention: newest 3 of {01-02, 11-20..22, 11-26} -> keeps 11-26, 11-22, 11-21
	for d, want := range map[string]bool{"2026-11-26": true, "2026-11-22": true, "2026-11-21": true,
		"2026-11-20": false, "2026-01-02": false, "not-a-date": true} {
		_, err := os.Stat(filepath.Join(job, "versions", d))
		if (err == nil) != want {
			t.Errorf("%s present=%v", d, err == nil)
		}
	}
	// re-running the same day replaces the snapshot, no tmp left
	if _, err := w.snapshot(job, "2026-11-26", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(job, "versions", ".tmp-2026-11-26")); err == nil {
		t.Error("tmp left")
	}
	// used bytes: 5 + 6, hardlinks counted once
	if u := w.usedBytes(job); u != 11 {
		t.Fatalf("used %d", u)
	}
	if _, err := w.snapshot(job, "../x", 3); err == nil {
		t.Error("bad date accepted")
	}
}

func TestBackupDestinationErrors(t *testing.T) {
	f := &fakeExec{}
	w, vol := newWorker(t, f)
	run := func(b *BackupParams) BackupOutcome { return w.Run(context.Background(), backupTask(b)) }
	b := goodBackup()
	b.Dest = BackupDest{}
	if o := run(b); o.Message != MsgNoDest || len(f.calls) != 0 {
		t.Fatalf("no dest: %+v", o)
	}
	b = goodBackup()
	b.Dest.Share = "missing"
	if o := run(b); o.Message != MsgBadDest || len(f.calls) != 0 {
		t.Fatalf("missing share: %+v", o)
	}
	// a share without backupTarget is refused
	cfg, _ := shares.Load(w.SharesPath)
	cfg.Shares[0].BackupTarget = false
	shares.Save(w.SharesPath, cfg)
	if o := run(goodBackup()); o.Message != MsgBadDest {
		t.Fatalf("not a target: %+v", o)
	}
	cfg.Shares[0].BackupTarget = true
	shares.Save(w.SharesPath, cfg)
	// a symlinked subfolder is refused
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(vol, "data", "daily-backup"))
	if o := run(goodBackup()); o.Message != MsgBadDest || len(f.calls) != 0 {
		t.Fatalf("symlink: %+v", o)
	}
	if _, err := ResolveJobDir(vol, w.SharesPath, backupTask(goodBackup())); !errors.Is(err, ErrBadDest) {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote through the symlink")
	}
}

func TestBackupLayout(t *testing.T) {
	f := &fakeExec{}
	w, vol := newWorker(t, f)
	b := goodBackup()
	b.Fresh = nil
	if o := w.Run(context.Background(), backupTask(b)); o.Result != ResultOK {
		t.Fatalf("%+v", o)
	}
	for _, d := range []string{"current/nas-export", "current/hrcm", "versions/2026-11-26/nas-export"} {
		if st, err := os.Stat(filepath.Join(jobOf(vol), d)); err != nil || !st.IsDir() {
			t.Errorf("%s: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(vol, "apps")); err == nil {
		t.Error("old apps dir created")
	}
}

// --- runner ---

func runnerFixture(t *testing.T, user string, task Task) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	if err := Save(filepath.Join(dir, "tasks.json"), []Task{task}); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "lock")
	os.WriteFile(lock, nil, 0o600)
	f := &fakeExec{}
	w, _ := newWorker(t, f)
	hist := filepath.Join(dir, "task-history.json")
	r := &Runner{TasksPath: filepath.Join(dir, "tasks.json"), HistoryPath: hist, LockPath: lock,
		Wait: time.Second, Retry: time.Millisecond, Backup: w, User: func() string { return user }, Now: time.Now,
		Sleep: sleepCtx}
	return r, hist
}

func TestRunnerAccountCheck(t *testing.T) {
	r, hist := runnerFixture(t, "nas-revival", backupTask(goodBackup()))
	if run, err := r.Run(context.Background(), "pull"); err != nil || run.Result != ResultFailed || run.Message != MsgWrongAccount {
		t.Fatalf("%+v %v", run, err)
	}
	if _, err := os.Stat(hist); err == nil {
		t.Error("history written")
	}
	r, hist = runnerFixture(t, BackupUser, Default()[0])
	if run, _ := r.Run(context.Background(), "recycle"); run.Message != MsgWrongAccount {
		t.Fatalf("%+v", run)
	}
	if _, err := os.Stat(hist); err == nil {
		t.Error("history written")
	}
}

func TestRunnerBackupHistory(t *testing.T) {
	b := goodBackup()
	b.Fresh = nil
	r, hist := runnerFixture(t, BackupUser, backupTask(b))
	r.Backup.Exec = (&fakeExec{stdout: "Total transferred file size: 100 bytes\n"}).run
	run, err := r.Run(context.Background(), "pull")
	if err != nil || run.Result != ResultOK || run.Bytes != 200 {
		t.Fatalf("%+v %v", run, err)
	}
	st, err := os.Stat(hist)
	if err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("%v %v", st, err)
	}
	h, _ := LoadHistory(hist)
	if got := h.Runs["pull"][0]; got.Bytes != 200 || got.UsedBytes != 0 && got.UsedBytes < 0 {
		t.Fatalf("%+v", got)
	}
	raw, _ := os.ReadFile(hist)
	if !strings.Contains(string(raw), `"bytes": 200`) {
		t.Fatal(string(raw))
	}
}

func TestParseTransferred(t *testing.T) {
	for in, want := range map[string]int64{"Total transferred file size: 12,345 bytes": 12345,
		"x\nTotal transferred file size: 0 bytes\n": 0, "nothing": 0, "Total transferred file size: abc bytes": 0} {
		if got := ParseTransferred(in); got != want {
			t.Errorf("%q: %d", in, got)
		}
	}
}
