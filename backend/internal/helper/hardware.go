package helper

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/hardware"
)

// CodeFanApplyFailed: nsa310-fan refused the profile; the previous one was
// restored (or the fan runs at full speed, which is safe).
const CodeFanApplyFailed = "fan_apply_failed"

// HardwareManager owns hardware.json and the files the board scripts read
// (W3.4): thresholds, fan profile, LED night mode and the fault blink.
type HardwareManager struct {
	Path        string // hardware.json
	FanDefaults string // /etc/default/nsa310-fan
	LEDDefaults string // /etc/default/nsa310-leds
	LEDTimer    string // /etc/systemd/system/nsa310-leds.timer
	Run         Runner
	mu          sync.Mutex
}

func NewHardwareManager(run Runner) *HardwareManager {
	return &HardwareManager{
		Path: hardware.DefaultConfigPath, FanDefaults: hardware.FanDefaultsPath,
		LEDDefaults: hardware.LEDDefaultsPath, LEDTimer: hardware.LEDTimerPath, Run: run,
	}
}

func hardwareFieldErr(err error) error {
	var fe *hardware.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

// load never fails: a broken file must not block fixing it from the UI, so
// it starts from the defaults.
func (m *HardwareManager) load() hardware.Settings {
	s, err := hardware.Load(m.Path)
	if err != nil {
		slog.Warn("helper: hardware.json unreadable, starting from defaults", "err", err)
	}
	return s
}

func (m *HardwareManager) save(s hardware.Settings) error {
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(m.Path, hardware.Marshal(s), 0o644)
}

// SetThresholds stores the temperature thresholds; the web process reads them.
func (m *HardwareManager) SetThresholds(_ context.Context, t hardware.Thresholds, user string) (hardware.Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.load()
	s.Thresholds = t
	if err := hardware.Validate(s); err != nil {
		return s, hardwareFieldErr(err)
	}
	if err := m.save(s); err != nil {
		slog.Error("helper: write hardware.json", "err", err)
		return s, internalErr("Nastavení hardwaru nelze uložit.")
	}
	slog.Info("hardware thresholds changed", "board", t.BoardWarnC, "boardCrit", t.BoardCritC,
		"disk", t.DiskWarnC, "diskCrit", t.DiskCritC, "by", user)
	return s, nil
}

func readOptional(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	return b, err == nil
}

// SetFanProfile renders the profile for nsa310-fan and re-runs it. The script
// writes the LM85 and reads every value back; on any failure it leaves the
// fan at full speed, and the previous profile is put back.
func (m *HardwareManager) SetFanProfile(ctx context.Context, id, user string) (hardware.Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.load()
	p, ok := hardware.ProfileByID(id)
	if !ok {
		return s, invalid("profile", "Neznámý profil ventilátoru.")
	}
	prev, hadPrev := readOptional(m.FanDefaults)
	if err := atomicfile.Write(m.FanDefaults, []byte(hardware.RenderFanDefaults(p)), 0o644); err != nil {
		slog.Error("helper: write fan defaults", "err", err)
		return s, internalErr("Profil ventilátoru nelze uložit.")
	}
	if _, err := m.Run(ctx, "systemctl", "restart", "nsa310-fan.service"); err != nil {
		slog.Error("helper: nsa310-fan rejected profile", "profile", id, "err", err)
		if hadPrev {
			_ = atomicfile.Write(m.FanDefaults, prev, 0o644)
		} else {
			_ = os.Remove(m.FanDefaults)
		}
		if _, err := m.Run(ctx, "systemctl", "restart", "nsa310-fan.service"); err != nil {
			slog.Error("helper: nsa310-fan previous profile failed too, fan at full speed", "err", err)
		}
		return s, &Error{Code: CodeFanApplyFailed, Message: "Řadič ventilátoru profil nepřijal. Zůstává předchozí nastavení, případně ventilátor běží naplno."}
	}
	s.FanProfile = id
	if err := m.save(s); err != nil {
		slog.Error("helper: write hardware.json", "err", err)
		return s, internalErr("Profil běží, ale nastavení se nepodařilo uložit.")
	}
	slog.Info("fan profile changed", "profile", id, "by", user)
	return s, nil
}

// SetLEDs stores night mode and the fault blink, renders the defaults and the
// switch timer and re-runs nsa310-leds so the change shows at once.
func (m *HardwareManager) SetLEDs(ctx context.Context, l hardware.LEDs, user string) (hardware.Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.load()
	s.LEDs = l
	if err := hardware.Validate(s); err != nil {
		return s, hardwareFieldErr(err)
	}
	fail := func(what string, err error) (hardware.Settings, error) {
		slog.Error("helper: led settings", "step", what, "err", err)
		return s, internalErr("Nastavení LED nelze použít.")
	}
	if err := atomicfile.Write(m.LEDDefaults, []byte(hardware.RenderLEDDefaults(l.Night)), 0o644); err != nil {
		return fail("defaults", err)
	}
	if l.Night.Enabled {
		if err := atomicfile.Write(m.LEDTimer, []byte(hardware.RenderLEDTimer(l.Night)), 0o644); err != nil {
			return fail("timer", err)
		}
		if _, err := m.Run(ctx, "systemctl", "daemon-reload"); err != nil {
			return fail("daemon-reload", err)
		}
		// restart, not enable --now: a running timer must pick up new times.
		if _, err := m.Run(ctx, "systemctl", "enable", "nsa310-leds.timer"); err != nil {
			return fail("enable", err)
		}
		if _, err := m.Run(ctx, "systemctl", "restart", "nsa310-leds.timer"); err != nil {
			return fail("restart timer", err)
		}
	} else if _, err := os.Stat(m.LEDTimer); err == nil {
		if _, err := m.Run(ctx, "systemctl", "disable", "--now", "nsa310-leds.timer"); err != nil {
			return fail("disable", err)
		}
		if err := os.Remove(m.LEDTimer); err != nil {
			return fail("remove timer", err)
		}
		if _, err := m.Run(ctx, "systemctl", "daemon-reload"); err != nil {
			return fail("daemon-reload", err)
		}
	}
	if _, err := m.Run(ctx, "systemctl", "start", "nsa310-leds.service"); err != nil {
		return fail("apply", err)
	}
	if err := m.save(s); err != nil {
		return fail("save", err)
	}
	slog.Info("led settings changed", "night", l.Night.Enabled, "start", l.Night.Start, "end", l.Night.End,
		"faultBlink", l.FaultBlink, "by", user)
	return s, nil
}

// SetFault blinks the red system LED (kernel timer trigger) or gives it back
// to the panic trigger, through nsa310-led-fault@on|off: the helper's
// sandbox keeps /sys read-only. Called by the web process when the health
// turns critical or recovers; night mode never touches red LEDs.
func (m *HardwareManager) SetFault(ctx context.Context, on bool) error {
	unit := "nsa310-led-fault@off.service"
	if on {
		unit = "nsa310-led-fault@on.service"
	}
	if _, err := m.Run(ctx, "systemctl", "start", unit); err != nil {
		slog.Error("helper: fault led", "on", on, "err", err)
		return internalErr("LED nelze nastavit.")
	}
	slog.Info("fault led", "on", on)
	return nil
}
