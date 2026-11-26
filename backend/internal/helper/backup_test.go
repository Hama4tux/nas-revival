package helper

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/tasks"
)

func keyBlob(typ string) string {
	b := []byte{0, 0, 0, byte(len(typ))}
	b = append(b, typ...)
	b = append(b, 1, 2, 3, 4)
	return base64.StdEncoding.EncodeToString(b)
}

type bkFix struct {
	m      *BackupManager
	dir    string
	calls  []string
	chowns []string
	out    map[string]string // call prefix -> stdout
	fail   map[string]error
}

func newBkFix(t *testing.T) *bkFix {
	t.Helper()
	dir := t.TempDir()
	x := &bkFix{dir: dir, out: map[string]string{}, fail: map[string]error{}}
	x.m = &BackupManager{Iface: "enp1s0", KeyPath: filepath.Join(dir, "home", ".ssh", "id_ed25519"),
		KnownHosts: filepath.Join(dir, "known"), ConfigPath: filepath.Join(dir, "backup.json"), TempDir: dir,
		Lookup:   func(string) (uint32, int, error) { return 990, 990, nil },
		Chown:    func(p string, u, g int) error { x.chowns = append(x.chowns, filepath.Base(p)); return nil },
		Hostname: func() (string, error) { return "nas;rm -rf", nil }}
	x.m.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		x.calls = append(x.calls, call)
		if name == "ssh-keygen" && len(args) > 0 && args[0] == "-q" {
			f := args[len(args)-1]
			os.WriteFile(f, []byte("PRIVATE"), 0o644)
			os.WriteFile(f+".pub", []byte("ssh-ed25519 AAAAkey nas-backup@nas\n"), 0o600)
		}
		for p, e := range x.fail {
			if strings.HasPrefix(call, p) {
				return nil, e
			}
		}
		for p, o := range x.out {
			if strings.HasPrefix(call, p) {
				return []byte(o), nil
			}
		}
		if name == "ssh-keygen" {
			return []byte("256 SHA256:abcdef nas-backup@nas (ED25519)\n"), nil
		}
		return nil, nil
	}
	return x
}

func TestBackupKeyGenerateAndHint(t *testing.T) {
	x := newBkFix(t)
	x.out["ip "] = `[{"addr_info":[{"family":"inet","local":"192.0.2.5","prefixlen":24,"scope":"global"}]}]`
	k, err := x.m.KeyGet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if k.PublicKey != "ssh-ed25519 AAAAkey nas-backup@nas" || k.Fingerprint != "SHA256:abcdef" {
		t.Fatalf("%+v", k)
	}
	want := `from="192.0.2.5",restrict,command="rrsync -ro /var/backups" ssh-ed25519 AAAAkey nas-backup@nas`
	if k.AuthorizedKeysHint != want {
		t.Fatalf("%s", k.AuthorizedKeysHint)
	}
	gen := x.calls[0]
	if gen != "ssh-keygen -q -t ed25519 -N  -C nas-backup@nasrm-rf -f "+x.m.KeyPath {
		t.Fatalf("%q", gen)
	}
	st, _ := os.Stat(x.m.KeyPath)
	pst, _ := os.Stat(x.m.KeyPath + ".pub")
	dst, _ := os.Stat(filepath.Dir(x.m.KeyPath))
	if st.Mode().Perm() != 0o600 || pst.Mode().Perm() != 0o644 || dst.Mode().Perm() != 0o700 {
		t.Fatal(st.Mode(), pst.Mode(), dst.Mode())
	}
	if len(x.chowns) != 3 {
		t.Fatal(x.chowns)
	}
	// second call: key exists, no generation
	n := len(x.calls)
	if _, err := x.m.KeyGet(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range x.calls[n:] {
		if strings.HasPrefix(c, "ssh-keygen -q") {
			t.Fatal("regenerated")
		}
	}
}

func TestBackupKeyNotInstalledAndNoIP(t *testing.T) {
	x := newBkFix(t)
	x.m.Lookup = func(string) (uint32, int, error) { return 0, 0, errors.New("no user") }
	_, err := x.m.KeyGet(context.Background())
	if e, ok := err.(*Error); !ok || e.Code != CodeNotInstalled || e.Message != "Zálohování není nainstalované (chybí účet nas-backup)." {
		t.Fatalf("%v", err)
	}
	x = newBkFix(t)
	k, err := x.m.KeyGet(context.Background())
	if err != nil || strings.Contains(k.AuthorizedKeysHint, "from=") {
		t.Fatalf("%+v %v", k, err)
	}
}

func TestHostValidationRejectsOptions(t *testing.T) {
	x := newBkFix(t)
	ctx := context.Background()
	for _, h := range []string{"-oProxyCommand=x", "a b", "", "host;x"} {
		if _, err := x.m.HostScan(ctx, h, 22); err == nil {
			t.Errorf("scan %q", h)
		}
		if err := x.m.HostTrust(ctx, h, 22, "ssh-ed25519", keyBlob("ssh-ed25519")); err == nil {
			t.Errorf("trust %q", h)
		}
		if _, err := x.m.Test(ctx, h, 22, "root", "a"); err == nil {
			t.Errorf("test %q", h)
		}
	}
	if _, err := x.m.Test(ctx, "192.0.2.1", 22, "-oFoo", "a"); err == nil {
		t.Error("user")
	}
	for _, p := range []string{"../x", "/etc", "a/../b", ""} {
		if _, err := x.m.Test(ctx, "192.0.2.1", 22, "root", p); err == nil {
			t.Errorf("path %q", p)
		}
	}
	if _, err := x.m.HostScan(ctx, "192.0.2.1", 70000); err == nil {
		t.Error("port")
	}
	if len(x.calls) != 0 {
		t.Fatalf("commands ran: %v", x.calls)
	}
}

func TestHostScanArgvAndKnownChanged(t *testing.T) {
	x := newBkFix(t)
	ed, rsa := keyBlob("ssh-ed25519"), keyBlob("ssh-rsa")
	x.out["ssh-keyscan"] = "# 192.0.2.10:2222 SSH-2.0\n[192.0.2.10]:2222 ssh-ed25519 " + ed + "\n[192.0.2.10]:2222 ssh-rsa " + rsa + "\n"
	res, err := x.m.HostScan(context.Background(), "192.0.2.10", 2222)
	if err != nil || len(res.Keys) != 2 || res.Known || res.Changed || res.Keys[0].Fingerprint != "SHA256:abcdef" {
		t.Fatalf("%+v %v", res, err)
	}
	if x.calls[0] != "ssh-keyscan -T 5 -p 2222 -t ed25519,ecdsa,rsa -- 192.0.2.10" {
		t.Fatalf("%q", x.calls[0])
	}
	if err := x.m.HostTrust(context.Background(), "192.0.2.10", 2222, "ssh-ed25519", ed); err != nil {
		t.Fatal(err)
	}
	res, _ = x.m.HostScan(context.Background(), "192.0.2.10", 2222)
	if !res.Known || res.Changed {
		t.Fatalf("%+v", res)
	}
	// server now offers another ed25519 key
	other := base64.StdEncoding.EncodeToString(append([]byte{0, 0, 0, 11}, append([]byte("ssh-ed25519"), 9, 9)...))
	x.out["ssh-keyscan"] = "[192.0.2.10]:2222 ssh-ed25519 " + other + "\n"
	res, _ = x.m.HostScan(context.Background(), "192.0.2.10", 2222)
	if res.Known || !res.Changed {
		t.Fatalf("%+v", res)
	}
	// nothing offered
	x.out["ssh-keyscan"] = ""
	if _, err := x.m.HostScan(context.Background(), "192.0.2.10", 2222); err == nil {
		t.Fatal("empty scan accepted")
	}
}

func TestHostTrustRewrite(t *testing.T) {
	x := newBkFix(t)
	ctx := context.Background()
	ed, rsa := keyBlob("ssh-ed25519"), keyBlob("ssh-rsa")
	os.WriteFile(x.m.KnownHosts, []byte("# keep\nother.example.lan ssh-ed25519 AAAAx\n192.0.2.10 ssh-rsa OLD\n192.0.2.10 ssh-ed25519 OLD2\n[192.0.2.10]:2222 ssh-rsa KEEP\n"), 0o644)
	if err := x.m.HostTrust(ctx, "192.0.2.10", 22, "ssh-ed25519", ed); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(x.m.KnownHosts)
	want := "# keep\nother.example.lan ssh-ed25519 AAAAx\n[192.0.2.10]:2222 ssh-rsa KEEP\n192.0.2.10 ssh-ed25519 " + ed + "\n"
	if string(b) != want {
		t.Fatalf("%q", b)
	}
	if err := x.m.HostTrust(ctx, "Backup.Example.LAN", 2200, "ssh-rsa", rsa); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(x.m.KnownHosts)
	if !strings.HasSuffix(string(b), "[backup.example.lan]:2200 ssh-rsa "+rsa+"\n") {
		t.Fatalf("%q", b)
	}
	st, _ := os.Stat(x.m.KnownHosts)
	if st.Mode().Perm() != 0o644 {
		t.Fatal(st.Mode())
	}
	for name, c := range map[string][2]string{
		"type":     {"ssh-dss", ed},
		"b64":      {"ssh-ed25519", "!!!"},
		"mismatch": {"ssh-ed25519", rsa},
		"long":     {"ssh-ed25519", strings.Repeat("A", 2000)},
		"inject":   {"ssh-ed25519", ed + "\nevil ssh-rsa x"},
	} {
		if err := x.m.HostTrust(ctx, "192.0.2.10", 22, c[0], c[1]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBackupTestArgv(t *testing.T) {
	x := newBkFix(t)
	x.out["runuser -u nas-backup -- rsync"] = "drwxr-xr-x          4,096 2026/11/26 06:00:00 .\n-rw-r--r--          12 2026/11/26 06:00:00 last ok\ndrwxr-xr-x 4,096 2026/11/26 06:00:00 sub\n"
	x.out["runuser -u nas-backup -- ssh"] = "nr-shell-check\n"
	r, err := x.m.Test(context.Background(), "2001:db8::1", 2222, "root", "nas-export")
	if err != nil || !r.OK || !r.FullAccess || len(r.Entries) != 2 || r.Entries[0] != "last ok" {
		t.Fatalf("%+v %v", r, err)
	}
	ssh := "ssh -i " + x.m.KeyPath + " -p 2222 -o BatchMode=yes -o ConnectTimeout=20 -o StrictHostKeyChecking=yes -o UserKnownHostsFile=" + x.m.KnownHosts
	if x.calls[0] != "runuser -u nas-backup -- rsync --list-only --timeout=20 -e "+ssh+" -- root@[2001:db8::1]:nas-export/" {
		t.Fatalf("%q", x.calls[0])
	}
	if x.calls[1] != "runuser -u nas-backup -- "+ssh+" root@2001:db8::1 -- echo nr-shell-check" {
		t.Fatalf("%q", x.calls[1])
	}
	// restricted key: other output
	x.out["runuser -u nas-backup -- ssh"] = ""
	if r, _ := x.m.Test(context.Background(), "192.0.2.10", 22, "root", "a"); !r.OK || r.FullAccess {
		t.Fatalf("%+v", r)
	}
	// failure
	x.fail["runuser -u nas-backup -- rsync"] = &exec.ExitError{Stderr: []byte("Host key verification failed.")}
	if r, err := x.m.Test(context.Background(), "192.0.2.10", 22, "root", "a"); err != nil || r.OK || r.Message != tasks.MsgHostKey {
		t.Fatalf("%+v %v", r, err)
	}
	x.m.Lookup = func(string) (uint32, int, error) { return 0, 0, errors.New("x") }
	if _, err := x.m.Test(context.Background(), "192.0.2.10", 22, "root", "a"); err == nil {
		t.Fatal("no account accepted")
	}
}

func TestBackupShareSwitch(t *testing.T) {
	x := newBkFix(t)
	ctx := context.Background()
	if x.m.ShareEnabled() {
		t.Fatal("default must be off")
	}
	applied := 0
	x.m.ApplyShare = func(context.Context) error { applied++; return nil }
	st, err := x.m.ShareSet(ctx, true, "admin")
	if err != nil || !st.Enabled || st.Name != "zalohy" || st.Path != "/srv/data/versions" || applied != 1 || !x.m.ShareEnabled() {
		t.Fatalf("%+v %v", st, err)
	}
	// failing Samba -> previous state restored
	x.m.ApplyShare = func(context.Context) error { return internalErr("x") }
	if _, err := x.m.ShareSet(ctx, false, "admin"); err == nil || !x.m.ShareEnabled() {
		t.Fatal("not rolled back")
	}
	x.m.ApplyShare = nil
	x.m.ShareSet(ctx, false, "admin")
	if g, _ := x.m.ShareGet(ctx); g.Enabled {
		t.Fatal("still on")
	}
	os.WriteFile(x.m.ConfigPath, []byte(`{"version":1,"smbShare":true,"x":1}`), 0o644)
	if x.m.ShareEnabled() {
		t.Fatal("unknown field accepted")
	}
}

func TestSambaFragmentIncludesReadOnlyBackupShare(t *testing.T) {
	dir := t.TempDir()
	m := &ShareManager{Path: filepath.Join(dir, "shares.json"), SambaDir: dir, SambaConf: filepath.Join(dir, "frag.conf"),
		LookPath: func(string) (string, error) { return "", errors.New("none") }, Probe: func(string) bool { return true }}
	on := true
	m.BackupShare = func() bool { return on }
	if err := m.Resamba(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.SambaConf)
	s := string(b)
	if !strings.Contains(s, "[zalohy]") || !strings.Contains(s, "read only = yes") || !strings.Contains(s, "path = /srv/data/versions") ||
		strings.Contains(s, "write list") || strings.Contains(s, "admin users") || !strings.Contains(s, "valid users = @nas-admin") {
		t.Fatalf("%s", s)
	}
	on = false
	m.Resamba(context.Background())
	b, _ = os.ReadFile(m.SambaConf)
	if strings.Contains(string(b), "zalohy") {
		t.Fatal("share still rendered")
	}
	_ = shares.BackupShareName
}

func TestDispatchBackupOps(t *testing.T) {
	x := newBkFix(t)
	d := &Dispatcher{Backup: x.m}
	ctx := context.Background()
	if _, err := d.Handle(ctx, request{Op: "backup.test", Args: []byte(`{"host":"-oX","port":22,"user":"root","path":"a"}`)}); err == nil {
		t.Fatal("option host accepted")
	}
	if _, err := d.Handle(ctx, request{Op: "backup.share.set", Args: []byte(`{}`)}); err == nil {
		t.Fatal("missing enabled accepted")
	}
	if _, err := d.Handle(ctx, request{Op: "backup.host.scan", Args: []byte(`{"host":"a","evil":1}`)}); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := d.Handle(ctx, request{Op: "backup.nope"}); err == nil {
		t.Fatal("unknown op")
	}
}
