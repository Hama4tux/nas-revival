package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/storage"
)

func sp(s string) *string { return &s }

// usbFixture: sdb = rescue stick (excluded UUID), sdc = FAT stick with an
// exFAT and a FAT partition, sdd = ext4 stick mounted by us, sde mounted by
// the COPY job; sda is the internal disk.
func usbFixture(t *testing.T) (*UsbManager, *[]string) {
	t.Helper()
	ex := filepath.Join(t.TempDir(), "exclude")
	os.WriteFile(ex, []byte("# rescue\nBD2334F5-c317-40e6-8b7e-f74c7456460b\n"), 0o644)
	rep := storage.Report{Disks: []storage.Disk{
		{ID: "ata-WDC", Name: "sda", Bus: "ata", Partitions: []storage.Partition{{Name: "sda3", FSType: sp("ext4"),
			Mounts: []storage.Mount{{Point: "/srv/data"}}}}},
		{ID: "usb-Cruzer", Name: "sdb", Bus: "usb", Model: sp("Cruzer Fit"), Partitions: []storage.Partition{
			{Name: "sdb1", FSType: sp("ext4"), FSUUID: sp("bd2334f5-c317-40e6-8b7e-f74c7456460b"), Label: sp("rootfs")}}},
		{ID: "usb-Kingston", Name: "sdc", Bus: "usb", Partitions: []storage.Partition{
			{Name: "sdc1", FSType: sp("exfat"), Label: sp("BIG")},
			{Name: "sdc2", FSType: sp("vfat"), Label: sp("Fotky 2026/č")}}},
		{ID: "usb-Ext", Name: "sdd", Bus: "usb", Partitions: []storage.Partition{
			{Name: "sdd1", FSType: sp("ext4"), Label: sp("data"), Mounts: []storage.Mount{{Point: "/srv/usb/data"}},
				Usage: &storage.Usage{TotalBytes: 100, UsedBytes: 40}}}},
		{ID: "usb-Copy", Name: "sde", Bus: "usb", Partitions: []storage.Partition{
			{Name: "sde1", FSType: sp("vfat"), Mounts: []storage.Mount{{Point: "/run/nsa310-copy/sde1", ReadOnly: true}}}}},
		{ID: "usb-Install", Name: "sdf", Bus: "usb", Partitions: []storage.Partition{
			{Name: "sdf1", FSType: sp("ext4"), Label: sp("nrinstall")}}},
	}}
	var runs []string
	m := &UsbManager{
		Report:  func() (storage.Report, error) { return rep, nil },
		Exclude: ex,
		DB: func() (accounts.DB, error) {
			return accounts.DB{Groups: []accounts.GroupEntry{{Name: "nas-users", GID: 990}}}, nil
		},
		Taken: func(p string) bool { return p == "/srv/usb/data" },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			runs = append(runs, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	return m, &runs
}

func TestUsbListBlocksWhatMustNotBeMounted(t *testing.T) {
	m, _ := usbFixture(t)
	l, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range l.Disks {
		for _, p := range d.Partitions {
			got[p.Name] = p.Blocked
		}
	}
	if len(l.Disks) != 5 {
		t.Fatalf("disks = %+v", l.Disks)
	}
	if _, ok := got["sda3"]; ok {
		t.Error("internal disk listed")
	}
	for name, want := range map[string]string{"sdb1": "Záchranná", "sdc1": "EXFAT", "sdc2": "", "sdd1": "", "sde1": "Připojeno jinde", "sdf1": "instalační"} {
		if g := got[name]; (want == "") != (g == "") || !strings.Contains(g, want) {
			t.Errorf("%s blocked = %q, want %q", name, g, want)
		}
	}
}

func TestUsbMountArgs(t *testing.T) {
	m, runs := usbFixture(t)
	ctx := context.Background()
	if _, err := m.Mount(ctx, "usb-Kingston", "sdc2", false, "admin"); err != nil {
		t.Fatal(err)
	}
	want := "systemd-mount --no-ask-password --collect --fsck=no -t vfat -o nosuid,nodev,noexec,noatime,uid=0,gid=990,fmask=0117,dmask=0007,utf8,shortname=mixed,flush /dev/sdc2 /srv/usb/Fotky_2026"
	if len(*runs) != 1 || (*runs)[0] != want {
		t.Errorf("run = %q", *runs)
	}
	for _, c := range []struct{ disk, part string }{
		{"usb-Cruzer", "sdb1"}, {"usb-Kingston", "sdc1"}, {"usb-Copy", "sde1"}, {"usb-Install", "sdf1"},
		{"usb-Ext", "sdd1"}, {"ata-WDC", "sda3"}, {"usb-Kingston", "sdb1"}, {"usb-Kingston", "../sda3"},
	} {
		if _, err := m.Mount(ctx, c.disk, c.part, true, "admin"); err == nil {
			t.Errorf("mount %s/%s accepted", c.disk, c.part)
		}
	}
	if len(*runs) != 1 {
		t.Errorf("runs = %q", *runs)
	}
}

func TestUsbUnmountAndEject(t *testing.T) {
	m, runs := usbFixture(t)
	ctx := context.Background()
	if _, err := m.Unmount(ctx, "usb-Copy", "sde1", "admin"); err == nil {
		t.Error("foreign mount unmounted")
	}
	if _, err := m.Eject(ctx, "usb-Copy", "admin"); code(err) != CodeUsbBusy {
		t.Errorf("eject while the COPY job reads = %v", err)
	}
	if _, err := m.Eject(ctx, "usb-Ext", "admin"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*runs, "|") != "systemd-umount --no-ask-password /srv/usb/data|systemctl start nas-revival-usb-eject@sdd.service" {
		t.Errorf("runs = %q", *runs)
	}
}

func TestMountName(t *testing.T) {
	taken := func(p string) bool { return p == "DATA" || p == "DATA-2" }
	for in, want := range map[[2]string]string{
		{"DATA", "sdc1"}:              "DATA-3",
		{"", "sdc1"}:                  "usb-sdc1",
		{"../..", "sdc1"}:             "usb-sdc1",
		{"Fotky 2026", "sdc1"}:        "Fotky_2026",
		{strings.Repeat("a", 40), ""}: strings.Repeat("a", 32),
	} {
		if g := MountName(in[0], in[1], taken); g != want {
			t.Errorf("MountName(%q) = %q, want %q", in[0], g, want)
		}
	}
}
