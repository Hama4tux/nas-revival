package updates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

const simulation = `Reading package lists...
Building dependency tree...
Reading state information...
Calculating upgrade...
The following packages have been kept back:
  linux-foo zz-old
  aa-pkg
The following packages will be upgraded:
  libssl3t64 openssl tzdata
Inst libssl3t64 [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [armel]) []
Inst openssl [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [armel])
Inst tzdata [2025b-4] (2025b-4+deb13u1 Debian:13.2/stable, Debian:13.2/stable-updates [all])
Inst libnew1 (1.2-3 Debian:13.2/stable [armel])
Conf libssl3t64 (3.5.1-1+deb13u1 Debian-Security:13/stable-security [armel])
3 upgraded, 1 newly installed, 0 to remove and 3 not upgraded.
`

func TestParseSimulation(t *testing.T) {
	pkgs, kept := ParseSimulation([]byte(simulation))
	want := []Package{
		{Name: "libssl3t64", From: "3.5.1-1", To: "3.5.1-1+deb13u1", Origin: "Debian-Security:13/stable-security", Security: true},
		{Name: "openssl", From: "3.5.1-1", To: "3.5.1-1+deb13u1", Origin: "Debian-Security:13/stable-security", Security: true},
		{Name: "tzdata", From: "2025b-4", To: "2025b-4+deb13u1", Origin: "Debian:13.2/stable, Debian:13.2/stable-updates"},
		{Name: "libnew1", To: "1.2-3", Origin: "Debian:13.2/stable"},
	}
	if !reflect.DeepEqual(pkgs, want) {
		t.Errorf("packages:\n%+v\nwant\n%+v", pkgs, want)
	}
	if !reflect.DeepEqual(kept, []string{"aa-pkg", "linux-foo", "zz-old"}) {
		t.Errorf("kept back: %v", kept)
	}
	if p, k := ParseSimulation([]byte("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n")); len(p) != 0 || len(k) != 0 || p == nil || k == nil {
		t.Errorf("empty: %v %v", p, k)
	}
}

func TestParsePrintURIs(t *testing.T) {
	out := "'http://deb.debian.org/debian/pool/main/o/openssl/openssl_3.5.1-1%2bdeb13u1_armel.deb' openssl_3.5.1-1+deb13u1_armel.deb 1400123 SHA256:ab\n" +
		"'http://x/libssl3t64_3.5.1-1%2bdeb13u1_armel.deb' libssl3t64_3.5.1-1%2bdeb13u1_armel.deb 2000000 SHA256:cd\n" +
		"garbage line\n'http://x/bad.deb' bad.deb notanumber SHA256:x\n"
	got := ParsePrintURIs([]byte(out))
	if !reflect.DeepEqual(got, map[string]int64{"openssl": 1400123, "libssl3t64": 2000000}) {
		t.Errorf("sizes: %v", got)
	}
}

func TestSetupLineAndRestart(t *testing.T) {
	n, v, ok := SetupLine("Setting up libssl3t64:armel (3.5.1-1+deb13u1) ...")
	if !ok || n != "libssl3t64" || v != "3.5.1-1+deb13u1" {
		t.Errorf("setup: %q %q %v", n, v, ok)
	}
	if _, _, ok := SetupLine("Unpacking openssl (3.5.1-1+deb13u1) over (3.5.1-1) ..."); ok {
		t.Error("unpacking is not set up")
	}
	got := NeedsRestart([]string{"tzdata 2025b", "libc6 2.41-12", "openssl 3.5"})
	if !reflect.DeepEqual(got, []string{"libc6", "openssl"}) {
		t.Errorf("restart: %v", got)
	}
}

func TestLoadMissingAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil || s.Packages == nil || s.KeptBack == nil || s.RestartPackages == nil || s.CheckedAt != nil {
		t.Fatalf("missing: %+v %v", s, err)
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.CheckedAt = &now
	s.Packages = []Package{{Name: "a", To: "1", Security: true}}
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !got.CheckedAt.Equal(now) || got.Security() != 1 {
		t.Errorf("round trip: %+v %v", got, err)
	}
	if fi, _ := os.Stat(StatePath(dir)); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
}

// fakeApt answers the apt-get/dpkg calls of a job.
type fakeApt struct {
	calls      []string
	updateOut  string
	updateErr  error
	simulate   string
	upgradeErr error
	setup      []string
}

func (f *fakeApt) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	last := args[len(args)-1]
	switch {
	case last == "update":
		return []byte(f.updateOut), f.updateErr
	case contains(args, "-s"):
		return []byte(f.simulate), nil
	case contains(args, "--print-uris"):
		return []byte("'http://x/openssl_2_armel.deb' openssl_2_armel.deb 500 SHA256:x\n"), nil
	case last == "clean":
		return nil, nil
	}
	return nil, errors.New("unexpected " + name)
}

func (f *fakeApt) stream(line func(string), name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == "dpkg" {
		return nil
	}
	for _, l := range f.setup {
		line(l)
	}
	if f.upgradeErr == nil {
		// After the upgrade nothing is pending any more.
		f.simulate = "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n"
	}
	return f.upgradeErr
}

func contains(l []string, x string) bool {
	for _, v := range l {
		if v == x {
			return true
		}
	}
	return false
}

var boot = time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)

func newJob(t *testing.T, f *fakeApt) *Job {
	clock := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	return &Job{Dir: t.TempDir(), Run: f.run, Stream: f.stream,
		Now:      func() time.Time { clock = clock.Add(time.Second); return clock },
		BootTime: func() (time.Time, error) { return boot, nil },
		Log:      func(string, ...any) {}}
}

const oneUpdate = "Inst openssl [1] (2 Debian-Security:13/stable-security [armel])\n"

func TestCheck(t *testing.T) {
	f := &fakeApt{simulate: oneUpdate}
	j := newJob(t, f)
	if err := j.Execute(context.Background(), JobCheck); err != nil {
		t.Fatal(err)
	}
	st, _ := Load(j.Dir)
	if st.CheckedAt == nil || st.ListedAt == nil || st.CheckError != "" || len(st.Packages) != 1 || st.Packages[0].Size != 500 || st.DownloadBytes != 500 {
		t.Errorf("state: %+v", st)
	}
	if !strings.Contains(f.calls[0], "Acquire::Languages=none") || !strings.Contains(f.calls[0], "DPkg::Lock::Timeout=300") {
		t.Errorf("update args: %s", f.calls[0])
	}
}

func TestCheckFetchFailureKeepsOldListsAndTime(t *testing.T) {
	f := &fakeApt{simulate: oneUpdate, updateOut: "Err:1 http://deb.debian.org/debian trixie InRelease\n  Temporary failure resolving 'deb.debian.org'\nW: Failed to fetch http://deb.debian.org/debian/dists/trixie/InRelease\nW: Some index files failed to download.\n"}
	j := newJob(t, f)
	err := j.Execute(context.Background(), JobCheck)
	st, _ := Load(j.Dir)
	if err == nil || st.CheckedAt != nil || !strings.Contains(st.CheckError, "index files") || len(st.Packages) != 1 {
		t.Errorf("err %v state %+v", err, st)
	}
}

func TestUpgrade(t *testing.T) {
	f := &fakeApt{simulate: oneUpdate, setup: []string{"Unpacking openssl (2) over (1) ...", "Setting up openssl (2) ...", "Setting up libc6:armel (2.41-13) ..."}}
	j := newJob(t, f)
	if err := j.Execute(context.Background(), JobCheck); err != nil {
		t.Fatal(err)
	}
	if err := j.Execute(context.Background(), JobUpgrade); err != nil {
		t.Fatal(err)
	}
	st, _ := Load(j.Dir)
	u := st.Upgrade
	if u == nil || u.Result != ResultOK || u.Total != 1 || u.Done != 2 || u.FinishedAt == nil ||
		!reflect.DeepEqual(u.Packages, []string{"openssl 2", "libc6 2.41-13"}) {
		t.Fatalf("upgrade: %+v", u)
	}
	if len(st.Packages) != 0 || !reflect.DeepEqual(st.RestartPackages, []string{"openssl", "libc6"}) || !st.RestartSince.Equal(boot) {
		t.Errorf("state: %+v", st)
	}
	var sawConfigure, sawConfold, sawClean bool
	for _, c := range f.calls {
		sawConfigure = sawConfigure || c == "dpkg --configure -a"
		sawConfold = sawConfold || (strings.Contains(c, "--force-confold") && strings.Contains(c, "-y"))
		sawClean = sawClean || strings.HasSuffix(c, " clean")
	}
	if !sawConfigure || !sawConfold || !sawClean {
		t.Errorf("calls: %v", f.calls)
	}
	// A later boot forgets the restart reasons of the earlier one.
	boot = boot.Add(time.Hour)
	defer func() { boot = boot.Add(-time.Hour) }()
	f.simulate, f.setup = oneUpdate, []string{"Setting up tzdata (2) ..."}
	if err := j.Execute(context.Background(), JobUpgrade); err != nil {
		t.Fatal(err)
	}
	st, _ = Load(j.Dir)
	if len(st.RestartPackages) != 0 {
		t.Errorf("after reboot: %v", st.RestartPackages)
	}
}

func TestUpgradeFailure(t *testing.T) {
	f := &fakeApt{simulate: oneUpdate, upgradeErr: errors.New("exit status 100"),
		setup: []string{"Setting up openssl (2) ...", "E: Sub-process /usr/bin/dpkg returned an error code (1)"}}
	j := newJob(t, f)
	err := j.Execute(context.Background(), JobUpgrade)
	st, _ := Load(j.Dir)
	if err == nil || st.Upgrade.Result != ResultFailed || !strings.Contains(st.Upgrade.Message, "dpkg returned an error") {
		t.Errorf("err %v upgrade %+v", err, st.Upgrade)
	}
	for _, c := range f.calls {
		if strings.HasSuffix(c, " clean") {
			t.Error("clean after a failed upgrade")
		}
	}
}

func TestExecuteLockAndJob(t *testing.T) {
	j := newJob(t, &fakeApt{simulate: oneUpdate})
	if err := j.Execute(context.Background(), "rm"); err == nil {
		t.Error("unknown job accepted")
	}
	l, err := os.OpenFile(filepath.Join(j.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := flockEx(l); err != nil {
		t.Fatal(err)
	}
	if err := j.Execute(context.Background(), JobCheck); !errors.Is(err, ErrBusy) {
		t.Errorf("locked: %v", err)
	}
}

func flockEx(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

func TestBootTime(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "stat"), []byte("cpu  1 2 3\nbtime 1795760000\nprocesses 9\n"), 0o644)
	got, err := BootTime(dir)
	if err != nil || got.Unix() != 1795760000 {
		t.Errorf("%v %v", got, err)
	}
	os.WriteFile(filepath.Join(dir, "stat"), []byte("cpu 1\n"), 0o644)
	if _, err := BootTime(dir); err == nil {
		t.Error("missing btime accepted")
	}
}
