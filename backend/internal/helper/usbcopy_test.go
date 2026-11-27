package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/shares"
)

func usbCopyFix(t *testing.T) (*UsbCopyManager, string) {
	t.Helper()
	dir := t.TempDir()
	vol := filepath.Join(dir, "data")
	os.MkdirAll(filepath.Join(vol, "data"), 0o755)
	os.MkdirAll(filepath.Join(vol, "plain"), 0o755)
	sp := filepath.Join(dir, "shares.json")
	err := shares.Save(sp, shares.Config{Version: 1, Shares: []shares.Share{
		{Name: "data", Volume: "/srv/data", Folder: "data", ReadOnly: true, BackupTarget: true},
		{Name: "plain", Volume: "/srv/data", Folder: "plain"},
		{Name: "elsewhere", Volume: "/srv/usb1", Folder: "x", BackupTarget: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &UsbCopyManager{Path: filepath.Join(dir, "usbcopy.json"), DestPath: filepath.Join(dir, "usbcopy.dest"),
		SharesPath: sp, Volume: "/srv/data", Root: vol}, vol
}

func TestUsbCopySetAndGet(t *testing.T) {
	m, vol := usbCopyFix(t)
	ctx := context.Background()
	if i, err := m.Get(ctx); err != nil || i.Share != "" || i.Dest != "" {
		t.Fatalf("empty get: %+v %v", i, err)
	}
	i, err := m.Set(ctx, "DATA", "usb-backups", "admin")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(vol, "data", "usb-backups")
	if i.Dest != want || i.Share != "data" {
		t.Fatalf("got %+v want dest %s", i, want)
	}
	if st, err := os.Stat(want); err != nil || !st.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	if b, _ := os.ReadFile(m.DestPath); string(b) != want+"\n" {
		t.Fatalf("dest file %q", b)
	}
	if g, _ := m.Get(ctx); g.Share != "data" || g.Folder != "usb-backups" || g.Dest != want {
		t.Fatalf("get: %+v", g)
	}
	// No subfolder: the share folder itself.
	if i, err := m.Set(ctx, "data", "", "admin"); err != nil || i.Dest != filepath.Join(vol, "data") {
		t.Fatalf("no folder: %+v %v", i, err)
	}
	// Clearing removes both files.
	if _, err := m.Set(ctx, "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{m.Path, m.DestPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", p)
		}
	}
}

func TestUsbCopySetRefuses(t *testing.T) {
	m, vol := usbCopyFix(t)
	ctx := context.Background()
	os.Symlink("/tmp", filepath.Join(vol, "data", "lnk"))
	os.WriteFile(filepath.Join(vol, "data", "file"), nil, 0o644)
	cases := map[string][2]string{
		"missing share":   {"nope", ""},
		"not backup tgt":  {"plain", ""},
		"other volume":    {"elsewhere", ""},
		"dotdot":          {"data", ".."},
		"slash":           {"data", "a/b"},
		"too long":        {"data", strings.Repeat("a", 40)},
		"symlink folder":  {"data", "lnk"},
		"file as folder":  {"data", "file"},
		"folder no share": {"", "x"},
	}
	for name, c := range cases {
		if _, err := m.Set(ctx, c[0], c[1], "admin"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := os.Stat(m.DestPath); !os.IsNotExist(err) {
		t.Error("a refused set must not write the dest file")
	}
}

func TestUsbCopyOpsThroughDispatcher(t *testing.T) {
	m, _ := usbCopyFix(t)
	d := &Dispatcher{Usb: &UsbManager{Copy: m}}
	if _, err := d.handleUsb(context.Background(), request{Op: "usb.copy.set", Args: []byte(`{"share":"data","folder":"usb-backups"}`)}, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := d.handleUsb(context.Background(), request{Op: "usb.copy.get", Args: []byte(`{}`)}, "admin")
	if err != nil || r.(UsbCopyInfo).Folder != "usb-backups" {
		t.Fatalf("%+v %v", r, err)
	}
}
