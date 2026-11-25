package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"nas-revival/backend/internal/hal"
)

// Info is GET /system/info: identification that changes rarely.
type Info struct {
	Version       string     `json:"version"`
	Hostname      string     `json:"hostname"`
	BoardModel    *string    `json:"boardModel"`
	OS            string     `json:"os"`
	DebianVersion *string    `json:"debianVersion"`
	Kernel        string     `json:"kernel"`
	Architecture  string     `json:"architecture"`
	CPUModel      string     `json:"cpuModel"`
	Cores         int        `json:"cores"`
	MemTotalBytes uint64     `json:"memTotalBytes"`
	BootTime      *time.Time `json:"bootTime"`
	UptimeSeconds int64      `json:"uptimeSeconds"`
	// LastPackageChange is the last dpkg activity (install/upgrade/remove).
	LastPackageChange *time.Time `json:"lastPackageChange"`
}

// CapabilityState is one entry of GET /system/capabilities.
type CapabilityState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Capabilities is GET /system/capabilities.
type Capabilities struct {
	ProfileID    string            `json:"profileId"`
	ProfileName  string            `json:"profileName"`
	SATABays     *int              `json:"sataBays"`
	USBPorts     *int              `json:"usbPorts"`
	Capabilities []CapabilityState `json:"capabilities"`
}

// SystemInfo reads Info from the file-system root.
func SystemInfo(root, version string) (Info, error) {
	r := hal.Reader{Root: root}
	h, err := r.Host()
	if err != nil {
		return Info{}, err
	}
	in := Info{Version: version, Hostname: h.Hostname, BoardModel: h.BoardModel, OS: h.OS,
		Kernel: h.Kernel, UptimeSeconds: h.UptimeSeconds, Architecture: archName(root)}
	if c, err := r.CPU(); err == nil {
		in.CPUModel, in.Cores = c.Model, c.Cores
	}
	if m, err := r.Memory(); err == nil {
		in.MemTotalBytes = m.TotalBytes
	}
	if v, err := os.ReadFile(filepath.Join(root, "etc/debian_version")); err == nil {
		s := strings.TrimSpace(string(v))
		in.DebianVersion = &s
	}
	in.BootTime = bootTime(root)
	for _, p := range []string{"var/log/dpkg.log", "var/lib/dpkg/status"} {
		if st, err := os.Stat(filepath.Join(root, p)); err == nil {
			t := st.ModTime().UTC()
			in.LastPackageChange = &t
			break
		}
	}
	return in, nil
}

func archName(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "proc/sys/kernel/arch")); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return runtime.GOARCH
}

func bootTime(root string) *time.Time {
	data, err := os.ReadFile(filepath.Join(root, "proc/stat"))
	if err != nil {
		return nil
	}
	for _, l := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(l, "btime "); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				t := time.Unix(n, 0).UTC()
				return &t
			}
		}
	}
	return nil
}

// CapabilitiesOf lists every known capability of p in a stable order.
func CapabilitiesOf(p Profile) Capabilities {
	c := Capabilities{ProfileID: p.ID, ProfileName: p.Name, Capabilities: []CapabilityState{}}
	if p.SATABays > 0 {
		c.SATABays = &p.SATABays
	}
	if p.USBPorts > 0 {
		c.USBPorts = &p.USBPorts
	}
	ids := []string{CapTemperatures, CapFanSpeed, CapFanControl, CapLEDs, CapCopyButton, CapSMART,
		CapDiskStandby, CapKernelSlots, CapRTCWake, CapWakeOnLAN, CapAutoPowerOn, CapRAID}
	for _, id := range ids {
		st, ok := p.Capabilities[id]
		if !ok {
			st = Unsupported
		}
		c.Capabilities = append(c.Capabilities, CapabilityState{ID: id, Status: st})
	}
	return c
}
