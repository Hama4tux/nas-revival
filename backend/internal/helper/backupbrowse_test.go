package helper

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// versionsFixture builds versions/<date>/job like the runner does: the
// unchanged file is a hardlink of the older version, the dump is new.
func versionsFixture(t *testing.T) (*BackupManager, string) {
	t.Helper()
	vol := t.TempDir()
	mk := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(vol, "versions", "2026-11-24", "job")
	newer := filepath.Join(vol, "versions", "2026-11-25", "job")
	mk(filepath.Join(old, "app", "same.txt"), "12345")
	mk(filepath.Join(old, "app", "dump.sql"), "aaa")
	os.MkdirAll(filepath.Join(newer, "app"), 0o700)
	if err := os.Link(filepath.Join(old, "app", "same.txt"), filepath.Join(newer, "app", "same.txt")); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(newer, "app", "dump.sql"), "bbbbbbb")
	mk(filepath.Join(newer, "app", "b-file"), "x")
	os.Mkdir(filepath.Join(newer, "app", "a-dir"), 0o700)
	// Symlinks that point out of the version must never be followed.
	secret := filepath.Join(vol, "secret")
	mk(secret, "top secret")
	os.Symlink(secret, filepath.Join(newer, "app", "escape"))
	os.Symlink("../../../../secret", filepath.Join(newer, "app", "rel-escape"))
	// Another job and a stray file are ignored.
	mk(filepath.Join(vol, "versions", "2026-11-25", "other", "x"), "x")
	mk(filepath.Join(vol, "versions", "notes.txt"), "x")
	return &BackupManager{Volume: vol}, vol
}

func TestBackupVersionsSizesAndAddedBytes(t *testing.T) {
	m, _ := versionsFixture(t)
	res, err := m.Versions(context.Background(), "job")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete || len(res.Versions) != 2 {
		t.Fatalf("versions = %+v", res)
	}
	n, o := res.Versions[0], res.Versions[1]
	if n.Date != "2026-11-25" || o.Date != "2026-11-24" {
		t.Fatalf("order = %+v", res.Versions)
	}
	if o.Files != 2 || o.Bytes != 8 || o.AddedBytes != 8 {
		t.Errorf("oldest = %+v", o)
	}
	// same.txt (5 B) is a hardlink of the older version: not added.
	if n.Files != 3 || n.Bytes != 13 || n.AddedBytes != 8 {
		t.Errorf("newest = %+v", n)
	}
	if r, err := m.Versions(context.Background(), "nothing"); err != nil || len(r.Versions) != 0 {
		t.Errorf("unknown job = %+v %v", r, err)
	}
	if _, err := m.Versions(context.Background(), "../x"); err == nil {
		t.Error("bad id accepted")
	}
}

func TestBackupBrowseSortsAndPages(t *testing.T) {
	m, _ := versionsFixture(t)
	res, err := m.Browse(context.Background(), "job", "2026-11-25", "app", 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range res.Entries {
		names = append(names, e.Name+":"+e.Type)
	}
	want := "a-dir:dir b-file:file dump.sql:file escape:link rel-escape:link same.txt:file"
	if strings.Join(names, " ") != want || res.Total != 6 {
		t.Fatalf("entries = %v (total %d)", names, res.Total)
	}
	if res.Entries[2].Size != 7 {
		t.Errorf("dump size = %d", res.Entries[2].Size)
	}
	if r, err := m.Browse(context.Background(), "job", "2026-11-25", "app", 5); err != nil || len(r.Entries) != 1 {
		t.Errorf("page = %+v %v", r, err)
	}
	if r, err := m.Browse(context.Background(), "job", "2026-11-25", "", 0); err != nil || len(r.Entries) != 1 || r.Entries[0].Name != "app" {
		t.Errorf("root = %+v %v", r, err)
	}
}

func TestBackupBrowseRefusesEscapes(t *testing.T) {
	m, _ := versionsFixture(t)
	for _, c := range []struct{ id, date, path string }{
		{"job", "2026-11-25", "../other"},
		{"job", "2026-11-25", "app/../.."},
		{"job", "2026-11-25", "/etc"},
		{"job", "2026-11-25", "app/escape"},
		{"job", "2026-11-25", "app/rel-escape"},
		{"job", "2026-11-25", "app/same.txt"},
		{"job", "../2026", "app"},
		{"job", "2026-11-26", "app"},
		{"..", "2026-11-25", ""},
	} {
		if _, err := m.Browse(context.Background(), c.id, c.date, c.path, 0); err == nil {
			t.Errorf("browse %+v accepted", c)
		}
	}
}

func TestValidVersionPath(t *testing.T) {
	for p, ok := range map[string]bool{
		"": true, "app": true, "app/Faktury 2026/č.pdf": true,
		"/app": false, "app/": false, "app//x": false, "./app": false, "app/..": false,
		"..": false, "a\x00b": false, "a\nb": false, "\xff": false, strings.Repeat("a", 1025): false,
	} {
		if ValidVersionPath(p) != ok {
			t.Errorf("ValidVersionPath(%q) = %v", p, !ok)
		}
	}
}

func TestBackupOpenPassesDescriptorOverSocket(t *testing.T) {
	m, _ := versionsFixture(t)
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, ln, uint32(os.Getuid()), &Dispatcher{Backup: m}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	cl := &Client{Socket: sock}

	f, meta, err := cl.BackupOpen(context.Background(), "job", "2026-11-25", "app/dump.sql")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(body) != "bbbbbbb" || meta.Name != "dump.sql" || meta.Size != 7 {
		t.Fatalf("file = %q %+v %v", body, meta, err)
	}

	for _, p := range []string{"app/escape", "app/rel-escape", "app/a-dir", "app/../../secret", "", "app/none"} {
		if f, _, err := cl.BackupOpen(context.Background(), "job", "2026-11-25", p); err == nil {
			f.Close()
			t.Errorf("open %q accepted", p)
		}
	}

	v, err := cl.BackupVersions(context.Background(), "job")
	if err != nil || len(v.Versions) != 2 {
		t.Errorf("versions over socket = %+v %v", v, err)
	}
	b, err := cl.BackupBrowse(context.Background(), "job", "2026-11-24", "app", 0)
	if err != nil || b.Total != 2 {
		t.Errorf("browse over socket = %+v %v", b, err)
	}
}
