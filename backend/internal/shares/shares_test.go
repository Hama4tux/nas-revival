package shares

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nas-revival/backend/internal/accounts"
)

func testDB() accounts.DB {
	return accounts.DB{
		Passwd: []accounts.PasswdEntry{
			{Name: "root", UID: 0, GID: 0},
			{Name: "jana", UID: 1001, GID: 990},
			{Name: "petr", UID: 1002, GID: 990},
			{Name: "eva", UID: 1003, GID: 990},
		},
		Groups: []accounts.GroupEntry{
			{Name: "nas-users", GID: 990},
			{Name: "rodina", GID: 1100, Members: []string{"jana", "petr"}},
		},
	}
}

func baseShare() Share {
	return Share{Name: "Fotky", Volume: "/srv/data", Folder: "Fotky", Recycle: Recycle{Enabled: true, Days: 30},
		Permissions: []Permission{{KindGroup, "rodina", AccessRW}, {KindUser, "petr", AccessNone}}}
}

func TestEncodeACLByteVector(t *testing.T) {
	got := hex.EncodeToString(EncodeACL([]ACLEntry{
		{TagOther, 0, idNone}, {TagUser, 5, 1002}, {TagUserObj, 7, idNone}, {TagMask, 7, idNone},
		{TagGroupObj, 0, idNone}, {TagUser, 7, 1001},
	}))
	want := "02000000" + // version 2
		"0100" + "0700" + "ffffffff" + // USER_OBJ rwx
		"0200" + "0700" + "e9030000" + // USER 1001 rwx
		"0200" + "0500" + "ea030000" + // USER 1002 r-x
		"0400" + "0000" + "ffffffff" + // GROUP_OBJ ---
		"1000" + "0700" + "ffffffff" + // MASK rwx
		"2000" + "0000" + "ffffffff" // OTHER ---
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestValidateShape(t *testing.T) {
	ok, err := ValidateShape(Share{Name: "Fotky", Volume: "/srv/data", Description: "  Rodinné  "})
	if err != nil || ok.Folder != "Fotky" || ok.Description != "Rodinné" || ok.Permissions == nil {
		t.Fatalf("%+v %v", ok, err)
	}
	ok, _ = ValidateShape(Share{Name: "A", Volume: "/srv/data", Recycle: Recycle{Enabled: true}})
	if ok.Recycle.Days != 30 {
		t.Fatalf("days = %d", ok.Recycle.Days)
	}
	long := strings.Repeat("x", 65)
	bads := map[string]Share{
		"name":         {Name: "a b", Volume: "/srv/data"},
		"name2":        {Name: "GLOBAL", Volume: "/srv/data"},
		"name3":        {Name: "Print$", Volume: "/srv/data"},
		"description":  {Name: "a", Volume: "/srv/data", Description: long},
		"description2": {Name: "a", Volume: "/srv/data", Description: "x\ny"},
		"description3": {Name: "a", Volume: "/srv/data", Description: `a\b`},
		"folder":       {Name: "a", Volume: "/srv/data", Folder: "../etc"},
		"volume":       {Name: "a", Volume: "/etc"},
		"volume2":      {Name: "a", Volume: "/srv/data/../x"},
		"days":         {Name: "a", Volume: "/srv/data", Recycle: Recycle{Enabled: true, Days: 366}},
		"groupnone":    {Name: "a", Volume: "/srv/data", Permissions: []Permission{{KindGroup, "rodina", AccessNone}}},
		"dup":          {Name: "a", Volume: "/srv/data", Permissions: []Permission{{KindUser, "petr", AccessRO}, {KindUser, "petr", AccessRW}}},
		"kind":         {Name: "a", Volume: "/srv/data", Permissions: []Permission{{"x", "petr", AccessRO}}},
		"access":       {Name: "a", Volume: "/srv/data", Permissions: []Permission{{KindUser, "petr", "x"}}},
	}
	for n, s := range bads {
		if _, err := ValidateShape(s); err == nil {
			t.Errorf("%s accepted", n)
		} else if _, isField := err.(*FieldError); !isField {
			t.Errorf("%s: %T", n, err)
		}
	}
	many := Share{Name: "a", Volume: "/srv/data"}
	for i := 0; i < 65; i++ {
		many.Permissions = append(many.Permissions, Permission{KindUser, "u" + strings.Repeat("a", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)), AccessRO})
	}
	if _, err := ValidateShape(many); err == nil {
		t.Error("65 permissions accepted")
	}
}

func TestCheckPrincipals(t *testing.T) {
	db := testDB()
	if err := CheckPrincipals(baseShare(), db); err != nil {
		t.Fatal(err)
	}
	s := baseShare()
	s.Permissions = []Permission{{KindUser, "root", AccessRO}}
	if CheckPrincipals(s, db) == nil {
		t.Error("root accepted")
	}
	s.Permissions = []Permission{{KindGroup, "nas-users", AccessRO}}
	if err := CheckPrincipals(s, db); err != nil {
		t.Error(err)
	}
	s.Permissions = []Permission{{KindGroup, "sudo", AccessRO}}
	if CheckPrincipals(s, db) == nil {
		t.Error("unknown group accepted")
	}
}

func TestEffectiveAndConflicts(t *testing.T) {
	db := testDB()
	s := baseShare()
	s.Permissions = append(s.Permissions, Permission{KindGroup, "nas-users", AccessRO})
	got := map[string]UserAccess{}
	for _, u := range Effective(s, db) {
		got[u.User] = u
	}
	want := map[string]UserAccess{
		"jana": {"jana", AccessRW, "group:rodina"},
		"petr": {"petr", AccessNone, "user"},
		"eva":  {"eva", AccessRO, "group:nas-users"},
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %+v, want %+v", k, got[k], w)
		}
	}
	s.ReadOnly = true
	for _, u := range Effective(s, db) {
		if u.Access == AccessRW {
			t.Errorf("%s keeps rw on a read-only share", u.User)
		}
	}
	c := Conflicts(s, db)
	if len(c) != 2 || !strings.Contains(c[0], "petr") || !strings.Contains(c[0], "rodina") {
		t.Fatalf("conflicts = %v", c)
	}
	s.Permissions = []Permission{{KindGroup, "rodina", AccessRW}, {KindUser, "petr", AccessRO}}
	if c := Conflicts(s, db); len(c) != 1 || !strings.Contains(c[0], "ro přepisuje rw") {
		t.Fatalf("conflicts = %v", c)
	}
}

func TestRenderGolden(t *testing.T) {
	s1 := baseShare()
	s1.Description = "Rodinné fotky"
	s1.Hidden = true
	s1.Permissions = []Permission{{KindGroup, "rodina", AccessRW}, {KindUser, "eva", AccessRO}, {KindUser, "petr", AccessNone}}
	s2 := Share{Name: "Sklad", Volume: "/srv/data", Folder: "Sklad", ReadOnly: true,
		Permissions: []Permission{{KindGroup, "nas-users", AccessRW}}}
	s3 := Share{Name: "Prazdna", Volume: "/srv/data", Folder: "Prazdna", Permissions: []Permission{{KindUser, "petr", AccessNone}}}

	acl := Render([]Share{s1, s2, s3}, func(string) bool { return true })
	wantACL := `# Generated by NAS Revival. Do not edit; changes are overwritten.

[Fotky]
   path = /srv/data/Fotky
   comment = Rodinné fotky
   browseable = no
   read only = no
   valid users = @rodina eva
   invalid users = petr
   read list = eva
   write list = @rodina
   inherit acls = yes
   vfs objects = recycle
   recycle:repository = .recycle/%U
   recycle:keeptree = yes
   recycle:versions = yes
   recycle:touch_mtime = yes
   recycle:exclude_dir = .recycle
   hide files = /.recycle/

[Sklad]
   path = /srv/data/Sklad
   read only = yes
   valid users = @nas-users
   read list = @nas-users
   inherit acls = yes

[Prazdna]
   path = /srv/data/Prazdna
   read only = no
   available = no
   invalid users = petr
   inherit acls = yes
`
	if acl != wantACL {
		t.Fatalf("ACL render:\n%s", acl)
	}
	smb := Render([]Share{s2}, func(string) bool { return false })
	wantSMB := `# Generated by NAS Revival. Do not edit; changes are overwritten.

[Sklad]
   path = /srv/data/Sklad
   read only = yes
   valid users = @nas-users
   read list = @nas-users
   force group = nas-users
   create mask = 0660
   directory mask = 0770
`
	if smb != wantSMB {
		t.Fatalf("fallback render:\n%s", smb)
	}
}

const mountinfo = `22 1 8:2 / / rw,relatime - ext4 /dev/sda2 rw
30 22 8:3 / /srv/data rw,relatime shared:2 - ext4 /dev/sda3 rw
31 22 8:4 / /srv/with\040space rw - ext4 /dev/sda4 rw
32 22 0:40 / /srv/tmp rw - tmpfs tmpfs rw
33 22 8:5 / /srv/ntfs rw - fuseblk /dev/sda5 rw
34 22 8:2 /var /srv/bind rw - ext4 /dev/sda2 rw
35 22 8:6 / /mnt/other rw - ext4 /dev/sdb1 rw
36 22 8:7 / /srv rw - ext4 /dev/sdb2 rw
`

func TestVolumeDetection(t *testing.T) {
	ms := DataMounts(ParseMountinfo(strings.NewReader(mountinfo)))
	var got []string
	for _, m := range ms {
		got = append(got, m.Point)
	}
	if strings.Join(got, "|") != "/srv/data|/srv/with space" {
		t.Fatalf("mounts = %v", got)
	}
	f := filepath.Join(t.TempDir(), "mountinfo")
	os.WriteFile(f, []byte(mountinfo), 0o600)
	vols, err := Volumes(f, func(string) (uint64, uint64, error) { return 100, 40, nil },
		func(v string) bool { return v == "/srv/data" })
	if err != nil || len(vols) != 2 || vols[0].TotalBytes != 100 || vols[0].AvailBytes != 40 || !vols[0].ACL || vols[1].ACL {
		t.Fatalf("%+v %v", vols, err)
	}
	if _, err := FindVolume(vols, "/srv/tmp"); err == nil {
		t.Error("tmpfs accepted")
	}
}

func TestACLProbeCachesAndDetectsNoSupport(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	var seen string
	p := NewACLProbe(func(path, name string, data []byte) error {
		calls++
		seen = path
		if name != XattrAccess {
			t.Errorf("name %s", name)
		}
		return syscall.EOPNOTSUPP
	})
	if p(dir) || p(dir) || calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if !strings.HasPrefix(filepath.Base(seen), ".nas-revival-aclprobe-") {
		t.Errorf("probe dir %s", seen)
	}
	if _, err := os.Stat(seen); err == nil {
		t.Error("probe dir left behind")
	}
	ok := NewACLProbe(func(string, string, []byte) error { return nil })
	if !ok(dir) {
		t.Error("supported volume reported false")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("leftovers %v", ents)
	}
}

type setCall struct{ path, name string }

func TestApplyTreeACL(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "f.txt"), []byte("x"), 0o644)
	os.Symlink("/etc", filepath.Join(root, "link"))
	var calls []setCall
	datas := map[string][]byte{}
	set := func(p, n string, d []byte) error {
		calls = append(calls, setCall{p, n})
		datas[p+"|"+n] = d
		return nil
	}
	s := baseShare()
	n, err := ApplyTree(context.Background(), root, s, testDB(), set)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// 2 dirs x (access+default) + 1 file access, symlink skipped.
	if len(calls) != 5 {
		t.Fatalf("calls = %v", calls)
	}
	for _, c := range calls {
		if strings.HasSuffix(c.path, "link") {
			t.Error("symlink touched")
		}
	}
	dirACL := datas[root+"|"+XattrAccess]
	fileACL := datas[filepath.Join(root, "sub", "f.txt")+"|"+XattrAccess]
	if string(datas[root+"|"+XattrDefault]) != string(dirACL) {
		t.Error("default ACL differs")
	}
	// group rodina (gid 1100 = 0x44c) rw: dir rwx, file rw-; petr (1002) none.
	if !strings.Contains(hex.EncodeToString(dirACL), "0200"+"0000"+"ea030000") ||
		!strings.Contains(hex.EncodeToString(dirACL), "0800"+"0700"+"4c040000") ||
		!strings.Contains(hex.EncodeToString(fileACL), "0800"+"0600"+"4c040000") ||
		!strings.Contains(hex.EncodeToString(fileACL), "1000"+"0600") {
		t.Fatalf("dir %x\nfile %x", dirACL, fileACL)
	}
	// read-only share: rw becomes r-x / r--.
	s.ReadOnly = true
	ro := BuildACL(s, testDB(), true)
	for _, e := range ro {
		if e.Tag == TagGroup && e.Perm != 5 {
			t.Errorf("ro dir group perm %d", e.Perm)
		}
	}
}

func TestApplyTreeErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := ApplyTree(context.Background(), root, baseShare(), testDB(),
		func(string, string, []byte) error { return syscall.ENOTSUP }); !errors.Is(err, ErrNoACL) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ApplyTree(ctx, root, baseShare(), testDB(), func(string, string, []byte) error { return nil })
	var inc *IncompleteError
	if !errors.As(err, &inc) {
		t.Fatalf("err = %v", err)
	}
}

func TestFallbackSetupAndAdopt(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "d"), 0o700)
	os.WriteFile(filepath.Join(root, "d", "f"), nil, 0o600)
	os.Symlink("/etc/passwd", filepath.Join(root, "l"))
	type ch struct {
		p      string
		uid, g int
	}
	var chowns []ch
	chown := func(p string, u, g int) error { chowns = append(chowns, ch{p, u, g}); return nil }
	if err := SetupRootFallback(root, 990, chown); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(root)
	if fi.Mode()&os.ModeSetgid != 0 || fi.Mode().Perm() != 0o770 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if chowns[0] != (ch{root, 0, 990}) {
		t.Fatalf("chown %v", chowns)
	}
	chowns = nil
	n, err := AdoptFallback(context.Background(), root, 990, chown)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, c := range chowns {
		if c.uid != -1 || c.g != 990 || strings.HasSuffix(c.p, "/l") {
			t.Errorf("chown %v", c)
		}
	}
	d, _ := os.Stat(filepath.Join(root, "d"))
	f, _ := os.Stat(filepath.Join(root, "d", "f"))
	if d.Mode().Perm() != 0o770 || d.Mode()&os.ModeSetgid != 0 || f.Mode().Perm() != 0o660 {
		t.Fatalf("d %v f %v", d.Mode(), f.Mode())
	}
	if err := SetupRootACL(root, chown); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(root); fi.Mode().Perm() != 0o770 {
		t.Fatal(fi.Mode())
	}
}

func TestCleanupRecycle(t *testing.T) {
	share := t.TempDir()
	rec := filepath.Join(share, ".recycle", "petr", "sub")
	os.MkdirAll(rec, 0o755)
	now := time.Now()
	mk := func(p string, age time.Duration, size int) {
		os.WriteFile(p, make([]byte, size), 0o644)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
	}
	old := 40 * 24 * time.Hour
	mk(filepath.Join(rec, "old.txt"), old, 10)
	mk(filepath.Join(rec, "new.txt"), time.Hour, 5)
	mk(filepath.Join(share, ".recycle", "petr", "old2"), old, 7)
	outside := filepath.Join(share, "keep.txt")
	mk(outside, old, 3)
	// An old symlink to a file outside the bin: the link goes, the target stays.
	os.Symlink(outside, filepath.Join(share, ".recycle", "petr", "lnk"))
	os.MkdirAll(filepath.Join(share, ".recycle", "eva", "empty"), 0o755)
	r, err := CleanupRecycle(context.Background(), share, 30, now.Add(0))
	if err != nil {
		t.Fatal(err)
	}
	if r.RemovedFiles != 2 || r.RemovedBytes != 17 {
		t.Fatalf("result %+v", r) // symlink is fresh (Lstat mtime = now): kept
	}
	if _, err := os.Stat(filepath.Join(rec, "new.txt")); err != nil {
		t.Error("new file removed")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("target outside removed")
	}
	if _, err := os.Stat(filepath.Join(share, ".recycle", "eva")); err == nil {
		t.Error("empty dirs kept")
	}
	if _, err := os.Stat(filepath.Join(share, ".recycle")); err != nil {
		t.Error(".recycle removed")
	}
	// Later the link is old too and is removed as a link.
	r, _ = CleanupRecycle(context.Background(), share, 30, now.Add(60*24*time.Hour))
	if _, err := os.Lstat(filepath.Join(share, ".recycle", "petr", "lnk")); err == nil {
		t.Error("old symlink kept")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("symlink target removed")
	}
	if r, err := CleanupRecycle(context.Background(), t.TempDir(), 30, now); err != nil || r.RemovedFiles != 0 {
		t.Error("no .recycle must be a no-op")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "shares.json")
	c, err := Load(p)
	if err != nil || len(c.Shares) != 0 {
		t.Fatal(err)
	}
	b, _ := ValidateShape(Share{Name: "b", Volume: "/srv/data"})
	a, _ := ValidateShape(Share{Name: "A", Volume: "/srv/data"})
	c.Shares = []Share{b, a}
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p)
	if err != nil || len(c2.Shares) != 2 || c2.Shares[0].Name != "A" || c2.Find("B") != 1 {
		t.Fatalf("%+v %v", c2, err)
	}
	if _, err := Parse([]byte(`{"version":2}`)); err == nil {
		t.Error("version 2 accepted")
	}
	if _, err := Parse([]byte(`{"version":1,"shares":[{"name":"a","volume":"/srv/d"},{"name":"A","volume":"/srv/d"}]}`)); err == nil {
		t.Error("duplicate accepted")
	}
}
