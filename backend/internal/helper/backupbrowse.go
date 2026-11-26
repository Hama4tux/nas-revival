package helper

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"nas-revival/backend/internal/tasks"
)

// Browsing the backup versions (W4.3 phase 2, BKP-11). The versions belong to
// nas-backup (0700); the web process sees them only through these ops. Every
// lookup goes through os.Root so a symlink stored in a backup can never lead
// outside versions/<date>/<id>, and a download refuses symlinks entirely.

// Limits of the browse ops.
const (
	// MaxVersionWalk bounds the entries walked by backup.versions over all versions.
	MaxVersionWalk = 200000
	// MaxBrowseRead is how many entries of one directory are read at most.
	MaxBrowseRead = 5000
	// BrowsePage is the page size of backup.browse.
	BrowsePage    = 200
	maxBrowsePath = 1024
)

var versionDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// BackupVersion is one daily version of a job.
type BackupVersion struct {
	Date  string `json:"date"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
	// AddedBytes is the size of the files that are not hardlinks of the
	// previous (older) version, i.e. what the version costs on the disk.
	AddedBytes int64 `json:"addedBytes"`
}

// BackupVersions is the result of backup.versions, newest first.
type BackupVersions struct {
	Versions []BackupVersion `json:"versions"`
	// Complete is false when the walk stopped at MaxVersionWalk; the sizes
	// of the older versions are then missing (zero).
	Complete bool `json:"complete"`
}

// BrowseEntry is one entry of a directory in a version.
type BrowseEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"` // dir | file | link | other
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// BadName marks a name that is not valid UTF-8; it is shown with
	// replacement characters and cannot be opened from the web UI.
	BadName bool `json:"badName,omitempty"`
}

// BrowseResult is the result of backup.browse.
type BrowseResult struct {
	Path    string        `json:"path"`
	Entries []BrowseEntry `json:"entries"`
	Offset  int           `json:"offset"`
	Total   int           `json:"total"`
	// Truncated is true when the directory has more than MaxBrowseRead entries.
	Truncated bool `json:"truncated"`
}

// BackupFileMeta describes a file passed to the web process as a descriptor.
type BackupFileMeta struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// fileResult is a reply that carries an open file next to the JSON result.
type fileResult struct {
	meta any
	f    *os.File
}

// ValidVersionPath checks a path inside a version: relative, clean, UTF-8,
// no "." or ".." elements, no NUL or control characters. "" is the root.
func ValidVersionPath(p string) bool {
	if p == "" {
		return true
	}
	if len(p) > maxBrowsePath || !utf8.ValidString(p) || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (m *BackupManager) volume() string {
	if m.Volume == "" {
		return tasks.BackupVolume
	}
	return m.Volume
}

// realDir reports whether p is a directory and not a symlink.
func realDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

// versionDates lists the dates that hold a version of id, newest first.
func (m *BackupManager) versionDates(id string) ([]string, error) {
	vroot := filepath.Join(m.volume(), "versions")
	entries, err := os.ReadDir(vroot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, internalErr("Verze záloh nelze přečíst.")
	}
	var dates []string
	for _, e := range entries {
		if e.IsDir() && versionDateRe.MatchString(e.Name()) && realDir(filepath.Join(vroot, e.Name(), id)) {
			dates = append(dates, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

func checkJob(id string) error {
	if !tasks.ValidID(id) {
		return invalid("id", "Neplatné ID úlohy.")
	}
	return nil
}

// Versions lists the versions of a job with their sizes. Unchanged files are
// hardlinks of the previous version, so AddedBytes counts only inodes that
// the next older version does not have.
func (m *BackupManager) Versions(ctx context.Context, id string) (BackupVersions, error) {
	if err := checkJob(id); err != nil {
		return BackupVersions{}, err
	}
	dates, err := m.versionDates(id)
	if err != nil {
		return BackupVersions{}, err
	}
	res := BackupVersions{Versions: make([]BackupVersion, len(dates)), Complete: true}
	type inode struct{ dev, ino uint64 }
	var prev map[inode]bool
	budget := MaxVersionWalk
	// Oldest first, so each version is compared with the one before it.
	for i := len(dates) - 1; i >= 0; i-- {
		v := BackupVersion{Date: dates[i]}
		cur := map[inode]bool{}
		root := filepath.Join(m.volume(), "versions", dates[i], id)
		walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if budget--; budget < 0 {
				return fs.SkipAll
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			v.Files++
			v.Bytes += info.Size()
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				k := inode{uint64(st.Dev), uint64(st.Ino)}
				cur[k] = true
				if !prev[k] {
					v.AddedBytes += info.Size()
				}
			}
			return nil
		})
		if walkErr != nil {
			return BackupVersions{}, internalErr("Verze záloh nelze přečíst.")
		}
		if budget < 0 {
			// The newer versions stay zero; mark the list incomplete.
			res.Complete = false
			for j := i; j >= 0; j-- {
				res.Versions[j] = BackupVersion{Date: dates[j]}
			}
			break
		}
		res.Versions[i] = v
		prev = cur
	}
	return res, nil
}

// openVersion opens versions/<date>/<id> as a root after checking that no
// element of it is a symlink.
func (m *BackupManager) openVersion(id, date string) (*os.Root, error) {
	if err := checkJob(id); err != nil {
		return nil, err
	}
	if !versionDateRe.MatchString(date) {
		return nil, invalid("date", "Neplatné datum verze.")
	}
	vroot := filepath.Join(m.volume(), "versions")
	for _, p := range []string{vroot, filepath.Join(vroot, date), filepath.Join(vroot, date, id)} {
		if !realDir(p) {
			return nil, notFound("Verze zálohy neexistuje.")
		}
	}
	r, err := os.OpenRoot(filepath.Join(vroot, date, id))
	if err != nil {
		return nil, notFound("Verze zálohy neexistuje.")
	}
	return r, nil
}

func entryType(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "dir"
	case mode&fs.ModeSymlink != 0:
		return "link"
	case mode.IsRegular():
		return "file"
	}
	return "other"
}

// Browse lists one directory of a version: directories first, then by name,
// BrowsePage entries from offset.
func (m *BackupManager) Browse(ctx context.Context, id, date, p string, offset int) (BrowseResult, error) {
	if !ValidVersionPath(p) {
		return BrowseResult{}, invalid("path", "Neplatná cesta.")
	}
	if offset < 0 {
		return BrowseResult{}, invalid("offset", "Neplatný posun.")
	}
	r, err := m.openVersion(id, date)
	if err != nil {
		return BrowseResult{}, err
	}
	defer r.Close()
	return listDir(ctx, r, p, offset, nil)
}

// listDir reads one directory below r: directories first, then by name,
// BrowsePage entries from offset. hide drops names from the listing.
func listDir(ctx context.Context, r *os.Root, p string, offset int, hide func(name string) bool) (BrowseResult, error) {
	name := p
	if name == "" {
		name = "."
	}
	d, err := r.Open(name)
	if err != nil {
		return BrowseResult{}, notFound("Složka neexistuje.")
	}
	defer d.Close()
	if st, err := d.Stat(); err != nil || !st.IsDir() {
		return BrowseResult{}, invalid("path", "Cesta není složka.")
	}
	des, err := d.ReadDir(MaxBrowseRead + 1)
	if err != nil && len(des) == 0 && !errors.Is(err, io.EOF) {
		return BrowseResult{}, internalErr("Složku nelze přečíst.")
	}
	res := BrowseResult{Path: p, Offset: offset, Entries: []BrowseEntry{}}
	if len(des) > MaxBrowseRead {
		des, res.Truncated = des[:MaxBrowseRead], true
	}
	all := make([]BrowseEntry, 0, len(des))
	for _, de := range des {
		if ctx.Err() != nil {
			return BrowseResult{}, ctx.Err()
		}
		if hide != nil && hide(de.Name()) {
			continue
		}
		e := BrowseEntry{Name: de.Name(), Type: entryType(de.Type())}
		if !utf8.ValidString(e.Name) {
			e.Name, e.BadName = strings.ToValidUTF8(e.Name, "�"), true
		}
		if info, err := de.Info(); err == nil {
			e.Modified = info.ModTime().UTC()
			if e.Type == "file" {
				e.Size = info.Size()
			}
		}
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool {
		if (all[i].Type == "dir") != (all[j].Type == "dir") {
			return all[i].Type == "dir"
		}
		return all[i].Name < all[j].Name
	})
	res.Total = len(all)
	if offset < len(all) {
		end := min(offset+BrowsePage, len(all))
		res.Entries = all[offset:end]
	}
	return res, nil
}

// Open opens a regular file of a version for reading; the descriptor goes
// to the web process, which streams it. Symlinks are refused.
func (m *BackupManager) Open(ctx context.Context, id, date, p string) (*fileResult, error) {
	if p == "" || !ValidVersionPath(p) {
		return nil, invalid("path", "Neplatná cesta.")
	}
	r, err := m.openVersion(id, date)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return openRegular(r, p)
}

// openRegular opens a regular file below r read-only, refusing a symlink as
// the last element.
func openRegular(r *os.Root, p string) (*fileResult, error) {
	f, err := r.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, notFound("Soubor neexistuje.")
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, invalid("path", "Stáhnout lze jen běžný soubor.")
	}
	return &fileResult{meta: BackupFileMeta{Name: path.Base(p), Size: st.Size(), Modified: st.ModTime().UTC()}, f: f}, nil
}
