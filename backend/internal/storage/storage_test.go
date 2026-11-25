package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nas-revival/backend/internal/hal"
)

func fixture(t *testing.T, files map[string]string) hal.Reader {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return hal.Reader{Root: root}
}

// nsa310 mirrors the layout on the NSA310 (serials and UUIDs are invented):
// one SSHD with root, swap and data, a USB stick holding /boot, and a bind
// mount of a data subdirectory.
var nsa310 = map[string]string{
	"sys/block/sda/size":             "1953525168\n",
	"sys/block/sda/dev":              "8:0\n",
	"sys/block/sda/queue/rotational": "1\n",
	"sys/block/sda/removable":        "0\n",
	"sys/block/sda/ro":               "0\n",
	"sys/block/sda/device/model":     "ST1000LM014-SSHD\n",
	"sys/block/sda/device/vendor":    "ATA     \n",
	"sys/block/sda/device/rev":       "LVD4\n",
	"sys/block/sda/sda1/partition":   "1\n",
	"sys/block/sda/sda1/size":        "62914560\n",
	"sys/block/sda/sda1/dev":         "8:1\n",
	"sys/block/sda/sda2/partition":   "2\n",
	"sys/block/sda/sda2/size":        "2097152\n",
	"sys/block/sda/sda2/dev":         "8:2\n",
	"sys/block/sda/sda3/partition":   "3\n",
	"sys/block/sda/sda3/size":        "1888511375\n",
	"sys/block/sda/sda3/dev":         "8:3\n",
	"run/udev/data/b8:0": "S:disk/by-path/platform-f1080000.sata-ata-1\nS:disk/by-id/ata-ST1000LM014-SSHD-8GB_TEST0001\n" +
		"S:disk/by-id/wwn-0x5000c500000000aa\nE:ID_BUS=ata\nE:ID_SERIAL_SHORT=TEST0001\nE:ID_REVISION=LVD4\n" +
		"E:ID_ATA_ROTATION_RATE_RPM=5400\nE:ID_PART_TABLE_TYPE=gpt\n",
	"run/udev/data/b8:1": "E:ID_FS_TYPE=ext4\nE:ID_FS_UUID=11111111-1111-1111-1111-111111111111\nE:ID_PART_ENTRY_UUID=aaaa0001\nE:ID_PART_ENTRY_NUMBER=1\n",
	"run/udev/data/b8:2": "E:ID_FS_TYPE=swap\nE:ID_PART_ENTRY_UUID=aaaa0002\n",
	"run/udev/data/b8:3": "E:ID_FS_TYPE=ext4\nE:ID_FS_LABEL=nasdata\nE:ID_PART_ENTRY_NAME=nas-data\nE:ID_PART_ENTRY_UUID=aaaa0003\n",

	"sys/block/sdb/size":             "15633408\n",
	"sys/block/sdb/dev":              "8:16\n",
	"sys/block/sdb/queue/rotational": "1\n",
	"sys/block/sdb/removable":        "1\n",
	"sys/block/sdb/device/model":     "Cruzer Fit      \n",
	"sys/block/sdb/device/vendor":    "SanDisk'\n",
	"sys/block/sdb/sdb1/partition":   "1\n",
	"sys/block/sdb/sdb1/size":        "1048576\n",
	"sys/block/sdb/sdb1/dev":         "8:17\n",
	"run/udev/data/b8:16":            "S:disk/by-id/usb-SanDisk_Cruzer_Fit_TEST0002-0:0\nE:ID_BUS=usb\nE:ID_SERIAL_SHORT=TEST0002\nE:ID_PART_TABLE_TYPE=dos\n",
	"run/udev/data/b8:17":            "S:disk/by-id/usb-SanDisk_Cruzer_Fit_TEST0002-0:0-part1\nE:ID_FS_TYPE=ext2\n",

	// A card-reader slot without media and an MTD block device must be ignored.
	"sys/block/sdc/size":       "0\n",
	"sys/block/mtdblock0/size": "2048\n",

	// The service runs with ProtectSystem=strict, so per-mount options say "ro"
	// for everything; only the superblock options tell the real state.
	"proc/self/mountinfo": "14 1 8:1 / / ro,noatime shared:1 - ext4 /dev/root rw,errors=remount-ro\n" +
		"22 14 0:21 / /proc rw,nosuid shared:5 - proc proc rw\n" +
		"31 14 8:17 / /boot ro,noatime shared:77 - ext2 /dev/sdb1 ro\n" +
		"38 14 8:3 / /srv/data rw,noatime shared:164 - ext4 /dev/sda3 rw\n" +
		"40 14 8:3 /share /export/my\\040share ro,noatime shared:164 - ext4 /dev/sda3 rw\n",
	"proc/swaps": "Filename\tType\tSize\tUsed\tPriority\n/dev/sda2 partition 1048572 9180 -2\n/swapfile file 1024 0 -3\n",
}

func statOK(path string) (uint64, uint64, uint64, error) {
	switch path {
	case "/":
		return 30 << 30, 25 << 30, 23 << 30, nil
	case "/srv/data":
		return 900 << 30, 800 << 30, 790 << 30, nil
	case "/boot":
		return 0, 0, 0, errors.New("statfs failed")
	}
	panic("unexpected statfs of " + path)
}

func TestReportNSA310(t *testing.T) {
	r, err := Collector{HAL: fixture(t, nsa310), StatFS: statOK}.Report()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Disks) != 2 {
		t.Fatalf("disks = %d, want 2 (empty slot and mtd skipped)", len(r.Disks))
	}
	sda := r.Disks[0]
	if sda.Name != "sda" || sda.ID != "wwn-0x5000c500000000aa" || !sda.IDStable {
		t.Errorf("sda id = %q stable=%v", sda.ID, sda.IDStable)
	}
	if sda.Type != "sshd" || sda.Bus != "ata" || sda.Vendor != nil || *sda.Serial != "TEST0001" || *sda.RotationRPM != 5400 {
		t.Errorf("sda identity = %+v", sda)
	}
	if !sda.System || !sda.SharedWithData || len(sda.Roles) != 3 || sda.Roles[0] != RoleSystem || sda.Roles[1] != RoleSwap || sda.Roles[2] != RoleData {
		t.Errorf("sda roles = %v system=%v shared=%v", sda.Roles, sda.System, sda.SharedWithData)
	}
	if len(sda.Partitions) != 3 {
		t.Fatalf("sda partitions = %d", len(sda.Partitions))
	}
	root, swap, data := sda.Partitions[0], sda.Partitions[1], sda.Partitions[2]
	if root.Usage == nil || root.Usage.UsedBytes != 5<<30 || root.Mounts[0].Point != "/" || root.Mounts[0].ReadOnly {
		t.Errorf("root partition = %+v", root)
	}
	if !swap.Swap || len(swap.Mounts) != 0 || *swap.FSType != "swap" {
		t.Errorf("swap partition = %+v", swap)
	}
	if len(data.Mounts) != 2 || data.Mounts[0].Bind || !data.Mounts[1].Bind || data.Mounts[1].Point != "/export/my share" ||
		data.Mounts[1].Root != "/share" {
		t.Errorf("data mounts = %+v", data.Mounts)
	}
	if data.Usage == nil || data.Usage.TotalBytes != 900<<30 || *data.Label != "nasdata" || *data.PartLabel != "nas-data" {
		t.Errorf("data partition = %+v", data)
	}

	sdb := r.Disks[1]
	if sdb.ID != "usb-SanDisk_Cruzer_Fit_TEST0002-0:0" || sdb.Type != "flash" || sdb.Bus != "usb" || *sdb.Vendor != "SanDisk'" {
		t.Errorf("sdb = %+v", sdb)
	}
	if sdb.System || sdb.SharedWithData || len(sdb.Roles) != 1 || sdb.Roles[0] != RoleBoot {
		t.Errorf("sdb roles = %v", sdb.Roles)
	}
	if !sdb.Partitions[0].Mounts[0].ReadOnly {
		t.Error("/boot with a read-only superblock must be read-only")
	}
	if sdb.Partitions[0].Usage != nil {
		t.Error("failed statfs must leave usage empty")
	}
	if d, ok := r.FindByID("wwn-0x5000c500000000aa"); !ok || d.Name != "sda" {
		t.Error("FindByID wwn failed")
	}
	if !sda.SMART || sdb.SMART {
		t.Errorf("SMART capable: sda=%v sdb=%v", sda.SMART, sdb.SMART)
	}
}

func TestResolveDisk(t *testing.T) {
	r := fixture(t, nsa310)
	d, ok, err := ResolveDisk(r, "wwn-0x5000c500000000aa")
	if err != nil || !ok || d.Name != "sda" || !d.SMARTCapable() {
		t.Fatalf("sda: %+v %v %v", d, ok, err)
	}
	d, ok, _ = ResolveDisk(r, "usb-SanDisk_Cruzer_Fit_TEST0002-0:0")
	if !ok || d.SMARTCapable() {
		t.Fatalf("usb stick must not be SMART capable: %+v", d)
	}
	if _, ok, _ := ResolveDisk(r, "ata-ST1000LM014-SSHD-8GB_TEST0001"); ok {
		t.Fatal("only the stable ID shown in the UI may resolve")
	}
}

// The ID must follow the disk, not the kernel name, when sda and sdb swap.
func TestIDSurvivesRenumbering(t *testing.T) {
	swapped := map[string]string{}
	for k, v := range nsa310 {
		swapped[k] = v
	}
	swapped["run/udev/data/b8:0"], swapped["run/udev/data/b8:16"] = nsa310["run/udev/data/b8:16"], nsa310["run/udev/data/b8:0"]
	r, err := Collector{HAL: fixture(t, swapped)}.Report()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Disks {
		if d.Name == "sdb" && d.ID != "wwn-0x5000c500000000aa" {
			t.Errorf("sdb id = %q", d.ID)
		}
	}
}

func TestNoUdevFallsBackToKernelName(t *testing.T) {
	r, err := Collector{HAL: fixture(t, map[string]string{
		"sys/block/sdd/size":             "1000\n",
		"sys/block/sdd/dev":              "8:48\n",
		"sys/block/sdd/queue/rotational": "0\n",
		"proc/self/mountinfo":            "",
		"proc/swaps":                     "",
	})}.Report()
	if err != nil {
		t.Fatal(err)
	}
	d := r.Disks[0]
	if d.ID != "dev-sdd" || d.IDStable || d.Bus != "unknown" || d.Type != "ssd" || d.System || len(d.Partitions) != 0 {
		t.Errorf("fallback disk = %+v", d)
	}
}

func TestWholeDiskFilesystem(t *testing.T) {
	r, err := Collector{HAL: fixture(t, map[string]string{
		"sys/block/sde/size":  "1000\n",
		"sys/block/sde/dev":   "8:64\n",
		"run/udev/data/b8:64": "E:ID_FS_TYPE=ext4\nE:ID_BUS=usb\n",
		"proc/self/mountinfo": "50 14 8:64 / /srv/usb rw - ext4 /dev/sde rw\n",
		"proc/swaps":          "",
	}), StatFS: func(string) (uint64, uint64, uint64, error) { return 100, 40, 30, nil }}.Report()
	if err != nil {
		t.Fatal(err)
	}
	d := r.Disks[0]
	if len(d.Partitions) != 1 || d.Partitions[0].Name != "sde" || d.Partitions[0].Usage.UsedBytes != 60 || d.Roles[0] != RoleData || d.System {
		t.Errorf("whole-disk fs = %+v", d)
	}
}

func TestMissingSysBlockFails(t *testing.T) {
	if _, err := (Collector{HAL: hal.Reader{Root: t.TempDir()}}).Report(); err == nil {
		t.Fatal("expected error without /sys/block")
	}
}
