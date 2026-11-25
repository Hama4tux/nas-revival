package hal

import (
	"os"
	"path/filepath"
	"testing"
)

// fixture writes files (path → content) below a temporary root.
func fixture(t *testing.T, files map[string]string) Reader {
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
	return Reader{Root: root}
}

// nsa310Base mirrors what Debian 13 with kernel 6.12 exposes on the NSA310.
var nsa310Base = map[string]string{
	"proc/sys/kernel/hostname":  "nas\n",
	"proc/sys/kernel/osrelease": "6.12.119\n",
	"proc/device-tree/model":    "ZyXEL NSA310\x00",
	"proc/uptime":               "12345.67 12000.00\n",
	"etc/os-release":            "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n",
	"proc/cpuinfo": "processor\t: 0\nmodel name\t: Feroceon 88FR131 rev 1 (v5l)\nBogoMIPS\t: 1191.93\n\n" +
		"Hardware\t: Marvell Kirkwood (Flattened Device Tree)\n",
	"proc/loadavg": "0.12 0.10 0.05 1/80 1234\n",
	"proc/stat":    "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 100 0 50 800 50 0 0 0 0 0\n",
	"proc/meminfo": "MemTotal:         250000 kB\nMemFree:  10000 kB\nMemAvailable:     180000 kB\nSwapTotal:       1048572 kB\nSwapFree:        1048000 kB\n",
	"proc/self/mounts": "/dev/sda1 / ext4 rw,relatime 0 0\nproc /proc proc rw 0 0\ntmpfs /run tmpfs rw 0 0\n" +
		"/dev/sda3 /srv/data ext4 rw 0 0\n/dev/sda1 / ext4 rw 0 0\n/dev/sdb1 /media/my\\040stick vfat rw 0 0\n/dev/loop3 /snap/core/1 squashfs ro 0 0\n",

	"sys/class/hwmon/hwmon0/name":        "lm85\n",
	"sys/class/hwmon/hwmon0/temp1_input": "41000\n",
	"sys/class/hwmon/hwmon0/temp2_input": "38500\n",
	"sys/class/hwmon/hwmon0/fan1_input":  "1830\n",
	"sys/class/hwmon/hwmon0/pwm1":        "128\n",

	"sys/class/net/lo/operstate":             "unknown\n",
	"sys/class/net/eth0/operstate":           "up\n",
	"sys/class/net/eth0/speed":               "1000\n",
	"sys/class/net/eth0/statistics/rx_bytes": "123456\n",
	"sys/class/net/eth0/statistics/tx_bytes": "654321\n",
	"sys/class/net/eth1/operstate":           "down\n",
	"sys/class/net/eth1/speed":               "-1\n",
	"sys/class/net/eth1/statistics/rx_bytes": "0\n",
	"sys/class/net/eth1/statistics/tx_bytes": "0\n",

	"sys/block/sda/size":             "1953525168\n",
	"sys/block/sda/device/model":     "ST1000LM014-1EJ1\n",
	"sys/block/sda/queue/rotational": "1\n",
	"sys/block/sda/removable":        "0\n",
	"sys/block/sdc/size":             "0\n",
	"sys/block/mtdblock0/size":       "2048\n",
	"sys/block/loop0/size":           "100\n",
}

func TestHost(t *testing.T) {
	h, err := fixture(t, nsa310Base).Host()
	if err != nil {
		t.Fatal(err)
	}
	if h.Hostname != "nas" || h.Kernel != "6.12.119" || h.UptimeSeconds != 12345 {
		t.Fatalf("unexpected host %+v", h)
	}
	if h.OS != "Debian GNU/Linux 13 (trixie)" {
		t.Fatalf("os = %q", h.OS)
	}
	if h.BoardModel == nil || *h.BoardModel != "ZyXEL NSA310" {
		t.Fatalf("board = %v", h.BoardModel)
	}
}

func TestCPUAndTimes(t *testing.T) {
	r := fixture(t, nsa310Base)
	c, err := r.CPU()
	if err != nil {
		t.Fatal(err)
	}
	if c.Cores != 1 || c.Model != "Feroceon 88FR131 rev 1 (v5l)" || c.Load5 != 0.10 {
		t.Fatalf("cpu %+v", c)
	}
	ct, err := r.CPUTimes()
	if err != nil {
		t.Fatal(err)
	}
	if ct.Total != 1000 || ct.Idle != 850 {
		t.Fatalf("times %+v", ct)
	}
}

func TestOldKernelCPUInfoUsesProcessorField(t *testing.T) {
	files := map[string]string{
		"proc/cpuinfo": "Processor\t: Feroceon 88FR131 rev 1 (v5l)\nBogoMIPS\t: 1192\n",
		"proc/loadavg": "0 0 0 1/1 1\n",
	}
	c, err := fixture(t, files).CPU()
	if err != nil {
		t.Fatal(err)
	}
	if c.Cores != 1 || c.Model != "Feroceon 88FR131 rev 1 (v5l)" {
		t.Fatalf("cpu %+v", c)
	}
}

func TestMemory(t *testing.T) {
	m, err := fixture(t, nsa310Base).Memory()
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalBytes != 250000*1024 || m.AvailableBytes != 180000*1024 || m.SwapFreeBytes != 1048000*1024 {
		t.Fatalf("mem %+v", m)
	}
}

func TestMemoryIncomplete(t *testing.T) {
	_, err := fixture(t, map[string]string{"proc/meminfo": "MemTotal: 1 kB\n"}).Memory()
	if err == nil {
		t.Fatal("expected error for missing MemAvailable")
	}
}

func TestSensors(t *testing.T) {
	s := fixture(t, nsa310Base).Sensors()
	if s.Status != SensorOK {
		t.Fatalf("status %s", s.Status)
	}
	if len(s.Temperatures) != 2 || s.Temperatures[0].ID != "lm85/temp1" || s.Temperatures[0].MilliCelsius != 41000 {
		t.Fatalf("temps %+v", s.Temperatures)
	}
	if len(s.Fans) != 1 || s.Fans[0].RPM != 1830 || s.Fans[0].Label != "fan1" {
		t.Fatalf("fans %+v", s.Fans)
	}
}

func TestSensorsMissingIsUnsupported(t *testing.T) {
	s := fixture(t, map[string]string{"proc/uptime": "1 1"}).Sensors()
	if s.Status != SensorUnsupported || len(s.Temperatures) != 0 || s.Fans == nil {
		t.Fatalf("got %+v", s)
	}
}

func TestSensorsUnreadableInputIsFault(t *testing.T) {
	files := map[string]string{
		"sys/class/hwmon/hwmon3/name":        "lm85",
		"sys/class/hwmon/hwmon3/temp1_input": "garbage",
		"sys/class/hwmon/hwmon3/fan1_input":  "1500",
	}
	s := fixture(t, files).Sensors()
	if s.Status != SensorFault || len(s.Fans) != 1 {
		t.Fatalf("got %+v", s)
	}
}

func TestNetInterfaces(t *testing.T) {
	ifs, err := fixture(t, nsa310Base).NetInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifs) != 2 {
		t.Fatalf("want eth0+eth1 without lo, got %+v", ifs)
	}
	eth0, eth1 := ifs[0], ifs[1]
	if !eth0.Up || eth0.SpeedMbps == nil || *eth0.SpeedMbps != 1000 || eth0.RxBytes != 123456 {
		t.Fatalf("eth0 %+v", eth0)
	}
	if eth1.Up || eth1.SpeedMbps != nil {
		t.Fatalf("eth1 must be down with unknown speed: %+v", eth1)
	}
}

func TestDisks(t *testing.T) {
	ds, err := fixture(t, nsa310Base).Disks()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("want only sda (no empty, mtd or loop devices), got %+v", ds)
	}
	d := ds[0]
	if d.Name != "sda" || d.SizeBytes != 1953525168*512 || !d.Rotational || d.Removable || *d.Model != "ST1000LM014-1EJ1" {
		t.Fatalf("disk %+v", d)
	}
}

func TestMounts(t *testing.T) {
	ms, err := fixture(t, nsa310Base).Mounts()
	if err != nil {
		t.Fatal(err)
	}
	want := []Mount{
		{"/dev/sda1", "/", "ext4"},
		{"/dev/sda3", "/srv/data", "ext4"},
		{"/dev/sdb1", "/media/my stick", "vfat"},
	}
	if len(ms) != len(want) {
		t.Fatalf("mounts %+v", ms)
	}
	for i := range want {
		if ms[i] != want[i] {
			t.Fatalf("mount %d = %+v, want %+v", i, ms[i], want[i])
		}
	}
}

func TestMountsResolveDevRoot(t *testing.T) {
	files := map[string]string{}
	for k, v := range nsa310Base {
		files[k] = v
	}
	files["proc/self/mounts"] = "/dev/root / ext4 rw,relatime 0 0\n/dev/sda3 /srv/data ext4 rw 0 0\n"
	files["proc/self/mountinfo"] = "22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/root rw\n" +
		"30 22 8:3 / /srv/data rw,relatime shared:9 - ext4 /dev/sda3 rw\n"
	files["sys/dev/block/8:2/uevent"] = "MAJOR=8\nMINOR=2\nDEVNAME=sda2\nDEVTYPE=partition\n"
	ms, err := fixture(t, files).Mounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Device != "/dev/sda2" || ms[1].Device != "/dev/sda3" {
		t.Fatalf("mounts %+v", ms)
	}

	// Unresolvable alias stays as reported.
	delete(files, "sys/dev/block/8:2/uevent")
	ms, err = fixture(t, files).Mounts()
	if err != nil || ms[0].Device != "/dev/root" {
		t.Fatalf("mounts %+v err %v", ms, err)
	}
}
