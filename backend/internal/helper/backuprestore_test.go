package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func restoreFixture(t *testing.T) (*RestoreManager, string, *[]chownRec) {
	t.Helper()
	bm, bvol := versionsFixture(t)
	fm, fvol, rec := filesFixture(t)
	old := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(bvol, "versions", "2026-11-25", "job", "app", "dump.sql"), old, old)
	os.Chtimes(filepath.Join(bvol, "versions", "2026-11-25", "job", "app"), old, old)
	return &RestoreManager{Backup: bm, Files: fm}, fvol, rec
}

func restoreAndWait(t *testing.T, m *RestoreManager, req RestoreRequest, user string) RestoreStatus {
	t.Helper()
	if _, err := m.Start(context.Background(), req, user); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	return m.Status()
}

// noHidden checks that no unfinished restore stayed in dir.
func noHidden(t *testing.T, dir string) {
	t.Helper()
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		if strings.HasPrefix(d.Name(), uploadPrefix) && d.Name() != ".nas-upload-0123" {
			t.Errorf("left behind: %s", d.Name())
		}
	}
}

func TestRestoreFolderIntoShare(t *testing.T) {
	m, fvol, rec := restoreFixture(t)
	st := restoreAndWait(t, m, RestoreRequest{ID: "job", Date: "2026-11-25", Path: "app", Share: "data", Name: "app (obnova)"}, "jana")
	if st.State != RestoreDone || st.Files != 3 || st.TotalFiles != 3 || st.Bytes != 13 || st.TotalBytes != 13 || st.Skipped != 2 {
		t.Fatalf("status = %+v", st)
	}
	dst := filepath.Join(fvol, "data", "app (obnova)")
	if b, _ := os.ReadFile(filepath.Join(dst, "dump.sql")); string(b) != "bbbbbbb" {
		t.Errorf("dump.sql = %q", b)
	}
	if fi, err := os.Stat(filepath.Join(dst, "a-dir")); err != nil || !fi.IsDir() {
		t.Errorf("a-dir: %v", err)
	}
	// Symlinks of the backup never reach the share.
	for _, n := range []string{"escape", "rel-escape"} {
		if _, err := os.Lstat(filepath.Join(dst, n)); err == nil {
			t.Errorf("%s restored", n)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dst, "dump.sql")); !fi.ModTime().Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)) || fi.Mode().Perm() != 0o660 {
		t.Errorf("dump.sql mtime/mode = %v %v", fi.ModTime(), fi.Mode())
	}
	if fi, _ := os.Stat(dst); !fi.ModTime().Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)) || fi.Mode().Perm() != 0o770 {
		t.Errorf("folder mtime/mode = %v %v", fi.ModTime(), fi.Mode())
	}
	// Folder, a-dir and three files belong to jana:nas-users.
	if len(*rec) != 5 {
		t.Errorf("chown = %+v", *rec)
	}
	for _, r := range *rec {
		if r.uid != 1001 || r.gid != 990 {
			t.Errorf("chown %+v", r)
		}
	}
	noHidden(t, filepath.Join(fvol, "data"))
}

func TestRestoreWholeVersionAndSingleFile(t *testing.T) {
	m, fvol, _ := restoreFixture(t)
	st := restoreAndWait(t, m, RestoreRequest{ID: "job", Date: "2026-11-25", Path: "app/dump.sql", Share: "data", Dir: "docs", Name: "dump.sql"}, "jana")
	if st.State != RestoreDone || st.Files != 1 || st.Dest != "docs/dump.sql" {
		t.Fatalf("status = %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(fvol, "data", "docs", "dump.sql")); string(b) != "bbbbbbb" {
		t.Errorf("file = %q", b)
	}
	st = restoreAndWait(t, m, RestoreRequest{ID: "job", Date: "2026-11-24", Share: "data", Dir: "docs", Name: "celá verze"}, "jana")
	if st.State != RestoreDone || st.Files != 2 {
		t.Fatalf("whole version = %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(fvol, "data", "docs", "celá verze", "app", "same.txt")); string(b) != "12345" {
		t.Errorf("same.txt = %q", b)
	}
	noHidden(t, filepath.Join(fvol, "data", "docs"))
}

func TestRestoreRefusals(t *testing.T) {
	m, _, _ := restoreFixture(t)
	ok := RestoreRequest{ID: "job", Date: "2026-11-25", Path: "app", Share: "data", Name: "x"}
	cases := []struct {
		name string
		mod  func(r *RestoreRequest)
		user string
		want string
	}{
		{"exists", func(r *RestoreRequest) { r.Name = "b.txt" }, "jana", CodeFileExists},
		{"read only user", nil, "petr", CodeForbidden},
		{"no access", nil, "eva", CodeNotFound},
		{"read-only share", func(r *RestoreRequest) { r.Share = "ro" }, "jana", CodeForbidden},
		{"symlink source", func(r *RestoreRequest) { r.Path = "app/escape" }, "jana", CodeInvalid},
		{"dotdot", func(r *RestoreRequest) { r.Path = "../other" }, "jana", CodeInvalid},
		{"missing", func(r *RestoreRequest) { r.Path = "app/nope" }, "jana", CodeNotFound},
		{"unknown date", func(r *RestoreRequest) { r.Date = "2026-01-01" }, "jana", CodeNotFound},
		{"bad name", func(r *RestoreRequest) { r.Name = "a/b" }, "jana", CodeInvalid},
		{"hidden name", func(r *RestoreRequest) { r.Name = ".nas-upload-x" }, "jana", CodeInvalid},
		{"into recycle", func(r *RestoreRequest) { r.Dir = ".recycle" }, "jana", CodeInvalid},
		{"dir is symlink", func(r *RestoreRequest) { r.Dir = "to-bin" }, "jana", CodeNotFound},
	}
	for _, c := range cases {
		r := ok
		if c.mod != nil {
			c.mod(&r)
		}
		if _, err := m.Start(context.Background(), r, c.user); code(err) != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	if m.Status().State != RestoreIdle {
		t.Errorf("status = %+v", m.Status())
	}
}

func TestRestoreNoSpaceFailsAndCleansUp(t *testing.T) {
	m, fvol, _ := restoreFixture(t)
	m.Files.Statfs = func(*os.File) (uint64, error) { return uploadReserve, nil }
	st := restoreAndWait(t, m, RestoreRequest{ID: "job", Date: "2026-11-25", Path: "app", Share: "data", Name: "app"}, "jana")
	if st.State != RestoreFailed || !strings.Contains(st.Message, "místa") || st.Finished == nil {
		t.Fatalf("status = %+v", st)
	}
	if _, err := os.Lstat(filepath.Join(fvol, "data", "app")); err == nil {
		t.Error("target created")
	}
	noHidden(t, filepath.Join(fvol, "data"))
}

func TestRestoreBusyAndCancel(t *testing.T) {
	m, fvol, _ := restoreFixture(t)
	release := make(chan struct{})
	m.Lower = func() { <-release }
	req := RestoreRequest{ID: "job", Date: "2026-11-25", Path: "app", Share: "data", Name: "app"}
	if _, err := m.Start(context.Background(), req, "jana"); err != nil {
		t.Fatal(err)
	}
	req.Name = "app2"
	if _, err := m.Start(context.Background(), req, "jana"); code(err) != CodeRestoreRunning {
		t.Fatalf("second start: %v", err)
	}
	if st := m.Cancel(); !st.Running() {
		t.Fatalf("cancel before the job ran: %+v", st)
	}
	close(release)
	m.Wait()
	if st := m.Status(); st.State != RestoreCancelled || st.Files != 0 {
		t.Fatalf("status = %+v", st)
	}
	if _, err := os.Lstat(filepath.Join(fvol, "data", "app")); err == nil {
		t.Error("cancelled restore created the target")
	}
	noHidden(t, filepath.Join(fvol, "data"))
	// A finished restore does not block the next one.
	m.Lower = nil
	if st := restoreAndWait(t, m, req, "jana"); st.State != RestoreDone {
		t.Fatalf("after cancel: %+v", st)
	}
}
