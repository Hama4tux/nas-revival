package helper

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"

	"nas-revival/backend/internal/diskpower"
)

// PowerDisk is what the helper knows about a disk for power management.
type PowerDisk struct {
	ID      string
	Name    string // kernel name, e.g. sda
	Capable bool   // internal SATA disk that hdparm addresses directly
	System  bool   // holds the root filesystem
}

// DiskPowerManager stores and applies standby timeout and APM (ops
// disk.setPower, disk.standby; ADR 0007).
type DiskPowerManager struct {
	// Resolve maps a stable disk ID to the kernel device; the request never
	// carries a path. Disks lists all disks for the boot task.
	Resolve    func(id string) (PowerDisk, bool, error)
	Disks      func() ([]PowerDisk, error)
	Run        Runner
	ConfigPath string
	mu         sync.Mutex
}

type DiskPowerResult struct {
	DiskID   string             `json:"diskId"`
	Settings diskpower.Settings `json:"settings"`
	// APMApplied is false when the drive has no APM and only the standby
	// timeout was set.
	APMApplied bool `json:"apmApplied"`
}

type DiskStandbyResult struct {
	DiskID string `json:"diskId"`
	// WasStandby: the disk already slept, nothing was sent.
	WasStandby bool `json:"wasStandby"`
}

func (m *DiskPowerManager) disk(id string) (PowerDisk, error) {
	if !diskIDRe.MatchString(id) {
		return PowerDisk{}, invalid("diskId", "Neplatné ID disku.")
	}
	d, ok, err := m.Resolve(id)
	if err != nil {
		return PowerDisk{}, internalErr("Seznam disků nelze načíst.")
	}
	if !ok {
		return PowerDisk{}, invalid("diskId", "Disk nebyl nalezen.")
	}
	if !d.Capable {
		return PowerDisk{}, invalid("diskId", "Disk nepodporuje správu napájení.")
	}
	return d, nil
}

func (m *DiskPowerManager) hdparm(ctx context.Context, args ...string) (string, error) {
	out, err := m.Run(ctx, "hdparm", args...)
	if err != nil {
		return string(out), internalErr("Nástroj hdparm selhal.")
	}
	return string(out), nil
}

// apply sets APM (when the drive has it) and the standby timer.
func (m *DiskPowerManager) apply(ctx context.Context, name string, s diskpower.Settings) (apmApplied bool, err error) {
	code, err := diskpower.StandbyCode(s.StandbyMinutes)
	if err != nil {
		return false, err
	}
	dev := "/dev/" + name
	if s.APM != nil {
		out, err := m.hdparm(ctx, "-B", dev)
		if err != nil {
			return false, err
		}
		if _, supported, ok := diskpower.ParseAPM(out); ok && supported {
			if _, err := m.hdparm(ctx, "-B", strconv.Itoa(*s.APM), dev); err != nil {
				return false, err
			}
			apmApplied = true
		}
	}
	if _, err := m.hdparm(ctx, "-S", strconv.Itoa(code), dev); err != nil {
		return apmApplied, err
	}
	return apmApplied, nil
}

// SetPower validates, applies and stores the settings of one disk. They are
// stored only after the drive accepted them.
func (m *DiskPowerManager) SetPower(ctx context.Context, id string, s diskpower.Settings) (DiskPowerResult, error) {
	if err := diskpower.Validate(s); err != nil {
		var de *diskpower.Error
		if errors.As(err, &de) {
			return DiskPowerResult{}, invalid(de.Field, de.Message)
		}
		return DiskPowerResult{}, invalid("", "Neplatné nastavení napájení.")
	}
	d, err := m.disk(id)
	if err != nil {
		return DiskPowerResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := diskpower.Load(m.ConfigPath)
	if err != nil {
		slog.Warn("helper: disks.json unreadable, starting empty", "err", err)
	}
	applied, err := m.apply(ctx, d.Name, s)
	if err != nil {
		return DiskPowerResult{}, err
	}
	cfg.Disks[id] = s
	if err := diskpower.Save(m.ConfigPath, cfg); err != nil {
		slog.Error("helper: disks.json not saved", "err", err)
		return DiskPowerResult{}, internalErr("Nastavení platí, ale nepodařilo se ho uložit; po restartu se ztratí.")
	}
	slog.Info("helper: disk.setPower", "disk", id, "device", d.Name, "profile", s.Profile,
		"standbyMinutes", s.StandbyMinutes, "apmApplied", applied)
	return DiskPowerResult{DiskID: id, Settings: s, APMApplied: applied}, nil
}

// Standby spins the disk down now. A running SMART self-test would be
// aborted, so it is refused.
func (m *DiskPowerManager) Standby(ctx context.Context, id string) (DiskStandbyResult, error) {
	d, err := m.disk(id)
	if err != nil {
		return DiskStandbyResult{}, err
	}
	dev := "/dev/" + d.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	out, err := m.hdparm(ctx, "-C", dev)
	if err != nil {
		return DiskStandbyResult{}, err
	}
	if st := diskpower.ParseState(out); st == diskpower.StateStandby || st == diskpower.StateSleep {
		return DiskStandbyResult{DiskID: id, WasStandby: true}, nil
	}
	caps, err := (&SmartManager{Run: m.Run}).smartctl(ctx, "-j", "-c", dev)
	if err != nil {
		return DiskStandbyResult{}, err
	}
	if a := caps.ATAData; a != nil && a.SelfTest != nil && a.SelfTest.Status != nil && a.SelfTest.Status.Value>>4 == 0xF {
		return DiskStandbyResult{}, &Error{Code: CodeTestRunning, Message: "Na disku běží samotest SMART; uspáním by se přerušil."}
	}
	if _, err := m.hdparm(ctx, "-y", dev); err != nil {
		return DiskStandbyResult{}, err
	}
	slog.Info("helper: disk.standby", "disk", id, "device", d.Name)
	return DiskStandbyResult{DiskID: id}, nil
}

// ApplyAll is the boot task disk-power: every internal disk gets its stored
// settings, or the default profile when it has none. Failures are logged and
// do not stop the other disks.
func (m *DiskPowerManager) ApplyAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	disks, err := m.Disks()
	if err != nil {
		slog.Error("helper: disk-power: disks not listed", "err", err)
		return
	}
	cfg, err := diskpower.Load(m.ConfigPath)
	if err != nil {
		slog.Warn("helper: disk-power: disks.json unreadable, using defaults", "err", err)
	}
	for _, d := range disks {
		if !d.Capable {
			continue
		}
		s, ok := cfg.Disks[d.ID]
		if !ok {
			s = diskpower.Default(d.System)
		}
		applied, err := m.apply(ctx, d.Name, s)
		if err != nil {
			slog.Error("helper: disk-power failed", "disk", d.ID, "device", d.Name, "err", err)
			continue
		}
		slog.Info("helper: disk-power applied", "disk", d.ID, "device", d.Name, "profile", s.Profile,
			"stored", ok, "standbyMinutes", s.StandbyMinutes, "apmApplied", applied)
	}
}
