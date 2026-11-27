package helper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Restore of a folder or file from a backup version into a shared folder
// (W4.3 phase 2b, BKP-11). The copy runs in the helper in the background,
// one at a time, into a hidden folder next to the target (the File Station
// hides and refuses the upload prefix), and gets its name only when it is
// complete, so a half-restored folder never looks finished and nothing that
// exists is ever overwritten. The target is opened with the caller's File
// Station access (rw required) and every new entry belongs to the caller and
// group nas-users, like an upload. Symlinks and special files of the backup
// are skipped: a link restored into a share could point anywhere.

// CodeRestoreRunning is the error of a second restore while one runs.
const CodeRestoreRunning = "restore_running"

const (
	// MaxRestoreEntries bounds the entries one restore takes.
	MaxRestoreEntries = 200000
	restoreChunk      = 16 << 20
)

// Restore states.
const (
	RestoreIdle      = "idle"
	RestoreScanning  = "scanning"
	RestoreCopying   = "copying"
	RestoreDone      = "done"
	RestoreFailed    = "failed"
	RestoreCancelled = "cancelled"
)

// RestoreRequest is the argument of backup.restore.start.
type RestoreRequest struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Path  string `json:"path"`
	Share string `json:"share"`
	Dir   string `json:"dir"`
	Name  string `json:"name"`
}

// RestoreStatus is the state of the current or last restore.
type RestoreStatus struct {
	State      string     `json:"state"`
	Job        string     `json:"job,omitempty"`
	Date       string     `json:"date,omitempty"`
	Path       string     `json:"path"`
	Share      string     `json:"share,omitempty"`
	Dest       string     `json:"dest,omitempty"`
	User       string     `json:"user,omitempty"`
	TotalFiles int64      `json:"totalFiles"`
	TotalBytes int64      `json:"totalBytes"`
	Files      int64      `json:"files"`
	Bytes      int64      `json:"bytes"`
	Skipped    int64      `json:"skipped"`
	Message    string     `json:"message,omitempty"`
	Started    *time.Time `json:"started,omitempty"`
	Finished   *time.Time `json:"finished,omitempty"`
}

// Running reports whether the restore is still working.
func (s RestoreStatus) Running() bool {
	return s.State == RestoreScanning || s.State == RestoreCopying
}

// RestoreManager runs the restores.
type RestoreManager struct {
	Backup *BackupManager
	Files  *FileManager
	// Lower lowers the CPU and I/O priority of the calling thread; nil in tests.
	Lower func()

	mu     sync.Mutex
	st     RestoreStatus
	cancel context.CancelFunc
	done   chan struct{}
}

// Status returns the current or last restore.
func (m *RestoreManager) Status() RestoreStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st.State == "" {
		return RestoreStatus{State: RestoreIdle}
	}
	return m.st
}

func (m *RestoreManager) update(f func(*RestoreStatus)) {
	m.mu.Lock()
	f(&m.st)
	m.mu.Unlock()
}

// Cancel stops a running restore; the partial copy is removed.
func (m *RestoreManager) Cancel() RestoreStatus {
	m.mu.Lock()
	if m.st.Running() && m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	return m.Status()
}

// Wait blocks until the running restore has finished (tests, shutdown).
func (m *RestoreManager) Wait() {
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}

// restoreJob is one restore with its open roots.
type restoreJob struct {
	m      *RestoreManager
	req    RestoreRequest
	user   string
	src    *os.Root
	isDir  bool
	target *fileCtx
	dir    *os.File
	tmp    string // hidden name in dir
}

func (j *restoreJob) close() {
	j.src.Close()
	j.dir.Close()
	j.target.close()
}

// Start checks the request and starts the copy in the background.
func (m *RestoreManager) Start(ctx context.Context, req RestoreRequest, user string) (RestoreStatus, error) {
	if !ValidVersionPath(req.Path) {
		return RestoreStatus{}, invalid("path", "Neplatná cesta v záloze.")
	}
	if err := checkPath(req.Dir); err != nil {
		return RestoreStatus{}, invalid("dir", "Neplatná cílová složka.")
	}
	if err := checkName(req.Name); err != nil {
		return RestoreStatus{}, err
	}
	m.mu.Lock()
	running := m.st.Running()
	m.mu.Unlock()
	if running {
		return RestoreStatus{}, &Error{Code: CodeRestoreRunning, Message: "Jedna obnova už běží; počkejte na její konec nebo ji zrušte."}
	}
	j, err := m.prepare(req, user)
	if err != nil {
		return RestoreStatus{}, err
	}
	m.mu.Lock()
	if m.st.Running() {
		m.mu.Unlock()
		j.close()
		return RestoreStatus{}, &Error{Code: CodeRestoreRunning, Message: "Jedna obnova už běží; počkejte na její konec nebo ji zrušte."}
	}
	now := time.Now().UTC()
	m.st = RestoreStatus{State: RestoreScanning, Job: req.ID, Date: req.Date, Path: req.Path, Share: req.Share,
		Dest: join(req.Dir, req.Name), User: user, Started: &now}
	jctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.done = cancel, make(chan struct{})
	done := m.done
	st := m.st
	m.mu.Unlock()
	slog.Info("helper: backup.restore start", "job", req.ID, "date", req.Date, "share", req.Share, "user", user)
	go func() {
		defer close(done)
		defer cancel()
		if m.Lower != nil {
			// The goroutine keeps its thread and the thread ends with it,
			// so the lowered priority never leaks to other work.
			runtime.LockOSThread()
			m.Lower()
		}
		j.run(jctx)
	}()
	return st, nil
}

// prepare opens the version and the target and checks both.
func (m *RestoreManager) prepare(req RestoreRequest, user string) (*restoreJob, error) {
	src, err := m.Backup.openVersion(req.ID, req.Date)
	if err != nil {
		return nil, err
	}
	name := req.Path
	if name == "" {
		name = "."
	}
	st, err := src.Lstat(name)
	if err != nil {
		src.Close()
		return nil, notFound("Položka v záloze neexistuje.")
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		src.Close()
		return nil, invalid("path", "Obnovit lze složku nebo běžný soubor.")
	}
	target, err := m.Files.open(user, req.Share, true)
	if err != nil {
		src.Close()
		return nil, err
	}
	d, err := openDir(target, req.Dir)
	if err != nil {
		src.Close()
		target.close()
		return nil, err
	}
	j := &restoreJob{m: m, req: req, user: user, src: src, isDir: st.IsDir(), target: target, dir: d}
	if _, err := target.root.Lstat(join(req.Dir, req.Name)); err == nil {
		j.close()
		return nil, existsErr("Položka s tímto názvem už v cílové složce existuje.")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		j.close()
		return nil, internalErr("Interní chyba.")
	}
	j.tmp = uploadPrefix + hex.EncodeToString(b)
	return j, nil
}

// srcName is the path of the restored item below the version root.
func (j *restoreJob) srcName() string {
	if j.req.Path == "" {
		return "."
	}
	return j.req.Path
}

func (j *restoreJob) run(ctx context.Context) {
	defer j.close()
	err := j.copy(ctx)
	state, msg := RestoreDone, ""
	switch {
	case err == nil:
	case ctx.Err() != nil:
		state, msg = RestoreCancelled, "Obnova byla zrušena; rozpracovaná kopie je smazaná."
	default:
		state, msg = RestoreFailed, err.Error()
		var he *Error
		if errors.As(err, &he) {
			msg = he.Message
		}
	}
	if err != nil {
		j.target.root.RemoveAll(join(j.req.Dir, j.tmp))
	}
	now := time.Now().UTC()
	j.m.update(func(s *RestoreStatus) { s.State, s.Message, s.Finished = state, msg, &now })
	st := j.m.Status()
	slog.Info("helper: backup.restore end", "job", j.req.ID, "date", j.req.Date, "share", j.req.Share, "user", j.user,
		"state", state, "files", st.Files, "bytes", st.Bytes, "skipped", st.Skipped)
}

// copy scans the item, checks the free space, copies it under the hidden
// name and renames it into place.
func (j *restoreJob) copy(ctx context.Context) error {
	files, bytes, err := j.scan(ctx)
	if err != nil {
		return err
	}
	j.m.update(func(s *RestoreStatus) { s.State, s.TotalFiles, s.TotalBytes = RestoreCopying, files, bytes })
	if free, err := j.m.Files.Statfs(j.dir); err == nil && uint64(bytes)+uploadReserve > free {
		return invalid("share", "Na cílovém svazku není dost místa.")
	}
	tmpPath := join(j.req.Dir, j.tmp)
	if j.isDir {
		if err := j.copyDir(ctx, tmpPath); err != nil {
			return err
		}
	} else if err := j.copyFile(ctx, j.srcName(), j.target.root, tmpPath); err != nil {
		return err
	}
	if err := unix.Syncfs(int(j.dir.Fd())); err != nil {
		return internalErr("Obnovená data nelze zapsat na disk.")
	}
	// Access may have been revoked during a long copy.
	c, err := j.m.Files.open(j.user, j.req.Share, true)
	if err != nil {
		return err
	}
	c.close()
	if err := renameNoReplace(j.dir, j.tmp, j.dir, j.req.Name); err != nil {
		return renameErr(err)
	}
	return nil
}

// scan counts the regular files and their bytes, bounded by MaxRestoreEntries.
func (j *restoreJob) scan(ctx context.Context) (files, bytes int64, err error) {
	n := 0
	err = fs.WalkDir(j.src.FS(), j.srcName(), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return internalErr("Zálohu nelze přečíst.")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if n++; n > MaxRestoreEntries {
			return invalid("path", "Obnova bere nejvýš 200 000 položek; vyberte menší složku.")
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return internalErr("Zálohu nelze přečíst.")
			}
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes, err
}

// copyDir recreates the folder tree below tmpPath of the target share.
func (j *restoreJob) copyDir(ctx context.Context, tmpPath string) error {
	if err := j.target.root.Mkdir(tmpPath, 0o770); err != nil {
		return internalErr("Cílovou složku nelze vytvořit.")
	}
	if err := j.ownPath(j.target.root, tmpPath, true); err != nil {
		return err
	}
	dst, err := j.target.root.OpenRoot(tmpPath)
	if err != nil {
		return internalErr("Cílovou složku nelze otevřít.")
	}
	defer dst.Close()
	type dirTime struct {
		rel   string
		mtime time.Time
	}
	var dirs []dirTime
	base := j.srcName()
	err = fs.WalkDir(j.src.FS(), base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return internalErr("Zálohu nelze přečíst.")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel := relTo(base, p)
		switch {
		case d.IsDir():
			if rel != "." {
				if err := dst.Mkdir(rel, 0o770); err != nil {
					return internalErr("Složku nelze vytvořit.")
				}
				if err := j.ownPath(dst, rel, true); err != nil {
					return err
				}
			}
			if info, err := d.Info(); err == nil {
				dirs = append(dirs, dirTime{rel, info.ModTime()})
			}
		case d.Type().IsRegular():
			return j.copyFile(ctx, p, dst, rel)
		default:
			j.m.update(func(s *RestoreStatus) { s.Skipped++ })
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Children change the folder times, so they are set last, deepest first.
	for i := len(dirs) - 1; i >= 0; i-- {
		dst.Chtimes(dirs[i].rel, dirs[i].mtime, dirs[i].mtime)
	}
	return nil
}

// relTo is p relative to base ("." for base itself); WalkDir paths start with base.
func relTo(base, p string) string {
	if base == "." {
		return p
	}
	if p == base {
		return "."
	}
	return strings.TrimPrefix(p, base+"/")
}

// ownPath gives an entry just created below r the caller's owner and modes.
func (j *restoreJob) ownPath(r *os.Root, p string, dir bool) error {
	flags := os.O_RDONLY | syscall.O_NOFOLLOW
	if dir {
		flags |= syscall.O_DIRECTORY
	}
	f, err := r.OpenFile(p, flags, 0)
	if err == nil {
		err = j.m.Files.own(j.target, f, dir)
		f.Close()
	}
	if err != nil {
		return internalErr("Obnovené položce nelze nastavit vlastníka.")
	}
	return nil
}

// copyFile copies the regular file p of the version to rel below dst,
// keeping its modification time. Between chunks it checks for a cancel.
func (j *restoreJob) copyFile(ctx context.Context, p string, dst *os.Root, rel string) error {
	fr, err := openRegular(j.src, p)
	if err != nil {
		// Replaced by something else since the scan.
		j.m.update(func(s *RestoreStatus) { s.Skipped++ })
		return nil
	}
	in := fr.f
	defer in.Close()
	out, err := dst.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o660)
	if err != nil {
		return internalErr("Soubor nelze vytvořit: " + path.Base(rel))
	}
	if err := j.m.Files.own(j.target, out, false); err != nil {
		out.Close()
		return internalErr("Obnovené položce nelze nastavit vlastníka.")
	}
	for {
		if ctx.Err() != nil {
			out.Close()
			return ctx.Err()
		}
		// io.CopyN between two files uses copy_file_range: no copy through user space.
		n, err := io.CopyN(out, in, restoreChunk)
		j.m.update(func(s *RestoreStatus) { s.Bytes += n })
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			out.Close()
			if errors.Is(err, syscall.ENOSPC) {
				return invalid("share", "Na cílovém svazku došlo místo.")
			}
			return internalErr("Soubor nelze zkopírovat: " + path.Base(rel))
		}
	}
	if err := out.Close(); err != nil {
		return internalErr("Soubor nelze zapsat: " + path.Base(rel))
	}
	mtime := fr.meta.(BackupFileMeta).Modified
	dst.Chtimes(rel, mtime, mtime)
	j.m.update(func(s *RestoreStatus) { s.Files++ })
	return nil
}

// SysLowerPriority gives the calling thread nice 10 and the lowest
// best-effort I/O priority, so a restore yields to SMB, NFS and the web.
func SysLowerPriority() {
	unix.Setpriority(unix.PRIO_PROCESS, 0, 10)
	const ioprioWhoProcess, ioprioClassBE, ioprioShift = 1, 2, 13
	unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, 0, uintptr(ioprioClassBE<<ioprioShift|7))
}
