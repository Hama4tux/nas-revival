package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nas-revival/backend/internal/shares"
)

// Fixed locations of the backup pull jobs (W4.3, ADR 0008).
const (
	BackupUser       = "nas-backup"
	BackupVolume     = "/srv/data"
	BackupKeyPath    = "/var/lib/nas-backup/.ssh/id_ed25519"
	BackupKnownHosts = "/etc/nas-revival/backup_known_hosts"
	BackupHome       = "/var/lib/nas-backup"
)

// Messages of the backup runner (Czech, no secrets).
const (
	MsgWrongAccount = "Úloha běží pod nesprávným účtem."
	MsgNoVolume     = "Datový svazek není připojen."
	MsgHostKey      = "Otisk klíče serveru nesouhlasí – ověřte a potvrďte ho znovu."
	MsgPartial      = "Část souborů se nepodařilo stáhnout."
	MsgNoDest       = "Úloha nemá nastavený cíl záloh."
	MsgBadDest      = "Cíl záloh není k dispozici (složka neexistuje, není na datovém svazku nebo nemá volbu Cíl záloh)."
)

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ExecFunc runs a fixed command (never through a shell) and returns stdout,
// stderr (both capped) and the exit code (-1 when it did not start).
type ExecFunc func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, code int, err error)

type capWriter struct {
	buf bytes.Buffer
	max int
}

func (c *capWriter) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// SysExec is the production ExecFunc. The context kill stops rsync on SIGTERM.
func SysExec(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "HOME=" + BackupHome}
	cmd.WaitDelay = 5 * time.Second
	so, se := &capWriter{max: 64 << 10}, &capWriter{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = so, se
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return so.buf.Bytes(), se.buf.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return so.buf.Bytes(), se.buf.Bytes(), -1, err
	}
	return so.buf.Bytes(), se.buf.Bytes(), 0, nil
}

// SSHOptions are the ssh options of the pull (the same for rsync -e, the
// connection test and the key scan check).
func SSHOptions(key string, port int, knownHosts string) []string {
	return []string{"-i", key, "-p", strconv.Itoa(port), "-o", "BatchMode=yes", "-o", "ConnectTimeout=20",
		"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + knownHosts}
}

// SSHCommand is the "-e" argument of rsync.
func SSHCommand(key string, port int, knownHosts string) string {
	return "ssh " + strings.Join(SSHOptions(key, port, knownHosts), " ")
}

// RemoteSpec builds user@host:path/ (IPv6 literals in brackets).
func RemoteSpec(user, host, path string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return user + "@" + host + ":" + path + "/"
}

// BackupWorker does the pull, the snapshot and the retention.
type BackupWorker struct {
	Volume     string
	SharesPath string // shares.json: the destination share is resolved there
	KeyPath    string
	KnownHosts string
	Exec       ExecFunc
	// VolumeMounted reports whether Volume is a mounted data volume.
	VolumeMounted func() bool
	Now           func() time.Time
}

// NewBackupWorker returns a worker with the production defaults.
func NewBackupWorker(volumeMounted func() bool) *BackupWorker {
	return &BackupWorker{Volume: BackupVolume, SharesPath: shares.DefaultConfigPath, KeyPath: BackupKeyPath, KnownHosts: BackupKnownHosts,
		Exec: SysExec, VolumeMounted: volumeMounted, Now: time.Now}
}

// BackupOutcome is what a pull run reports.
type BackupOutcome struct {
	Result, Message  string
	Bytes, UsedBytes int64
}

// within joins validated elements to a job directory and checks that the
// result stays below it.
func within(jobDir string, elems ...string) (string, error) {
	p := filepath.Join(append([]string{jobDir}, elems...)...)
	rel, err := filepath.Rel(jobDir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the job directory")
	}
	return p, nil
}

// ensureDir creates a directory below the job directory or accepts an
// existing real one (never a symlink).
func ensureDir(p string) error {
	if err := os.Mkdir(p, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("not a real directory: " + p)
	}
	return nil
}

// Run pulls every path, checks freshness, snapshots and prunes.
func (w *BackupWorker) Run(ctx context.Context, t Task) BackupOutcome {
	b := t.Backup
	if b == nil || !ValidID(t.ID) {
		return BackupOutcome{Result: ResultFailed, Message: MsgFailed}
	}
	if w.VolumeMounted == nil || !w.VolumeMounted() {
		return BackupOutcome{Result: ResultFailed, Message: MsgNoVolume}
	}
	fail := func(m string) BackupOutcome { return BackupOutcome{Result: ResultFailed, Message: m} }
	jobDir, err := ResolveJobDir(w.Volume, w.SharesPath, t)
	if err != nil {
		slog.Error("backup: destination", "task", t.ID, "err", err)
		return fail(DestMessage(err))
	}
	if err := w.prepareJob(jobDir); err != nil {
		slog.Error("backup: mkdir", "err", err)
		return fail(MsgFailed)
	}
	var (
		total int64
		first string
	)
	note := func(m string) {
		if first == "" {
			first = m
		}
	}
	for _, p := range b.Paths {
		dest, err := within(jobDir, "current", p)
		if err != nil {
			note(MsgFailed)
			continue
		}
		if err := os.MkdirAll(dest, 0o750); err != nil {
			slog.Error("backup: mkdir dest", "err", err)
			note(MsgFailed)
			continue
		}
		args := []string{"-rlt", "--delete", "--chmod=D750,F640", "--timeout=600", "--stats"}
		if b.BwlimitKiB > 0 {
			args = append(args, "--bwlimit="+strconv.Itoa(b.BwlimitKiB))
		}
		for _, e := range b.Excludes {
			args = append(args, "--exclude="+e)
		}
		args = append(args, "-e", SSHCommand(w.KeyPath, b.Port, w.KnownHosts), "--",
			RemoteSpec(b.User, b.Host, p), dest+"/")
		out, errOut, code, err := w.Exec(ctx, "rsync", args...)
		if ctx.Err() != nil {
			return BackupOutcome{Result: ResultStopped, Message: MsgStopped}
		}
		total += ParseTransferred(string(out))
		switch {
		case err != nil:
			slog.Error("backup: rsync not started", "err", err)
			note(MsgFailed)
		case code == 0:
		case code == 23 || code == 24:
			note(MsgPartial)
		case code == 255 || hostKeyMismatch(string(errOut)):
			note(MsgHostKey)
		default:
			note(fmt.Sprintf("Stažení selhalo (rsync %d).", code))
		}
	}
	if b.Fresh != nil && first == "" {
		if m := w.checkFresh(jobDir, b.Fresh); m != "" {
			note(m)
		}
	}
	day := w.Now().Format("2006-01-02")
	versions, err := w.snapshot(jobDir, day, b.KeepDays)
	if err != nil {
		slog.Error("backup: snapshot", "err", err)
		note("Verzi se nepodařilo uložit.")
	}
	used := w.usedBytes(jobDir)
	if first != "" {
		return BackupOutcome{Result: ResultFailed, Message: first, Bytes: total, UsedBytes: used}
	}
	return BackupOutcome{Result: ResultOK, Bytes: total, UsedBytes: used,
		Message: fmt.Sprintf("Staženo %s MB, verzí %d.", mbText(total), versions)}
}

// prepareJob creates <jobDir>/current and <jobDir>/versions (the job
// directory itself and its parents below the share folder as needed). The
// directories inherit the share's default ACL, so the share's readers see them.
func (w *BackupWorker) prepareJob(jobDir string) error {
	rel, err := filepath.Rel(w.Volume, jobDir)
	if err != nil {
		return err
	}
	p := w.Volume
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, seg)
		if err := ensureDir(p); err != nil {
			return err
		}
	}
	for _, d := range []string{"current", "versions"} {
		if err := ensureDir(filepath.Join(jobDir, d)); err != nil {
			return err
		}
	}
	return nil
}

func mbText(n int64) string {
	return strings.Replace(fmt.Sprintf("%.1f", float64(n)/(1<<20)), ".", ",", 1)
}

func hostKeyMismatch(s string) bool {
	return strings.Contains(s, "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(s, "Host key verification failed")
}

// ParseTransferred reads "Total transferred file size: 1,234 bytes" from
// rsync --stats (0 when absent).
func ParseTransferred(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Total transferred file size:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			return 0
		}
		n, err := strconv.ParseInt(strings.NewReplacer(",", "", ".", "").Replace(f[0]), 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return 0
}

// checkFresh returns a failure message when the marker file is missing or
// older than the limit.
func (w *BackupWorker) checkFresh(jobDir string, f *FreshParams) string {
	msg := fmt.Sprintf("Export na serveru je starší než %d h.", f.MaxAgeHours)
	p, err := within(jobDir, "current", f.File)
	if err != nil {
		return MsgFailed
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return msg
	}
	if w.Now().Sub(st.ModTime()) > time.Duration(f.MaxAgeHours)*time.Hour {
		return msg
	}
	return ""
}

// snapshot hardlinks <jobDir>/current to <jobDir>/versions/<day> and prunes
// the old versions; it returns how many versions remain.
func (w *BackupWorker) snapshot(jobDir, day string, keep int) (int, error) {
	if !dateRe.MatchString(day) {
		return 0, errors.New("bad date")
	}
	src := filepath.Join(jobDir, "current")
	vroot := filepath.Join(jobDir, "versions")
	tmp := filepath.Join(vroot, ".tmp-"+day)
	final := filepath.Join(vroot, day)
	if err := ensureDir(vroot); err != nil {
		return 0, err
	}
	if err := os.RemoveAll(tmp); err != nil {
		return 0, err
	}
	if err := linkTree(src, tmp); err != nil {
		os.RemoveAll(tmp)
		return 0, err
	}
	if err := os.RemoveAll(final); err != nil {
		os.RemoveAll(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.RemoveAll(tmp)
		return 0, err
	}
	return w.prune(jobDir, keep)
}

// linkTree recreates src below dst: directories 0750, files as hardlinks,
// symlinks as symlinks (never followed).
func linkTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.Mkdir(target, 0o750)
		case d.Type()&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, target)
		case d.Type().IsRegular():
			return os.Link(p, target)
		}
		return nil
	})
}

// prune keeps the newest keep dated versions of the job.
func (w *BackupWorker) prune(jobDir string, keep int) (int, error) {
	vroot := filepath.Join(jobDir, "versions")
	entries, err := os.ReadDir(vroot)
	if err != nil {
		return 0, err
	}
	var dates []string
	for _, e := range entries {
		if e.IsDir() && dateRe.MatchString(e.Name()) {
			dates = append(dates, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) <= keep {
		return len(dates), nil
	}
	var first error
	for _, d := range dates[keep:] {
		if err := os.RemoveAll(filepath.Join(vroot, d)); err != nil && first == nil {
			first = err
		}
	}
	return keep, first
}

// usedBytes sums the regular files of current and versions/*, each inode once.
func (w *BackupWorker) usedBytes(jobDir string) int64 {
	type key struct{ dev, ino uint64 }
	seen := map[key]bool{}
	var sum int64
	walk := func(root string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				k := key{uint64(st.Dev), uint64(st.Ino)}
				if seen[k] {
					return nil
				}
				seen[k] = true
			}
			sum += info.Size()
			return nil
		})
	}
	walk(filepath.Join(jobDir, "current"))
	if entries, err := os.ReadDir(filepath.Join(jobDir, "versions")); err == nil {
		for _, e := range entries {
			if e.IsDir() && dateRe.MatchString(e.Name()) {
				walk(filepath.Join(jobDir, "versions", e.Name()))
			}
		}
	}
	return sum
}
