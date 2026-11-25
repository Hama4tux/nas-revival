package system

import (
	"testing"
	"time"
)

func TestDetectProfileByCompatible(t *testing.T) {
	root := writeTree(t, map[string]string{
		"proc/device-tree/compatible": "zyxel,nsa310a\x00zyxel,nsa310\x00marvell,kirkwood\x00",
		"proc/device-tree/model":      "Something else\x00",
	})
	if p := DetectProfile(root); p.ID != "zyxel-nsa310" {
		t.Fatalf("profile = %q", p.ID)
	}
}

func TestDetectProfileFallbacks(t *testing.T) {
	model := writeTree(t, map[string]string{"proc/device-tree/model": "ZyXEL NSA310\x00"})
	if p := DetectProfile(model); p.ID != "zyxel-nsa310" {
		t.Fatalf("by model: %q", p.ID)
	}
	arm := writeTree(t, map[string]string{"proc/device-tree/compatible": "acme,box\x00"})
	if p := DetectProfile(arm); p.ID != "generic-arm-nas" {
		t.Fatalf("unknown DT: %q", p.ID)
	}
	if p := DetectProfile(t.TempDir()); p.ID != "generic-linux-nas" {
		t.Fatalf("no DT: %q", p.ID)
	}
}

func TestGenericProfileHasNoActuators(t *testing.T) {
	for _, c := range CapabilitiesOf(genericARM).Capabilities {
		if c.Status == Verified {
			t.Errorf("generic capability %s must not be verified", c.ID)
		}
		if (c.ID == CapFanControl || c.ID == CapLEDs || c.ID == CapKernelSlots) && c.Status != Unsupported {
			t.Errorf("generic %s = %s, want unsupported", c.ID, c.Status)
		}
	}
}

func TestCapabilitiesOfNSA310(t *testing.T) {
	c := CapabilitiesOf(nsa310)
	if c.SATABays == nil || *c.SATABays != 1 || c.USBPorts == nil || *c.USBPorts != 2 {
		t.Fatalf("ports: %+v %+v", c.SATABays, c.USBPorts)
	}
	got := map[string]string{}
	for _, s := range c.Capabilities {
		got[s.ID] = s.Status
	}
	if got[CapFanControl] != Verified || got[CapAutoPowerOn] != Unsupported || got[CapWakeOnLAN] != Unverified {
		t.Fatalf("caps: %v", got)
	}
	if len(c.Capabilities) != 12 {
		t.Fatalf("expected all 12 capabilities, got %d", len(c.Capabilities))
	}
}

func TestSystemInfo(t *testing.T) {
	files := map[string]string{}
	for k, v := range minimal {
		files[k] = v
	}
	files["proc/stat"] = "cpu 100 0 0 900 0 0 0 0 0 0\nbtime 1764086400\n"
	files["etc/debian_version"] = "13.8\n"
	files["proc/sys/kernel/arch"] = "armv5tel\n"
	files["var/log/dpkg.log"] = "x"
	root := writeTree(t, files)
	in, err := SystemInfo(root, "test-1")
	if err != nil {
		t.Fatal(err)
	}
	if in.Version != "test-1" || in.Hostname != "nas" || in.Kernel != "6.12.119" || in.Architecture != "armv5tel" {
		t.Fatalf("ids: %+v", in)
	}
	if in.DebianVersion == nil || *in.DebianVersion != "13.8" {
		t.Fatalf("debian: %v", in.DebianVersion)
	}
	if in.BootTime == nil || !in.BootTime.Equal(time.Unix(1764086400, 0)) {
		t.Fatalf("boot: %v", in.BootTime)
	}
	if in.MemTotalBytes != 1000*1024 || in.Cores != 1 || in.LastPackageChange == nil {
		t.Fatalf("mem/cores/dpkg: %+v", in)
	}
}

func TestSystemInfoMissingOptional(t *testing.T) {
	in, err := SystemInfo(writeTree(t, minimal), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if in.DebianVersion != nil || in.BootTime != nil || in.LastPackageChange != nil {
		t.Fatalf("missing data must be null: %+v", in)
	}
}
