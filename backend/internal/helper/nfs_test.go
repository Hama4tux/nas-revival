package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
)

type fakeNfsd struct {
	active        bool
	rejectExports bool // exportfs -ra fails while the exports file holds this text
	rejectText    string
	startFails    bool
	calls         []string
	exportsPath   string
}

func (f *fakeNfsd) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	if name == "systemctl" && args[0] == "is-active" {
		if f.active {
			return nil, nil
		}
		return nil, errors.New("exit 3")
	}
	f.calls = append(f.calls, call)
	switch {
	case name == "exportfs":
		b, _ := os.ReadFile(f.exportsPath)
		if f.rejectExports && strings.Contains(string(b), f.rejectText) {
			return []byte("exportfs: bad"), errors.New("exit 1")
		}
	case name == "systemctl" && args[0] == "enable":
		if f.startFails {
			return nil, errors.New("exit 1")
		}
		f.active = true
	case name == "systemctl" && args[0] == "disable":
		f.active = false
	}
	return nil, nil
}

type nfsFix struct {
	m      *NfsManager
	f      *fakeNfsd
	shares string
	fsys   string
}

func newNfsFix(t *testing.T, installed bool) *nfsFix {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "passwd"), []byte(basePasswd), 0o644)
	os.WriteFile(filepath.Join(dir, "group"), []byte(baseGroup), 0o644)
	sh := filepath.Join(dir, "shares.json")
	cfg := shares.Config{Version: 1, Shares: []shares.Share{
		{Name: "zalohy", Volume: "/srv/data", Folder: "zalohy", Permissions: []shares.Permission{{Kind: shares.KindGroup, Name: "nas-users", Access: shares.AccessRW}}},
		{Name: "archiv", Volume: "/srv/data", Folder: "archiv", ReadOnly: true, Permissions: []shares.Permission{}},
	}}
	if err := shares.Save(sh, cfg); err != nil {
		t.Fatal(err)
	}
	exp := filepath.Join(dir, "exports.d", "nas-revival.exports")
	f := &fakeNfsd{exportsPath: exp}
	fsys := filepath.Join(dir, "filesystems")
	os.WriteFile(fsys, []byte("nodev\tsysfs\n\text4\n"), 0o644)
	m := &NfsManager{Path: filepath.Join(dir, "nfs.json"), Exports: exp, Shares: sh,
		Accounts:   accounts.Paths{Passwd: filepath.Join(dir, "passwd"), Group: filepath.Join(dir, "group")},
		ClientsDir: filepath.Join(dir, "clients"), Filesystems: fsys, Run: f.run,
		LookPath: func(string) (string, error) {
			if installed {
				return "/usr/sbin/exportfs", nil
			}
			return "", errors.New("not found")
		}}
	return &nfsFix{m: m, f: f, shares: sh, fsys: fsys}
}

func proxmox(enabled bool) nfs.Settings {
	return nfs.Settings{Enabled: enabled, Exports: []nfs.Export{
		{Share: "zalohy", Clients: []nfs.Client{{Host: "192.168.1.30"}}},
	}}
}

func TestNfsGetUnconfigured(t *testing.T) {
	x := newNfsFix(t, true)
	st, err := x.m.Get(context.Background())
	if err != nil || !st.Installed || st.Settings.Enabled || st.Running || len(st.Shares) != 2 || len(st.Missing) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	// Sorted by name like shares.json.
	if st.Shares[1].Path != "/srv/data/zalohy" || !st.Shares[0].ReadOnly || !st.Shares[1].GroupWrite || st.Shares[0].GroupWrite {
		t.Fatalf("shares %+v", st.Shares)
	}
}

func TestNfsSetEnableAndDisable(t *testing.T) {
	x := newNfsFix(t, true)
	ctx := context.Background()
	st, err := x.m.Set(ctx, proxmox(true), "admin")
	if err != nil || !st.Running || !st.Managed || !st.Settings.Enabled {
		t.Fatalf("%+v %v", st, err)
	}
	b, _ := os.ReadFile(x.m.Exports)
	// anongid is the nas-users group of the account database (990).
	if !strings.Contains(string(b), "/srv/data/zalohy 192.168.1.30(rw,sync,secure,no_subtree_check,sec=sys,root_squash,anonuid=65534,anongid=990)") {
		t.Fatalf("exports:\n%s", b)
	}
	if got := strings.Join(x.f.calls, "|"); got != "systemctl enable --now nfs-server.service|exportfs -ra" {
		t.Fatalf("calls %s", got)
	}
	x.f.calls = nil
	st, err = x.m.Set(ctx, proxmox(false), "admin")
	if err != nil || st.Running || !st.Managed {
		t.Fatalf("%+v %v", st, err)
	}
	if b, _ := os.ReadFile(x.m.Exports); strings.Contains(string(b), "/srv") {
		t.Fatalf("disabled NFS still exports:\n%s", b)
	}
	if got := strings.Join(x.f.calls, "|"); got != "systemctl disable --now nfs-server.service" {
		t.Fatalf("calls %s", got)
	}
}

func TestNfsSetRejections(t *testing.T) {
	ctx := context.Background()
	x := newNfsFix(t, false)
	if _, err := x.m.Set(ctx, proxmox(true), "admin"); codeOf(err) != CodeInvalid {
		t.Fatalf("not installed: %v", err)
	}
	x = newNfsFix(t, true)
	s := proxmox(true)
	s.Exports[0].Share = "neni"
	if _, err := x.m.Set(ctx, s, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "neni") {
		t.Fatalf("missing share: %v", err)
	}
	s = proxmox(true)
	s.Exports[0].Clients[0].Host = "*"
	if _, err := x.m.Set(ctx, s, "admin"); codeOf(err) != CodeInvalid {
		t.Fatalf("wildcard: %v", err)
	}
	if len(x.f.calls) != 0 || fileExists(x.m.Path) || fileExists(x.m.Exports) {
		t.Fatalf("rejected settings changed something: %v", x.f.calls)
	}
}

func TestNfsSetRollsBackWhenExportsRefused(t *testing.T) {
	x := newNfsFix(t, true)
	ctx := context.Background()
	if _, err := x.m.Set(ctx, proxmox(true), "admin"); err != nil {
		t.Fatal(err)
	}
	oldJSON, _ := os.ReadFile(x.m.Path)
	oldExp, _ := os.ReadFile(x.m.Exports)
	x.f.rejectExports, x.f.rejectText = true, "192.168.1.31"
	s := proxmox(true)
	s.Exports[0].Clients[0].Host = "192.168.1.31"
	if _, err := x.m.Set(ctx, s, "admin"); codeOf(err) != CodeInternal {
		t.Fatalf("want internal error, got %v", err)
	}
	if b, _ := os.ReadFile(x.m.Path); string(b) != string(oldJSON) {
		t.Fatalf("nfs.json not restored:\n%s", b)
	}
	if b, _ := os.ReadFile(x.m.Exports); string(b) != string(oldExp) {
		t.Fatalf("exports not restored:\n%s", b)
	}
	if !x.f.active {
		t.Fatal("server should keep running with the old exports")
	}
}

func TestNfsSetFirstStartFailsRestoresNothing(t *testing.T) {
	x := newNfsFix(t, true)
	x.f.startFails = true
	_, err := x.m.Set(context.Background(), proxmox(true), "admin")
	if codeOf(err) != CodeInternal || !strings.Contains(err.Error(), "NFSD") {
		t.Fatalf("kernel without nfsd: %v", err)
	}
	if fileExists(x.m.Path) || fileExists(x.m.Exports) {
		t.Fatal("files left behind after a failed first start")
	}
	// With nfsd in the kernel the message is the generic one.
	os.WriteFile(x.fsys, []byte("nodev\tnfsd\n"), 0o644)
	if _, err := x.m.Set(context.Background(), proxmox(true), "admin"); err == nil || strings.Contains(err.Error(), "NFSD") {
		t.Fatalf("kernel with nfsd: %v", err)
	}
}

func TestNfsRefreshAndExported(t *testing.T) {
	x := newNfsFix(t, true)
	ctx := context.Background()
	// Without nfs.json a share change writes nothing.
	x.m.Refresh(ctx, shares.Config{Version: 1})
	if fileExists(x.m.Exports) {
		t.Fatal("refresh created exports without NFS settings")
	}
	if _, err := x.m.Set(ctx, proxmox(true), "admin"); err != nil {
		t.Fatal(err)
	}
	if !x.m.Exported("zalohy") || x.m.Exported("archiv") {
		t.Fatal("Exported")
	}
	x.f.calls = nil
	cfg, _ := shares.Load(x.shares)
	cfg.Shares[cfg.Find("zalohy")].ReadOnly = true
	x.m.Refresh(ctx, cfg)
	if b, _ := os.ReadFile(x.m.Exports); !strings.Contains(string(b), "192.168.1.30(ro,") {
		t.Fatalf("read-only share not re-exported read-only:\n%s", b)
	}
	if strings.Join(x.f.calls, "|") != "exportfs -ra" {
		t.Fatalf("calls %v", x.f.calls)
	}
	x.f.calls = nil
	x.m.Refresh(ctx, cfg) // unchanged: no exportfs
	if len(x.f.calls) != 0 {
		t.Fatalf("unchanged refresh ran %v", x.f.calls)
	}
}

func TestNfsRestore(t *testing.T) {
	x := newNfsFix(t, true)
	ctx := context.Background()
	if _, err := x.m.Set(ctx, proxmox(true), "admin"); err != nil {
		t.Fatal(err)
	}
	// A version without nfs.json means NFS off.
	if err := x.m.Restore(ctx, nil, false); err != nil {
		t.Fatal(err)
	}
	if fileExists(x.m.Path) || x.f.active {
		t.Fatal("restore of an absent version kept NFS on")
	}
	if err := x.m.Restore(ctx, []byte(`{"version":1,"enabled":true,"exports":[{"share":"zalohy","clients":[{"host":"*"}]}]}`), true); codeOf(err) != CodeInvalid {
		t.Fatalf("invalid version restored: %v", err)
	}
	b := []byte(`{"version":1,"enabled":true,"exports":[{"share":"zalohy","clients":[{"host":"192.168.1.30"}]}]}`)
	if err := x.m.Restore(ctx, b, true); err != nil || !x.f.active {
		t.Fatalf("restore: %v", err)
	}
}

func TestNfsStatus(t *testing.T) {
	x := newNfsFix(t, true)
	st, err := x.m.Status(context.Background())
	if err != nil || len(st.Clients) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	x.f.active = true
	os.MkdirAll(filepath.Join(x.m.ClientsDir, "1"), 0o755)
	os.WriteFile(filepath.Join(x.m.ClientsDir, "1", "info"), []byte("address: \"192.168.1.30:800\"\nstatus: confirmed\nminor version: 2\n"), 0o644)
	st, err = x.m.Status(context.Background())
	if err != nil || len(st.Clients) != 1 || st.Clients[0].Address != "192.168.1.30" || st.Clients[0].Version != "4.2" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestShareDeleteBlockedWhileExported(t *testing.T) {
	f := newShareFix(t, false)
	ctx := context.Background()
	if _, err := f.m.Create(ctx, shares.Share{Name: "Docs", Volume: f.vol}, "admin"); err != nil {
		t.Fatal(err)
	}
	var refreshed int
	f.m.NfsExported = func(n string) bool { return n == "Docs" }
	f.m.NfsRefresh = func(context.Context, shares.Config) { refreshed++ }
	if _, err := f.m.Delete(ctx, ShareDelete{Name: "Docs"}, "admin"); codeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "NFS") {
		t.Fatalf("delete of exported share: %v", err)
	}
	if _, err := f.m.Update(ctx, shares.Share{Name: "Docs", ReadOnly: true}, "admin"); err != nil || refreshed != 1 {
		t.Fatalf("update: %v, refreshed %d", err, refreshed)
	}
	f.m.NfsExported = func(string) bool { return false }
	if _, err := f.m.Delete(ctx, ShareDelete{Name: "Docs"}, "admin"); err != nil {
		t.Fatal(err)
	}
}
