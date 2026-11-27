package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/tasks"
)

type shareFix struct {
	m      *ShareManager
	vol    string
	dir    string
	acl    bool
	xattrs []string
	chowns []string
	runs   []string
	smbd   bool
}

func newShareFix(t *testing.T, acl bool) *shareFix {
	t.Helper()
	dir := t.TempDir()
	vol := filepath.Join(dir, "data")
	os.Mkdir(vol, 0o755)
	old := shares.VolumeRoot
	shares.VolumeRoot = dir + "/"
	t.Cleanup(func() { shares.VolumeRoot = old })
	f := &shareFix{vol: vol, dir: dir, acl: acl}
	mi := "22 1 8:2 / / rw - ext4 /dev/sda2 rw\n30 22 8:3 / " + vol + " rw - ext4 /dev/sda3 rw\n"
	os.WriteFile(filepath.Join(dir, "mountinfo"), []byte(mi), 0o644)
	os.WriteFile(filepath.Join(dir, "passwd"), []byte(basePasswd+"petr:x:1005:990::/nonexistent:/usr/sbin/nologin\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "group"), []byte(baseGroup), 0o644)
	os.MkdirAll(filepath.Join(dir, "samba"), 0o755)
	f.m = &ShareManager{
		Path:      filepath.Join(dir, "etc", "shares.json"),
		Accounts:  accounts.Paths{Passwd: filepath.Join(dir, "passwd"), Group: filepath.Join(dir, "group")},
		Mountinfo: filepath.Join(dir, "mountinfo"),
		Statfs:    func(string) (uint64, uint64, error) { return 1000, 500, nil },
		Probe:     func(string) bool { return acl },
		SetXattr: func(p, n string, _ []byte) error {
			f.xattrs = append(f.xattrs, filepath.Base(p)+" "+n)
			return nil
		},
		Chown: func(p string, u, g int) error {
			f.chowns = append(f.chowns, filepath.Base(p)+" "+itoa(u)+":"+itoa(g))
			return nil
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.runs = append(f.runs, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		LookPath: func(n string) (string, error) {
			if n == "smbd" && !f.smbd || n == "testparm" && !f.smbd {
				return "", os.ErrNotExist
			}
			return "/usr/bin/" + n, nil
		},
		SambaDir:   filepath.Join(dir, "samba"),
		SambaConf:  filepath.Join(dir, "samba", "nas-revival-shares.conf"),
		SambaMain:  filepath.Join(dir, "samba", "smb.conf"),
		Now:        func() time.Time { return time.Unix(1700000000, 0) },
		Background: func(f func()) { f() },
	}
	return f
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func fotky(vol string) shares.Share {
	return shares.Share{Name: "Fotky", Volume: vol, Recycle: shares.Recycle{Enabled: true, Days: 14},
		Permissions: []shares.Permission{{Kind: "group", Name: "rodina", Access: "rw"}, {Kind: "user", Name: "petr", Access: "none"}}}
}

func TestShareCreateACLMode(t *testing.T) {
	f := newShareFix(t, true)
	l, err := f.m.Create(context.Background(), fotky(f.vol), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Shares) != 1 || l.Shares[0].Enforcement != "acl" || !l.Shares[0].Exists || l.Shares[0].Path != f.vol+"/Fotky" {
		t.Fatalf("%+v", l.Shares)
	}
	if len(l.Volumes) != 1 || !l.Volumes[0].ACL || l.Volumes[0].AvailBytes != 500 {
		t.Fatalf("volumes %+v", l.Volumes)
	}
	if strings.Join(f.xattrs, ",") != "Fotky "+shares.XattrAccess+",Fotky "+shares.XattrDefault {
		t.Fatalf("xattrs %v", f.xattrs)
	}
	if fi, _ := os.Stat(f.vol + "/Fotky"); fi.Mode().Perm() != 0o770 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if len(f.chowns) != 1 || f.chowns[0] != "Fotky 0:0" {
		t.Fatalf("chowns %v", f.chowns)
	}
	if strings.Join(l.Principals.Users, ",") != "jana,petr" || !strings.Contains(strings.Join(l.Principals.Groups, ","), "rodina") {
		t.Fatalf("principals %+v", l.Principals)
	}
	eff := map[string]string{}
	for _, e := range l.Shares[0].Effective {
		eff[e.User] = e.Access
	}
	if eff["jana"] != "rw" || eff["petr"] != "none" {
		t.Fatalf("effective %v", eff)
	}
	conf, _ := os.ReadFile(f.m.SambaConf)
	if !strings.Contains(string(conf), "inherit acls = yes") || !strings.Contains(string(conf), "[Fotky]") {
		t.Fatalf("conf %s", conf)
	}
	if l.Smb.Installed || l.Smb.Included {
		t.Fatalf("smb %+v", l.Smb)
	}
}

func TestShareCreateFallbackMode(t *testing.T) {
	f := newShareFix(t, false)
	l, err := f.m.Create(context.Background(), fotky(f.vol), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if l.Shares[0].Enforcement != "smb" || len(f.xattrs) != 0 {
		t.Fatalf("%+v xattrs %v", l.Shares[0], f.xattrs)
	}
	fi, _ := os.Stat(f.vol + "/Fotky")
	if fi.Mode()&os.ModeSetgid != 0 || fi.Mode().Perm() != 0o770 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if f.chowns[0] != "Fotky 0:990" {
		t.Fatalf("chowns %v", f.chowns)
	}
	conf, _ := os.ReadFile(f.m.SambaConf)
	if !strings.Contains(string(conf), "force group = nas-users") || strings.Contains(string(conf), "inherit acls") {
		t.Fatalf("conf %s", conf)
	}
}

func TestShareAdoptFallbackRecurses(t *testing.T) {
	f := newShareFix(t, false)
	os.MkdirAll(f.vol+"/Fotky/sub", 0o700)
	os.WriteFile(f.vol+"/Fotky/sub/a", nil, 0o600)
	if _, err := f.m.Create(context.Background(), fotky(f.vol), "admin"); err != nil {
		t.Fatal(err)
	}
	if len(f.chowns) != 4 { // root + root again in walk + sub + a
		t.Fatalf("chowns %v", f.chowns)
	}
	if fi, _ := os.Stat(f.vol + "/Fotky/sub/a"); fi.Mode().Perm() != 0o660 {
		t.Fatalf("file mode %v", fi.Mode())
	}
}

func TestShareAdoptACLKeepsExistingFolderOnFailure(t *testing.T) {
	f := newShareFix(t, true)
	os.Mkdir(f.vol+"/Fotky", 0o755)
	f.m.SetXattr = func(string, string, []byte) error { return os.ErrPermission }
	if _, err := f.m.Create(context.Background(), fotky(f.vol), "admin"); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(f.vol + "/Fotky"); err != nil {
		t.Fatal("adopted folder removed")
	}
	if _, err := os.Stat(f.m.Path); err == nil {
		t.Fatal("config saved after failure")
	}
}

func TestShareCreateFailureRemovesNewFolder(t *testing.T) {
	f := newShareFix(t, true)
	f.m.SetXattr = func(string, string, []byte) error { return os.ErrPermission }
	if _, err := f.m.Create(context.Background(), fotky(f.vol), "admin"); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(f.vol + "/Fotky"); err == nil {
		t.Fatal("new folder left behind")
	}
}

func TestShareCreateValidation(t *testing.T) {
	f := newShareFix(t, true)
	ctx := context.Background()
	if _, err := f.m.Create(ctx, fotky(f.vol), "admin"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(s *shares.Share){
		"duplicate name":   func(s *shares.Share) { s.Name = "fotky" },
		"same folder":      func(s *shares.Share) { s.Name = "Jine"; s.Folder = "fotky" },
		"unmounted volume": func(s *shares.Share) { s.Name = "X"; s.Volume = f.dir + "/other" },
		"system disk":      func(s *shares.Share) { s.Name = "X"; s.Volume = f.dir },
		"unknown user": func(s *shares.Share) {
			s.Name = "X"
			s.Permissions = []shares.Permission{{Kind: "user", Name: "nobody", Access: "ro"}}
		},
		"reserved": func(s *shares.Share) { s.Name = "IPC$" },
		"sudo group": func(s *shares.Share) {
			s.Name = "X"
			s.Permissions = []shares.Permission{{Kind: "group", Name: "sudo", Access: "ro"}}
		},
	}
	for n, mod := range cases {
		s := fotky(f.vol)
		mod(&s)
		_, err := f.m.Create(ctx, s, "admin")
		he, ok := AsError(err)
		if !ok || he.Code != CodeInvalid {
			t.Errorf("%s: err = %v", n, err)
		}
	}
	_, err := f.m.Create(ctx, shares.Share{Name: "Y", Volume: f.dir + "/other"}, "admin")
	if he, _ := AsError(err); he == nil || !strings.Contains(he.Message, "není připojený") {
		t.Errorf("message %v", err)
	}
	if _, err := os.Stat(f.dir + "/other"); err == nil {
		t.Error("folder created on an unmounted volume")
	}
}

func TestShareUpdateAndImmutable(t *testing.T) {
	f := newShareFix(t, true)
	ctx := context.Background()
	f.m.Create(ctx, fotky(f.vol), "admin")
	f.xattrs = nil
	s := fotky(f.vol)
	s.Description = "Nový popis"
	s.Hidden = true
	s.Permissions = []shares.Permission{{Kind: "group", Name: "nas-users", Access: "ro"}}
	l, err := f.m.Update(ctx, s, "admin")
	if err != nil || l.Shares[0].Description != "Nový popis" || !l.Shares[0].Hidden || len(f.xattrs) != 2 {
		t.Fatalf("%+v %v %v", l.Shares, err, f.xattrs)
	}
	s.Folder = "Jina"
	if _, err := f.m.Update(ctx, s, "admin"); err == nil {
		t.Error("folder change accepted")
	}
	s = fotky(f.vol)
	s.Name = "Neni"
	if _, err := f.m.Update(ctx, s, "admin"); err == nil {
		t.Error("unknown share accepted")
	} else if he, _ := AsError(err); he.Code != CodeNotFound {
		t.Error(err)
	}
}

func TestShareDelete(t *testing.T) {
	f := newShareFix(t, true)
	ctx := context.Background()
	f.m.Create(ctx, fotky(f.vol), "admin")
	os.WriteFile(f.vol+"/Fotky/a.txt", []byte("x"), 0o644)

	// Wrong confirmation: nothing changes.
	_, err := f.m.Delete(ctx, ShareDelete{Name: "Fotky", DeleteData: true, ConfirmName: "fotky"}, "admin")
	if he, _ := AsError(err); he == nil || he.Field != "confirmName" || he.Message != "Pro smazání dat napište přesný název složky." {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(f.vol + "/Fotky/a.txt"); err != nil {
		t.Fatal("data touched")
	}
	// Config only keeps the folder.
	f.m.Create(ctx, shares.Share{Name: "Docs", Volume: f.vol}, "admin")
	r, err := f.m.Delete(ctx, ShareDelete{Name: "Docs"}, "admin")
	if err != nil || r.DataRemoval != "none" || len(r.Shares) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(f.vol + "/Docs"); err != nil {
		t.Fatal("folder removed on config-only delete")
	}
	// With data.
	r, err = f.m.Delete(ctx, ShareDelete{Name: "Fotky", DeleteData: true, ConfirmName: "Fotky"}, "admin")
	if err != nil || r.DataRemoval != "started" || len(r.Shares) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	ents, _ := os.ReadDir(f.vol)
	for _, e := range ents {
		if e.Name() == "Fotky" || strings.HasPrefix(e.Name(), ".nas-revival-deleted-") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
	conf, _ := os.ReadFile(f.m.SambaConf)
	if strings.Contains(string(conf), "[Fotky]") {
		t.Fatal("share still in Samba fragment")
	}
	if _, err := f.m.Delete(ctx, ShareDelete{Name: "Fotky"}, "admin"); err == nil {
		t.Fatal("deleting twice")
	}
}

func TestShareDeleteRefusesSymlink(t *testing.T) {
	f := newShareFix(t, true)
	f.m.Create(context.Background(), fotky(f.vol), "admin")
	os.RemoveAll(f.vol + "/Fotky")
	target := t.TempDir()
	os.Symlink(target, f.vol+"/Fotky")
	_, err := f.m.Delete(context.Background(), ShareDelete{Name: "Fotky", DeleteData: true, ConfirmName: "Fotky"}, "admin")
	if err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("target removed")
	}
}

func TestShareSambaValidatedAndReloaded(t *testing.T) {
	f := newShareFix(t, true)
	f.smbd = true
	os.WriteFile(f.m.SambaMain, []byte("[global]\n   include = /etc/samba/nas-revival-shares.conf\n"), 0o644)
	l, err := f.m.Create(context.Background(), fotky(f.vol), "admin")
	if err != nil || !l.Smb.Installed || !l.Smb.Included {
		t.Fatalf("%+v %v", l.Smb, err)
	}
	got := strings.Join(f.runs, "|")
	if !strings.Contains(got, "testparm -s ") || !strings.Contains(got, "systemctl is-active --quiet smbd") ||
		!strings.Contains(got, "smbcontrol smbd reload-config") {
		t.Fatalf("runs %s", got)
	}
	// A config testparm rejects changes nothing.
	f.m.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "testparm" {
			return nil, os.ErrInvalid
		}
		return nil, nil
	}
	if _, err := f.m.Create(context.Background(), shares.Share{Name: "Dva", Volume: f.vol}, "admin"); err == nil {
		t.Fatal("rejected config accepted")
	}
	cfg, _ := shares.Load(f.m.Path)
	if len(cfg.Shares) != 1 {
		t.Fatalf("config = %+v", cfg)
	}
	if _, err := os.Stat(f.vol + "/Dva"); err == nil {
		t.Fatal("folder not cleaned up")
	}
}

func TestShareRecycleCleanupOp(t *testing.T) {
	f := newShareFix(t, true)
	f.m.Create(context.Background(), fotky(f.vol), "admin")
	bin := f.vol + "/Fotky/.recycle/petr"
	os.MkdirAll(bin, 0o755)
	p := bin + "/old"
	os.WriteFile(p, []byte("12345"), 0o644)
	old := f.m.Now().Add(-20 * 24 * time.Hour)
	os.Chtimes(p, old, old)
	d := &Dispatcher{Shares: f.m}
	res, err := d.Handle(context.Background(), request{Op: "share.recycleCleanup", User: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(shares.RecycleResult)
	if r.RemovedFiles != 1 || r.RemovedBytes != 5 {
		t.Fatalf("%+v", r)
	}
}

func TestShareDispatcherAndRestore(t *testing.T) {
	f := newShareFix(t, true)
	d := &Dispatcher{Shares: f.m}
	call := func(op string, args any) (any, error) {
		raw, _ := json.Marshal(args)
		return d.Handle(context.Background(), request{Op: op, Args: raw, User: "admin"})
	}
	if _, err := call("share.create", map[string]any{"share": fotky(f.vol)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.m.Path)
	if _, err := call("share.delete", map[string]any{"name": "Fotky"}); err != nil {
		t.Fatal(err)
	}
	f.xattrs = nil
	if err := f.m.Restore(context.Background(), before, true); err != nil {
		t.Fatal(err)
	}
	l, _ := f.m.List(context.Background())
	if len(l.Shares) != 1 || len(f.xattrs) != 2 {
		t.Fatalf("%+v xattrs %v", l.Shares, f.xattrs)
	}
	if err := f.m.Restore(context.Background(), []byte(`{"version":1,"shares":[{"name":"a b"}]}`), true); err == nil {
		t.Fatal("invalid restore accepted")
	}
	if err := f.m.Restore(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	if l, _ := f.m.List(context.Background()); len(l.Shares) != 0 {
		t.Fatal("restore of absent file kept shares")
	}
	if _, err := call("share.update", map[string]any{"bogus": 1}); err == nil {
		t.Fatal("unknown arg accepted")
	}
	if _, err := (&Dispatcher{}).Handle(context.Background(), request{Op: "share.list"}); err == nil {
		t.Fatal("share op without manager")
	}
}

func TestBackupAccountMatchesTasks(t *testing.T) {
	if shares.BackupAccount != tasks.BackupUser {
		t.Fatalf("%q != %q", shares.BackupAccount, tasks.BackupUser)
	}
}

func backupShare(vol string) shares.Share {
	return shares.Share{Name: "zalohy", Volume: vol, BackupTarget: true,
		Permissions: []shares.Permission{{Kind: "group", Name: "nas-users", Access: "ro"}}}
}

func TestShareBackupTargetCreate(t *testing.T) {
	f := newShareFix(t, true)
	f.m.BackupVolume = f.vol
	ctx := context.Background()
	l, err := f.m.Create(ctx, backupShare(f.vol), "admin")
	if err != nil || !l.Shares[0].BackupTarget || !l.Shares[0].ReadOnly || l.Shares[0].Recycle.Enabled {
		t.Fatalf("%+v %v", l.Shares, err)
	}
	// rw permission refused
	s := backupShare(f.vol)
	s.Name = "Druha"
	s.Permissions[0].Access = "rw"
	if _, err := f.m.Create(ctx, s, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "jen oprávnění ke čtení") {
		t.Errorf("rw permission: %v", err)
	}
	// other volume refused
	f.m.BackupVolume = "/srv/jiny"
	s = backupShare(f.vol)
	s.Name = "Treti"
	if _, err := f.m.Create(ctx, s, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "datovém svazku") {
		t.Errorf("volume: %v", err)
	}
}

func TestShareBackupTargetNeedsACL(t *testing.T) {
	f := newShareFix(t, false)
	f.m.BackupVolume = f.vol
	if _, err := f.m.Create(context.Background(), backupShare(f.vol), "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("no ACL volume: %v", err)
	}
}

func TestShareBackupTargetNfs(t *testing.T) {
	f := newShareFix(t, true)
	f.m.BackupVolume = f.vol
	ctx := context.Background()
	if _, err := f.m.Create(ctx, shares.Share{Name: "Docs", Volume: f.vol}, "admin"); err != nil {
		t.Fatal(err)
	}
	f.m.NfsExported = func(n string) bool { return n == "Docs" }
	on := shares.Share{Name: "Docs", Volume: f.vol, BackupTarget: true}
	if _, err := f.m.Update(ctx, on, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "NFS") {
		t.Fatalf("backupTarget on exported share: %v", err)
	}
	f.m.NfsExported = func(string) bool { return false }
	if _, err := f.m.Update(ctx, on, "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestShareBackupTargetInUse(t *testing.T) {
	f := newShareFix(t, true)
	f.m.BackupVolume = f.vol
	ctx := context.Background()
	if _, err := f.m.Create(ctx, backupShare(f.vol), "admin"); err != nil {
		t.Fatal(err)
	}
	f.m.InUseAsBackupTarget = func(n string) (string, bool) { return "úloha „Server“", n == "zalohy" }
	off := shares.Share{Name: "zalohy", Volume: f.vol, Permissions: []shares.Permission{}}
	if _, err := f.m.Update(ctx, off, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "úloha „Server“") {
		t.Fatalf("turn off: %v", err)
	}
	if _, err := f.m.Delete(ctx, ShareDelete{Name: "zalohy"}, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "cíl záloh") {
		t.Fatalf("delete: %v", err)
	}
	// Changing something else while it stays a target is fine.
	keep := backupShare(f.vol)
	keep.Description = "x"
	if _, err := f.m.Update(ctx, keep, "admin"); err != nil {
		t.Fatal(err)
	}
	f.m.InUseAsBackupTarget = func(string) (string, bool) { return "", false }
	if _, err := f.m.Update(ctx, off, "admin"); err != nil {
		t.Fatal(err)
	}
}
