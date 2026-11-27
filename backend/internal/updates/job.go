package updates

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ErrBusy: the other job holds the lock.
var ErrBusy = errors.New("another update job is running")

// Job runs one instance of nas-revival-updates@.service.
type Job struct {
	Dir string // DefaultStateDir
	// Run executes a command and returns stdout and stderr together; a
	// non-zero exit is an error. ctx may cancel it (the check only).
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Stream executes a command that must not be killed by a cancelled context
	// (dpkg) and calls line for every output line.
	Stream   func(line func(string), name string, args ...string) error
	Now      func() time.Time
	BootTime func() (time.Time, error)
	// Log receives a one-line summary for the job's journal.
	Log func(format string, args ...any)
}

// aptOpts are passed to every apt-get call: wait for the dpkg lock (a
// manual apt run, apt-daily) instead of failing at once.
var aptOpts = []string{"-q", "-o", "DPkg::Lock::Timeout=300"}

func apt(args ...string) []string { return append(append([]string{}, aptOpts...), args...) }

// Execute takes the job lock and runs job.
func (j *Job) Execute(ctx context.Context, job string) error {
	if !ValidJob(job) {
		return fmt.Errorf("unknown job %q", job)
	}
	lock, err := os.OpenFile(filepath.Join(j.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return ErrBusy
	}
	if job == JobCheck {
		return j.check(ctx)
	}
	return j.upgrade()
}

// check refreshes the package lists and computes the pending upgrades.
func (j *Job) check(ctx context.Context) error {
	st, err := Load(j.Dir)
	if err != nil {
		return err
	}
	// Translations are not shown anywhere; skipping them saves ~35 MB of lists and RAM.
	out, err := j.Run(ctx, "apt-get", apt("-o", "Acquire::Languages=none", "update")...)
	if err == nil {
		err = fetchError(out)
	}
	if err != nil {
		st.CheckError = LastLines(out, 3)
		if st.CheckError == "" {
			st.CheckError = err.Error()
		}
		j.Log("check: apt-get update failed: %v", err)
	} else {
		now := j.Now()
		st.CheckedAt, st.CheckError = &now, ""
	}
	// Old lists still give a valid answer when the refresh failed.
	if rerr := j.refresh(ctx, &st); rerr != nil {
		st.CheckError = "Seznam aktualizací nelze spočítat: " + rerr.Error()
		err = rerr
	}
	if serr := Save(j.Dir, st); serr != nil {
		return serr
	}
	j.Log("check: %d updates (%d security), %d kept back", len(st.Packages), st.Security(), len(st.KeptBack))
	return err
}

// fetchError: apt-get update exits 0 when an index could not be fetched and
// it fell back to the old one ("W: Failed to fetch", "Err:").
func fetchError(out []byte) error {
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "Err:") || strings.HasPrefix(l, "W: Failed to fetch ") {
			return errors.New("some package lists could not be downloaded")
		}
	}
	return nil
}

// refresh recomputes the pending upgrades from the current lists (no network).
func (j *Job) refresh(ctx context.Context, st *State) error {
	out, err := j.Run(ctx, "apt-get", apt("-s", "-o", "APT::Get::Show-User-Simulation-Note=false", "upgrade", "--with-new-pkgs")...)
	if err != nil {
		return fmt.Errorf("apt-get -s: %w", err)
	}
	pkgs, kept := ParseSimulation(out)
	var total int64
	if len(pkgs) > 0 {
		uris, err := j.Run(ctx, "apt-get", apt("--print-uris", "-qq", "upgrade", "--with-new-pkgs")...)
		if err != nil {
			return fmt.Errorf("apt-get --print-uris: %w", err)
		}
		sizes := ParsePrintURIs(uris)
		for i := range pkgs {
			pkgs[i].Size = sizes[pkgs[i].Name]
			total += pkgs[i].Size
		}
	}
	now := j.Now()
	st.Packages, st.KeptBack, st.DownloadBytes, st.ListedAt = pkgs, kept, total, &now
	return nil
}

// upgrade installs everything the last check listed. It is not cancellable:
// systemd stops the unit with SIGTERM to this process only (KillMode=mixed),
// dpkg keeps running until it is done.
func (j *Job) upgrade() error {
	st, err := Load(j.Dir)
	if err != nil {
		return err
	}
	up := &Upgrade{StartedAt: j.Now(), Result: ResultRunning, Total: len(st.Packages)}
	st.Upgrade = up
	if err := Save(j.Dir, st); err != nil {
		return err
	}
	var tail []string
	keep := func(l string) {
		tail = append(tail, l)
		if len(tail) > 20 {
			tail = tail[1:]
		}
	}
	// Finish an earlier interrupted run first (no-op on a clean system).
	runErr := j.Stream(keep, "dpkg", "--configure", "-a")
	last := time.Time{}
	if runErr == nil {
		runErr = j.Stream(func(l string) {
			keep(l)
			name, ver, ok := SetupLine(l)
			if !ok {
				return
			}
			up.Done++
			up.Packages = append(up.Packages, name+" "+ver)
			if j.Now().Sub(last) >= 2*time.Second {
				last = j.Now()
				// Progress only: a failed write shows an older count, the final Save reports errors.
				_ = Save(j.Dir, st)
			}
		}, "apt-get", apt("-y", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold",
			"upgrade", "--with-new-pkgs")...)
	}
	now := j.Now()
	up.FinishedAt = &now
	if runErr != nil {
		up.Result, up.Message = ResultFailed, LastLines([]byte(strings.Join(tail, "\n")), 4)
		if up.Message == "" {
			up.Message = runErr.Error()
		}
	} else {
		up.Result = ResultOK
		// Downloaded .debs are not needed any more (~size of the update on the system disk).
		if _, err := j.Run(context.Background(), "apt-get", apt("clean")...); err != nil {
			j.Log("upgrade: apt-get clean failed: %v", err)
		}
	}
	if boot, err := j.BootTime(); err == nil {
		if st.RestartSince == nil || !st.RestartSince.Equal(boot) {
			st.RestartPackages = []string{}
		}
		st.RestartSince = &boot
		st.RestartPackages = mergeNames(st.RestartPackages, NeedsRestart(up.Packages))
	}
	if err := j.refresh(context.Background(), &st); err != nil {
		j.Log("upgrade: refresh failed: %v", err)
	}
	if err := Save(j.Dir, st); err != nil {
		return err
	}
	j.Log("upgrade: %s, %d of %d packages set up", up.Result, up.Done, up.Total)
	return runErr
}

func mergeNames(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range append(append([]string{}, a...), b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
