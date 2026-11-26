package system

import (
	"os"
	"path/filepath"
	"strings"
)

// Evidence levels of a capability (docs/HARDWARE_COMPATIBILITY.md). The UI
// offers write operations only for verified capabilities.
const (
	Verified    = "verified"
	Unverified  = "unverified"
	Unsupported = "unsupported"
)

// Capability IDs known to the UI.
const (
	CapTemperatures = "temperatures"
	CapFanSpeed     = "fanSpeed"
	CapFanControl   = "fanControl"
	CapLEDs         = "leds"
	CapCopyButton   = "copyButton"
	CapSMART        = "smart"
	CapDiskStandby  = "diskStandby"
	CapKernelSlots  = "kernelSlots"
	CapRTCWake      = "rtcWake"
	CapWakeOnLAN    = "wakeOnLan"
	CapAutoPowerOn  = "autoPowerOn"
	CapRAID         = "raid"
)

// Profile describes board facts that sysfs cannot tell us, such as which
// fan headers of a multi-channel controller are actually wired. Profiles are
// trusted data compiled into the binary, never uploaded.
type Profile struct {
	ID   string
	Name string
	// Fans maps hwmon fan IDs to display labels. When non-empty, only these
	// fans are reported, so unwired channels do not appear as stopped fans.
	Fans map[string]string
	// Temps maps hwmon temperature IDs to display labels; others keep theirs.
	Temps map[string]string
	// SATABays and USBPorts are physical counts; 0 means unknown.
	SATABays int
	USBPorts int
	// Capabilities maps capability IDs to evidence levels; missing = unsupported.
	Capabilities map[string]string
}

// Owner-confirmed and measured 2026-11-25 (docs/HARDWARE_COMPATIBILITY.md).
var nsa310 = Profile{
	ID: "zyxel-nsa310", Name: "ZyXEL NSA310",
	Fans: map[string]string{"lm85/fan1": "Ventilátor"},
	// temp2 is the LM85's own sensor, temp1/temp3 remote diodes on the board
	// (which part each sits on is not verified).
	Temps:    map[string]string{"lm85/temp1": "Dioda 1", "lm85/temp2": "Čip LM85", "lm85/temp3": "Dioda 2"},
	SATABays: 1, USBPorts: 2,
	Capabilities: map[string]string{
		CapTemperatures: Verified,
		CapFanSpeed:     Verified,
		CapFanControl:   Verified,
		CapLEDs:         Verified,
		CapCopyButton:   Verified,
		CapSMART:        Verified,
		CapDiskStandby:  Unverified,
		CapKernelSlots:  Verified,
		CapRTCWake:      Unverified,
		CapWakeOnLAN:    Unverified,
		CapAutoPowerOn:  Unsupported,
		CapRAID:         Unsupported,
	},
}

// Generic profiles read standard interfaces only; no actuator is trusted.
var genericCaps = map[string]string{
	CapTemperatures: Unverified,
	CapFanSpeed:     Unverified,
	CapSMART:        Unverified,
	CapDiskStandby:  Unverified,
}

var (
	genericARM   = Profile{ID: "generic-arm-nas", Name: "Generic ARM NAS", Capabilities: genericCaps}
	genericLinux = Profile{ID: "generic-linux-nas", Name: "Generic Linux NAS", Capabilities: genericCaps}
)

// byCompatible selects a profile by a device-tree compatible string.
var byCompatible = map[string]Profile{
	"zyxel,nsa310a": nsa310,
	"zyxel,nsa310":  nsa310,
}

// byModel is the fallback when the compatible list is unreadable.
var byModel = map[string]Profile{"ZyXEL NSA310": nsa310}

// DetectProfile picks the profile of the board under root ("/" in production).
func DetectProfile(root string) Profile {
	dt := filepath.Join(root, "proc/device-tree")
	if data, err := os.ReadFile(filepath.Join(dt, "compatible")); err == nil {
		for _, c := range strings.Split(string(data), "\x00") {
			if p, ok := byCompatible[c]; ok {
				return p
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dt, "model")); err == nil {
		if p, ok := byModel[strings.TrimRight(strings.TrimSpace(string(data)), "\x00")]; ok {
			return p
		}
	}
	if _, err := os.Stat(dt); err == nil {
		return genericARM
	}
	return genericLinux
}

func profileFor(board *string) Profile {
	if board == nil {
		return Profile{}
	}
	return byModel[*board]
}
