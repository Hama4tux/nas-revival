package helper

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/shares"
)

type chownRec struct {
	path     string
	uid, gid int
}

// filesFixture: share "data" (jana rw via user, petr ro via group rodina,
// eva none), share "bin" with a recycle bin, share "ro" read-only.
func filesFixture(t *testing.T) (*FileManager, string, *[]chownRec) {
	t.Helper()
	vol := t.TempDir()
	for _, d := range []string{"data/docs/sub", "bin", "ro"} {
		if err := os.MkdirAll(filepath.Join(vol, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(vol, "data", "docs", "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(vol, "data", "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(vol, "bin", "old.txt"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(vol, "secret"), []byte("top secret"), 0o644)
	os.Symlink(filepath.Join(vol, "secret"), filepath.Join(vol, "data", "escape"))
	os.Symlink("../secret", filepath.Join(vol, "data", "rel-escape"))
	os.Symlink(filepath.Join(vol, "bin"), filepath.Join(vol, "data", "to-bin"))
	os.Mkdir(filepath.Join(vol, "data", ".recycle"), 0o700)
	os.WriteFile(filepath.Join(vol, "data", ".nas-upload-0123"), nil, 0o600)
	perm := func(kind, name, access string) shares.Permission {
		return shares.Permission{Kind: kind, Name: name, Access: access}
	}
	cfg := shares.Config{Version: 1, Shares: []shares.Share{
		{Name: "data", Volume: vol, Folder: "data", Permissions: []shares.Permission{
			perm(shares.KindUser, "jana", shares.AccessRW), perm(shares.KindGroup, "rodina", shares.AccessRO),
			perm(shares.KindUser, "eva", shares.AccessNone)}},
		{Name: "bin", Volume: vol, Folder: "bin", Recycle: shares.Recycle{Enabled: true, Days: 30},
			Permissions: []shares.Permission{perm(shares.KindUser, "jana", shares.AccessRW)}},
		{Name: "ro", Volume: vol, Folder: "ro", ReadOnly: true,
			Permissions: []shares.Permission{perm(shares.KindUser, "jana", shares.AccessRW)}},
		{Name: "bkp", Volume: vol, Folder: "ro", BackupTarget: true, ReadOnly: true,
			Permissions: []shares.Permission{perm(shares.KindUser, "jana", shares.AccessRO)}},
		{Name: "gone", Volume: vol, Folder: "gone",
			Permissions: []shares.Permission{perm(shares.KindUser, "jana", shares.AccessRW)}},
	}}
	db := accounts.DB{
		Passwd: []accounts.PasswdEntry{{Name: "root"}, {Name: "jana", UID: 1001, GID: 990},
			{Name: "petr", UID: 1002, GID: 990}, {Name: "eva", UID: 1003, GID: 990}},
		Groups: []accounts.GroupEntry{{Name: "nas-users", GID: 990}, {Name: "rodina", GID: 1100, Members: []string{"jana", "petr"}}},
	}
	var rec []chownRec
	m := &FileManager{
		Config: func() (shares.Config, error) { return cfg, nil },
		DB:     func() (accounts.DB, error) { return db, nil },
		HasACL: func(string) bool { return false },
		Chown: func(f *os.File, uid, gid int) error {
			rec = append(rec, chownRec{f.Name(), uid, gid})
			return nil
		},
		Statfs: func(*os.File) (uint64, error) { return 1 << 30, nil },
		Now:    func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) },
	}
	return m, vol, &rec
}

func code(err error) string {
	var he *Error
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

func TestFileSharesFollowTheSMBMatrix(t *testing.T) {
	m, _, _ := filesFixture(t)
	ctx := context.Background()
	got := func(user string) string {
		l, err := m.Shares(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, sh := range l.Shares {
			s = append(s, sh.Name+":"+sh.Access)
		}
		return strings.Join(s, ",")
	}
	if g := got("jana"); g != "data:rw,bin:rw,ro:ro,bkp:ro,gone:rw" {
		t.Errorf("jana = %s", g)
	}
	if g := got("petr"); g != "data:ro" {
		t.Errorf("petr = %s", g)
	}
	if g := got("eva"); g != "" {
		t.Errorf("eva = %s", g)
	}
	// Without a recovery administrator configured, "admin" is nobody.
	if l, _ := m.Shares(ctx, "admin"); !l.NoAccount || len(l.Shares) != 0 {
		t.Errorf("unknown admin = %+v", l)
	}
	if _, err := m.List(ctx, "admin", "data", "", 0); code(err) != CodeForbidden {
		t.Errorf("unknown admin list = %v", err)
	}
	if _, err := m.List(ctx, "eva", "data", "", 0); code(err) != CodeNotFound {
		t.Errorf("eva list = %v", err)
	}
	if _, err := m.List(ctx, "jana", "gone", "", 0); code(err) != CodeNotFound {
		t.Errorf("missing folder = %v", err)
	}
	if _, err := m.Mkdir(ctx, "petr", "data", "", "x"); code(err) != CodeForbidden {
		t.Errorf("ro user mkdir = %v", err)
	}
	if _, err := m.Mkdir(ctx, "jana", "ro", "", "x"); code(err) != CodeForbidden {
		t.Errorf("read-only share mkdir = %v", err)
	}
}

func TestFileListHidesBinAndUploadsAndRefusesEscapes(t *testing.T) {
	m, _, _ := filesFixture(t)
	ctx := context.Background()
	l, err := m.List(ctx, "petr", "data", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range l.Entries {
		names = append(names, e.Name+":"+e.Type)
	}
	if g := strings.Join(names, ","); g != "docs:dir,b.txt:file,escape:link,rel-escape:link,to-bin:link" || l.Access != "ro" {
		t.Errorf("list = %s %s", g, l.Access)
	}
	for _, p := range []string{"..", "../bin", "/etc", "docs/../..", ".recycle", ".nas-upload-0123", "to-bin", "escape"} {
		if _, err := m.List(ctx, "jana", "data", p, 0); err == nil {
			t.Errorf("list %q accepted", p)
		}
	}
	for _, p := range []string{"escape", "rel-escape", "docs", "", "../secret", "to-bin/old.txt"} {
		if fr, err := m.Open(ctx, "jana", "data", p); err == nil {
			fr.f.Close()
			t.Errorf("open %q accepted", p)
		}
	}
	fr, err := m.Open(ctx, "petr", "data", "docs/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(fr.f)
	fr.f.Close()
	if string(b) != "hello" {
		t.Errorf("read = %q", b)
	}
}

func TestFileValidName(t *testing.T) {
	for _, n := range []string{"a.txt", "Faktura 2026", "žluťoučký kůň", ".bashrc"} {
		if !ValidFileName(n) {
			t.Errorf("%q refused", n)
		}
	}
	for _, n := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "a?", " a", "a ", "a.", ".recycle", ".nas-upload-x", "a\x01", strings.Repeat("x", 256), "\xff"} {
		if ValidFileName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}

func TestFileMkdirRenameMove(t *testing.T) {
	m, vol, rec := filesFixture(t)
	ctx := context.Background()
	if _, err := m.Mkdir(ctx, "jana", "data", "docs", "nová"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(vol, "data", "docs", "nová"))
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o770 {
		t.Fatalf("mkdir = %v %v", st, err)
	}
	if len(*rec) != 1 || (*rec)[0].uid != 1001 || (*rec)[0].gid != 990 {
		t.Errorf("owner = %+v", *rec)
	}
	if _, err := m.Mkdir(ctx, "jana", "data", "docs", "a.txt"); code(err) != CodeFileExists {
		t.Errorf("mkdir over file = %v", err)
	}
	if _, err := m.Mkdir(ctx, "jana", "data", "to-bin", "x"); err == nil {
		t.Error("mkdir through a symlink out of the share accepted")
	}
	if _, err := m.Rename(ctx, "jana", "data", "b.txt", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rename(ctx, "jana", "data", "a.txt", "docs"); code(err) != CodeFileExists {
		t.Errorf("rename over existing = %v", err)
	}
	if _, err := m.Rename(ctx, "jana", "data", "a.txt", "../x"); err == nil {
		t.Error("rename with slash accepted")
	}
	// a.txt exists in docs too: that one fails, the folder moves.
	res, err := m.Move(ctx, "jana", "data", []string{"a.txt", "docs/nová", "nothing"}, "docs/sub")
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 2 || len(res.Failed) != 1 || res.Failed[0].Path != "nothing" {
		t.Errorf("move = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(vol, "data", "docs", "sub", "nová")); err != nil {
		t.Error(err)
	}
	if res, _ := m.Move(ctx, "jana", "data", []string{"docs"}, "docs/sub"); res.Done != 0 || len(res.Failed) != 1 {
		t.Errorf("move into itself = %+v", res)
	}
	if _, err := m.Move(ctx, "jana", "data", []string{"docs/a.txt"}, "../bin"); err == nil {
		t.Error("move out of the share accepted")
	}
	if _, err := m.Move(ctx, "jana", "data", []string{".recycle"}, ""); err == nil {
		t.Error("moving the bin accepted")
	}
}

func TestFileDeleteToRecycleBinAndForGood(t *testing.T) {
	m, vol, rec := filesFixture(t)
	ctx := context.Background()
	os.MkdirAll(filepath.Join(vol, "bin", "dir", "deep"), 0o755)
	os.WriteFile(filepath.Join(vol, "bin", "dir", "deep", "f"), []byte("f"), 0o644)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(vol, "bin", "dir", "deep", "f"), old, old)
	res, err := m.Delete(ctx, "jana", "bin", []string{"old.txt", "dir"})
	if err != nil || res.Done != 2 || !res.Recycled {
		t.Fatalf("delete = %+v %v", res, err)
	}
	f := filepath.Join(vol, "bin", ".recycle", "jana", "dir", "deep", "f")
	st, err := os.Stat(f)
	if err != nil || !st.ModTime().Equal(m.Now()) {
		t.Errorf("binned file = %v %v (mtime must be the deletion time)", st, err)
	}
	if st, _ := os.Stat(filepath.Join(vol, "bin", ".recycle", "jana")); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("user bin = %v", st)
	}
	owners := map[string]int{}
	for _, r := range *rec {
		owners[filepath.Base(r.path)] = r.uid
	}
	if owners[".recycle"] != 0 || owners["jana"] != 1001 {
		t.Errorf("bin owners = %v", owners)
	}
	// A second deletion of the same name gets the Samba versions name.
	os.WriteFile(filepath.Join(vol, "bin", "old.txt"), []byte("new"), 0o644)
	if res, _ := m.Delete(ctx, "jana", "bin", []string{"old.txt"}); res.Done != 1 {
		t.Fatalf("second delete = %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(vol, "bin", ".recycle", "jana", "Copy #1 of old.txt")); string(b) != "new" {
		t.Errorf("versioned = %q", b)
	}
	// Without a bin the data is gone; the symlink goes, its target stays.
	res, err = m.Delete(ctx, "jana", "data", []string{"docs", "escape", "nothing"})
	if err != nil || res.Done != 2 || res.Recycled || len(res.Failed) != 1 {
		t.Fatalf("delete = %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(vol, "data", "docs")); !os.IsNotExist(err) {
		t.Error("docs still there")
	}
	if _, err := os.Stat(filepath.Join(vol, "secret")); err != nil {
		t.Error("symlink target removed")
	}
	if _, err := m.Delete(ctx, "jana", "data", []string{""}); err == nil {
		t.Error("deleting the share root accepted")
	}
	if _, err := m.Delete(ctx, "jana", "data", []string{"to-bin/x", "../secret"}); err == nil {
		t.Error("delete outside accepted")
	}
}

func TestFileUploadOverSocket(t *testing.T) {
	m, vol, rec := filesFixture(t)
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, ln, uint32(os.Getuid()), &Dispatcher{Files: m}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	cl := &Client{Socket: sock}
	uctx := WithUser(context.Background(), "jana")

	f, token, err := cl.FileUploadOpen(uctx, "data", "docs", "up.bin", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("abc"))
	f.Close()
	// The unfinished upload is hidden.
	if l, _ := m.List(context.Background(), "jana", "data", "docs", 0); len(l.Entries) != 2 {
		t.Errorf("entries during upload = %+v", l.Entries)
	}
	if err := cl.FileUploadCommit(WithUser(context.Background(), "petr"), "data", "docs", token, "up.bin", false); err == nil {
		t.Error("commit by a read-only user accepted")
	}
	if err := cl.FileUploadCommit(uctx, "data", "docs", token, "up.bin", false); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(vol, "data", "docs", "up.bin"))
	if b, _ := os.ReadFile(filepath.Join(vol, "data", "docs", "up.bin")); string(b) != "abc" || err != nil || st.Mode().Perm() != 0o660 {
		t.Errorf("uploaded = %q %v %v", b, st, err)
	}
	if n := len(*rec); n != 1 || (*rec)[0].uid != 1001 {
		t.Errorf("owner = %+v", *rec)
	}
	if _, _, err := cl.FileUploadOpen(uctx, "data", "docs", "up.bin", 1, false); code(err) != CodeFileExists {
		t.Errorf("existing without overwrite = %v", err)
	}
	if _, _, err := cl.FileUploadOpen(uctx, "data", "docs", "sub", 1, true); code(err) != CodeFileExists {
		t.Errorf("overwrite of a folder = %v", err)
	}
	f, token, err = cl.FileUploadOpen(uctx, "data", "docs", "up.bin", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("xy"))
	f.Close()
	if err := cl.FileUploadCommit(uctx, "data", "docs", token, "up.bin", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(vol, "data", "docs", "up.bin")); string(b) != "xy" {
		t.Errorf("overwritten = %q", b)
	}
	// Abort removes the temporary file.
	f, token, err = cl.FileUploadOpen(uctx, "data", "", "z", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := cl.FileUploadAbort(uctx, "data", "", token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vol, "data", uploadPrefix+token)); !os.IsNotExist(err) {
		t.Error("temporary file left")
	}
	if err := cl.FileUploadCommit(uctx, "data", "", "../../x", "z", false); err == nil {
		t.Error("bad token accepted")
	}
	m.Statfs = func(*os.File) (uint64, error) { return 10 << 20, nil }
	if _, _, err := cl.FileUploadOpen(uctx, "data", "", "big", 1<<20, false); code(err) != CodeInvalid {
		t.Errorf("full volume = %v", err)
	}
}

func TestAdminsSeeEveryShareInFiles(t *testing.T) {
	m, _, rec := filesFixture(t)
	ctx := context.Background()
	base, _ := m.DB()
	db := base
	db.Passwd = append(append([]accounts.PasswdEntry{}, base.Passwd...), accounts.PasswdEntry{Name: "karel", UID: 1004, GID: 990})
	db.Groups = append(append([]accounts.GroupEntry{}, base.Groups...), accounts.GroupEntry{Name: accounts.AdminGroup, GID: 991, Members: []string{"karel"}})
	m.DB = func() (accounts.DB, error) { return db, nil }
	m.Recovery = func() string { return "admin" }
	for _, user := range []string{"karel", "admin"} {
		l, err := m.Shares(ctx, user)
		if err != nil || l.NoAccount {
			t.Fatalf("%s: %+v %v", user, l, err)
		}
		var s []string
		for _, sh := range l.Shares {
			s = append(s, sh.Name+":"+sh.Access)
		}
		// No permission entry needed; a read-only share stays read-only.
		if g := strings.Join(s, ","); g != "data:rw,bin:rw,ro:ro,bkp:ro,gone:rw" {
			t.Errorf("%s = %s", user, g)
		}
		if _, err := m.List(ctx, user, "data", "", 0); err != nil {
			t.Errorf("%s list = %v", user, err)
		}
		if _, err := m.Mkdir(ctx, user, "ro", "", "x"); code(err) != CodeForbidden {
			t.Errorf("%s read-only share mkdir = %v", user, err)
		}
		if _, err := m.Mkdir(ctx, user, "bkp", "", "x"); code(err) != CodeForbidden {
			t.Errorf("%s backup target mkdir = %v", user, err)
		}
	}
	*rec = nil
	if _, err := m.Mkdir(ctx, "admin", "data", "", "od-admina"); err != nil {
		t.Fatal(err)
	}
	// The recovery administrator has no account: new entries are root's with group nas-users.
	if len(*rec) != 1 || (*rec)[0].uid != 0 || (*rec)[0].gid != 990 {
		t.Errorf("owner = %+v", *rec)
	}
	// Other accounts are not affected.
	if l, _ := m.Shares(ctx, "eva"); len(l.Shares) != 0 {
		t.Errorf("eva = %+v", l.Shares)
	}
}
