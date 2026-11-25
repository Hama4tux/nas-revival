// Package config keeps the bounded history of the files the helper manages
// (ADR 0005, docs/ARCHITECTURE.md §4): every version is a snapshot of all
// managed files, the newest one is the manifest for drift detection.
//
// Layout under Dir:
//
//	history/000042/entry.json   metadata and sha256 of every managed file
//	history/000042/<file id>    content of each file that existed
//
// An entry is written into a temporary directory and renamed into place, so a
// crash leaves either the whole entry or none.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Limits of the history (CFG-03).
const (
	DefaultMaxEntries = 20
	DefaultMaxBytes   = 2 << 20
	// MaxFileSize caps one managed file; the managed files are a few hundred bytes.
	MaxFileSize = 256 << 10
)

// Entry kinds.
const (
	KindBaseline = "baseline" // first snapshot, state before NAS Revival changed anything
	KindChange   = "change"   // saved from the web UI
	KindManual   = "manual"   // found changed outside the web UI before the next change
	KindRollback = "rollback" // return to an older version
)

// Drift states of a managed file.
const (
	StateOK       = "ok"
	StateModified = "modified" // differs from the newest version
	StatePending  = "pending"  // a change waits for confirmation (network)
)

var (
	ErrNotFound = errors.New("config: version not found")
	ErrDrift    = errors.New("config: files changed outside the web UI")
	ErrTooLarge = errors.New("config: managed file too large")
)

// File is one entry of the managed file registry.
type File struct {
	ID   string // stable key, e.g. "network"
	Path string
}

// FileState is a managed file in one version.
type FileState struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size"`
}

// Entry is one version of the managed files.
type Entry struct {
	ID         int         `json:"id"`
	Time       time.Time   `json:"time"`
	User       string      `json:"user"`
	Kind       string      `json:"kind"`
	Module     string      `json:"module,omitempty"`
	RollbackOf int         `json:"rollbackOf,omitempty"`
	Files      []FileState `json:"files"`
	// Changed lists the file IDs that differ from the previous version.
	Changed []string `json:"changed"`
}

// Drift is the state of one managed file against the newest version.
type Drift struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// Store is the history. All methods are safe for concurrent use.
type Store struct {
	Dir        string
	Files      []File
	Now        func() time.Time
	MaxEntries int
	MaxBytes   int64
	// Pending reports files with a change waiting for confirmation; their
	// last committed content is used instead of the file on disk.
	Pending func(id string) bool

	mu sync.Mutex
}

var entryDirRe = regexp.MustCompile(`^[0-9]{6}$`)

func (s *Store) historyDir() string { return filepath.Join(s.Dir, "history") }
func (s *Store) entryDir(id int) string {
	return filepath.Join(s.historyDir(), fmt.Sprintf("%06d", id))
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) pending(id string) bool { return s.Pending != nil && s.Pending(id) }

// Init creates the directories, removes half-written entries and records the
// baseline when the history is empty.
func (s *Store) Init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.historyDir(), 0o700); err != nil {
		return err
	}
	des, err := os.ReadDir(s.historyDir())
	if err != nil {
		return err
	}
	for _, de := range des {
		if !entryDirRe.MatchString(de.Name()) {
			os.RemoveAll(filepath.Join(s.historyDir(), de.Name()))
		}
	}
	ents, err := s.entries()
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return nil
	}
	_, _, err = s.recordLocked(nil, "system", KindBaseline, "", 0)
	return err
}

// readLimited reads a managed file; a missing file is (nil, false, nil).
func readLimited(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > MaxFileSize {
		return nil, false, fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	return b, true, nil
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// snapshot is the content of all managed files at one moment.
type snapshot struct {
	states []FileState
	data   map[string][]byte
}

// current reads all managed files. Pending files take their content from
// latest (the last committed version).
func (s *Store) current(latest *Entry) (snapshot, error) {
	snap := snapshot{data: map[string][]byte{}}
	for _, f := range s.Files {
		if s.pending(f.ID) && latest != nil {
			if st, ok := stateOf(latest, f.ID); ok {
				snap.states = append(snap.states, st)
				if st.Present {
					b, err := os.ReadFile(filepath.Join(s.entryDir(latest.ID), f.ID))
					if err != nil {
						return snap, err
					}
					snap.data[f.ID] = b
				}
				continue
			}
		}
		b, ok, err := readLimited(f.Path)
		if err != nil {
			return snap, err
		}
		st := FileState{ID: f.ID, Path: f.Path, Present: ok}
		if ok {
			st.SHA256, st.Size = hashOf(b), int64(len(b))
			snap.data[f.ID] = b
		}
		snap.states = append(snap.states, st)
	}
	return snap, nil
}

func stateOf(e *Entry, id string) (FileState, bool) {
	for _, st := range e.Files {
		if st.ID == id {
			return st, true
		}
	}
	return FileState{}, false
}

func sameState(a, b FileState) bool {
	return a.Present == b.Present && a.SHA256 == b.SHA256
}

// changedVs lists the managed files whose state in states differs from e
// (all of them when e is nil).
func (s *Store) changedVs(e *Entry, states []FileState) []string {
	out := []string{}
	for _, st := range states {
		if e == nil {
			if st.Present {
				out = append(out, st.ID)
			}
			continue
		}
		old, ok := stateOf(e, st.ID)
		if !ok || !sameState(old, st) {
			out = append(out, st.ID)
		}
	}
	return out
}

// entries loads all versions, oldest first; unreadable ones are skipped.
func (s *Store) entries() ([]Entry, error) {
	des, err := os.ReadDir(s.historyDir())
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		if !de.IsDir() || !entryDirRe.MatchString(de.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.historyDir(), de.Name(), "entry.json"))
		if err != nil {
			slog.Warn("config: entry unreadable", "entry", de.Name(), "err", err)
			continue
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil || strconv.Itoa(e.ID) != trimZeros(de.Name()) {
			slog.Warn("config: entry corrupt", "entry", de.Name())
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func trimZeros(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}

func latestOf(ents []Entry) *Entry {
	if len(ents) == 0 {
		return nil
	}
	return &ents[len(ents)-1]
}

// Prepare runs before the helper writes a managed file: changes made outside
// the web UI since the newest version are recorded first, so the next change
// never hides them.
func (s *Store) Prepare() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := s.entries()
	if err != nil {
		return err
	}
	_, _, err = s.recordLocked(latestOf(ents), "", KindManual, "", 0)
	return err
}

// Record stores the current state as a new version when it differs from the
// newest one. It reports whether an entry was written.
func (s *Store) Record(user, kind, module string, rollbackOf int) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := s.entries()
	if err != nil {
		return Entry{}, false, err
	}
	return s.recordLocked(latestOf(ents), user, kind, module, rollbackOf)
}

func (s *Store) recordLocked(latest *Entry, user, kind, module string, rollbackOf int) (Entry, bool, error) {
	snap, err := s.current(latest)
	if err != nil {
		return Entry{}, false, err
	}
	changed := s.changedVs(latest, snap.states)
	if latest != nil && len(changed) == 0 {
		return *latest, false, nil
	}
	id := 1
	if latest != nil {
		id = latest.ID + 1
	}
	e := Entry{ID: id, Time: s.now(), User: user, Kind: kind, Module: module, RollbackOf: rollbackOf,
		Files: snap.states, Changed: changed}
	if err := s.write(e, snap.data); err != nil {
		return Entry{}, false, err
	}
	s.evict()
	return e, true, nil
}

func writeSynced(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Store) write(e Entry, data map[string][]byte) error {
	tmp, err := os.MkdirTemp(s.historyDir(), ".tmp-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(tmp)
		}
	}()
	for id, b := range data {
		if err := writeSynced(filepath.Join(tmp, id), b); err != nil {
			return err
		}
	}
	meta, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(tmp, "entry.json"), meta); err != nil {
		return err
	}
	if err := syncDir(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.entryDir(e.ID)); err != nil {
		return err
	}
	ok = true
	return syncDir(s.historyDir())
}

func dirSize(dir string) int64 {
	var n int64
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		if fi, err := de.Info(); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// evict removes the oldest versions above the limits; the newest one always stays.
func (s *Store) evict() {
	maxN, maxB := s.limits()
	ents, err := s.entries()
	if err != nil {
		return
	}
	sizes := make([]int64, len(ents))
	var total int64
	for i, e := range ents {
		sizes[i] = dirSize(s.entryDir(e.ID))
		total += sizes[i]
	}
	for i := 0; i < len(ents)-1 && (len(ents)-i > maxN || total > maxB); i++ {
		if err := os.RemoveAll(s.entryDir(ents[i].ID)); err != nil {
			slog.Error("config: eviction failed", "entry", ents[i].ID, "err", err)
			return
		}
		total -= sizes[i]
	}
}

func (s *Store) limits() (int, int64) {
	n, b := s.MaxEntries, s.MaxBytes
	if n <= 0 {
		n = DefaultMaxEntries
	}
	if b <= 0 {
		b = DefaultMaxBytes
	}
	return n, b
}

// History is the result of List.
type History struct {
	Entries    []Entry `json:"entries"` // newest first
	Drift      []Drift `json:"drift"`
	MaxEntries int     `json:"maxEntries"`
	MaxBytes   int64   `json:"maxBytes"`
	UsedBytes  int64   `json:"usedBytes"`
}

// List returns the versions and the drift state of every managed file.
func (s *Store) List() (History, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := s.entries()
	if err != nil {
		return History{}, err
	}
	n, b := s.limits()
	h := History{Entries: []Entry{}, MaxEntries: n, MaxBytes: b}
	for i := len(ents) - 1; i >= 0; i-- {
		h.Entries = append(h.Entries, ents[i])
		h.UsedBytes += dirSize(s.entryDir(ents[i].ID))
	}
	h.Drift, err = s.drift(latestOf(ents))
	return h, err
}

func (s *Store) drift(latest *Entry) ([]Drift, error) {
	out := []Drift{}
	for _, f := range s.Files {
		d := Drift{ID: f.ID, Path: f.Path, State: StateOK}
		if s.pending(f.ID) {
			d.State = StatePending
		} else if latest != nil {
			b, ok, err := readLimited(f.Path)
			if err != nil {
				return nil, err
			}
			cur := FileState{Present: ok}
			if ok {
				cur.SHA256 = hashOf(b)
			}
			if old, found := stateOf(latest, f.ID); !found || !sameState(old, cur) {
				d.State = StateModified
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// FileDiff compares one managed file in a version with the file now.
type FileDiff struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Same    bool   `json:"same"`
	Present bool   `json:"present"`        // exists in the version
	Current bool   `json:"currentPresent"` // exists now
	// Lines turn the current file into the version ("-" now, "+" version).
	Lines    []DiffLine `json:"lines"`
	TooLarge bool       `json:"tooLarge"`
}

// Detail is the result of Get.
type Detail struct {
	Entry Entry      `json:"entry"`
	Files []FileDiff `json:"files"`
	// Drift: files changed outside the web UI since the newest version; a
	// rollback overwrites them only with takeover.
	Drift []string `json:"drift"`
}

func (s *Store) find(id int) (Entry, []Entry, error) {
	ents, err := s.entries()
	if err != nil {
		return Entry{}, nil, err
	}
	for _, e := range ents {
		if e.ID == id {
			return e, ents, nil
		}
	}
	return Entry{}, ents, ErrNotFound
}

func (s *Store) content(e Entry, id string) []byte {
	b, err := os.ReadFile(filepath.Join(s.entryDir(e.ID), id))
	if err != nil {
		return nil
	}
	return b
}

// Get returns one version with the differences to the current files.
func (s *Store) Get(id int) (Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ents, err := s.find(id)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Entry: e, Files: []FileDiff{}}
	if d.Drift, err = s.driftIDs(latestOf(ents)); err != nil {
		return Detail{}, err
	}
	for _, f := range s.Files {
		cur, curOK, err := readLimited(f.Path)
		if err != nil {
			return Detail{}, err
		}
		st, _ := stateOf(&e, f.ID)
		var old []byte
		if st.Present {
			old = s.content(e, f.ID)
		}
		fd := FileDiff{ID: f.ID, Path: f.Path, Present: st.Present, Current: curOK}
		fd.Same = st.Present == curOK && (!curOK || st.SHA256 == hashOf(cur))
		if !fd.Same {
			fd.Lines, fd.TooLarge = Lines(string(cur), string(old))
		}
		d.Files = append(d.Files, fd)
	}
	return d, nil
}

func (s *Store) driftIDs(latest *Entry) ([]string, error) {
	ds, err := s.drift(latest)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, d := range ds {
		if d.State == StateModified {
			out = append(out, d.ID)
		}
	}
	return out, nil
}

// Restore is one file a rollback writes back.
type Restore struct {
	ID      string
	Path    string
	Present bool
	Data    []byte
}

// Plan returns the files that differ between version id and the disk. With
// drift it fails with ErrDrift unless takeover is set; then the manual state
// is recorded first so it stays in the history.
func (s *Store) Plan(id int, user string, takeover bool) ([]Restore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ents, err := s.find(id)
	if err != nil {
		return nil, err
	}
	latest := latestOf(ents)
	drift, err := s.driftIDs(latest)
	if err != nil {
		return nil, err
	}
	if len(drift) > 0 {
		if !takeover {
			return nil, ErrDrift
		}
		if _, _, err := s.recordLocked(latest, user, KindManual, "", 0); err != nil {
			return nil, err
		}
	}
	out := []Restore{}
	for _, f := range s.Files {
		st, _ := stateOf(&e, f.ID)
		cur, curOK, err := readLimited(f.Path)
		if err != nil {
			return nil, err
		}
		if st.Present == curOK && (!curOK || st.SHA256 == hashOf(cur)) {
			continue
		}
		r := Restore{ID: f.ID, Path: f.Path, Present: st.Present}
		if st.Present {
			r.Data = s.content(e, f.ID)
			if r.Data == nil || hashOf(r.Data) != st.SHA256 {
				return nil, fmt.Errorf("config: version %d: %s damaged", id, f.ID)
			}
		}
		out = append(out, r)
	}
	return out, nil
}
