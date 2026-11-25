// Package diskpower holds the disk power profiles, the hdparm encodings and
// the per-disk configuration file (ADR 0007, PWR-01..03). The web process only
// reads the state (Reader); the root helper applies and stores the settings.
package diskpower

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"nas-revival/backend/internal/atomicfile"
)

// DefaultConfigPath is written by the helper and read by the web process.
const DefaultConfigPath = "/etc/nas-revival/disks.json"

// Profiles. Custom takes the user's standby timeout and APM level.
const (
	Performance = "performance"
	Balanced    = "balanced"
	Eco         = "eco"
	Custom      = "custom"
)

// ProfileIDs in the order the UI offers them.
var ProfileIDs = []string{Performance, Balanced, Eco, Custom}

// APMNoParking is the highest level that keeps power management on; levels
// 128–254 never spin the disk down by themselves, and 254 also stops the
// aggressive head parking of laptop drives (SMART 193).
const APMNoParking = 254

// StandbyChoices are the offered timeouts in minutes (0 = never). Each has an
// exact hdparm -S encoding.
var StandbyChoices = []int{0, 10, 20, 30, 60, 120, 180, 240, 300}

// Settings is what is stored and applied for one disk.
type Settings struct {
	Profile        string `json:"profile"`
	StandbyMinutes int    `json:"standbyMinutes"`
	// APM is the level for hdparm -B (1–254, 255 = off); nil leaves the
	// drive's own setting, e.g. when it has no APM.
	APM *int `json:"apm"`
}

// Error is a validation error with the field it concerns.
type Error struct {
	Field   string
	Message string
}

func (e *Error) Error() string { return e.Field + ": " + e.Message }

func intp(v int) *int { return &v }

// ForProfile returns the settings of a fixed profile. On the system disk the
// balanced profile never spins down: the system wakes it again at once.
func ForProfile(profile string, systemDisk bool) (Settings, bool) {
	switch profile {
	case Performance:
		return Settings{Profile: Performance, StandbyMinutes: 0, APM: intp(APMNoParking)}, true
	case Balanced:
		s := Settings{Profile: Balanced, StandbyMinutes: 60, APM: intp(APMNoParking)}
		if systemDisk {
			s.StandbyMinutes = 0
		}
		return s, true
	case Eco:
		return Settings{Profile: Eco, StandbyMinutes: 20, APM: intp(128)}, true
	}
	return Settings{}, false
}

// Default is applied to a disk that has no entry in the configuration.
func Default(systemDisk bool) Settings {
	s, _ := ForProfile(Balanced, systemDisk)
	return s
}

// Resolve turns a request into the settings to store: a fixed profile uses
// its own values, custom takes standbyMinutes and apm.
func Resolve(profile string, standbyMinutes int, apm *int, systemDisk bool) (Settings, error) {
	if profile == Custom {
		s := Settings{Profile: Custom, StandbyMinutes: standbyMinutes, APM: apm}
		return s, Validate(s)
	}
	s, ok := ForProfile(profile, systemDisk)
	if !ok {
		return Settings{}, &Error{"profile", "Neznámý profil napájení."}
	}
	return s, nil
}

// Validate checks stored or requested settings.
func Validate(s Settings) error {
	switch s.Profile {
	case Performance, Balanced, Eco, Custom:
	default:
		return &Error{"profile", "Neznámý profil napájení."}
	}
	if _, err := StandbyCode(s.StandbyMinutes); err != nil {
		return err
	}
	if s.APM != nil && (*s.APM < 1 || *s.APM > 255) {
		return &Error{"apm", "Úroveň APM musí být 1–254, nebo 255 (vypnuto)."}
	}
	return nil
}

// StandbyCode encodes minutes for hdparm -S: 1–240 count 5 s units (up to
// 20 min), 241–251 count 30 min units (30–330 min), 0 disables the timer.
func StandbyCode(minutes int) (int, error) {
	for _, m := range StandbyChoices {
		if m != minutes {
			continue
		}
		if m <= 20 {
			return m * 12, nil
		}
		return 240 + m/30, nil
	}
	return 0, &Error{"standbyMinutes", "Nepodporovaná doba do uspání."}
}

// Config is the content of disks.json, keyed by stable disk ID.
type Config struct {
	Version int                 `json:"version"`
	Disks   map[string]Settings `json:"disks"`
}

// Load reads the configuration; a missing file is an empty configuration.
// Invalid entries are dropped so one bad disk does not block the others.
func Load(path string) (Config, error) {
	c := Config{Version: 1, Disks: map[string]Settings{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var raw Config
	if err := json.Unmarshal(b, &raw); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	for id, s := range raw.Disks {
		if Validate(s) == nil {
			c.Disks[id] = s
		}
	}
	return c, nil
}

// Save writes the configuration atomically (0644: the web process reads it).
func Save(path string, c Config) error {
	c.Version = 1
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o644)
}

// Drive power states reported by hdparm -C.
const (
	StateActive  = "active"
	StateStandby = "standby"
	StateSleep   = "sleeping"
	StateUnknown = "unknown"
)

var stateRe = regexp.MustCompile(`drive state is:\s+(\S+)`)

// ParseState reads the output of hdparm -C.
func ParseState(out string) string {
	m := stateRe.FindStringSubmatch(out)
	if m == nil {
		return StateUnknown
	}
	switch {
	case strings.HasPrefix(m[1], "active"), m[1] == "idle":
		return StateActive
	case m[1] == "standby":
		return StateStandby
	case m[1] == "sleeping":
		return StateSleep
	}
	return StateUnknown
}

var apmRe = regexp.MustCompile(`APM_level\s*=\s*(\S.*)`)

// ParseAPM reads the output of hdparm -B: the level, or supported=false.
// ok is false when the output is not understood.
func ParseAPM(out string) (level *int, supported, ok bool) {
	m := apmRe.FindStringSubmatch(out)
	if m == nil {
		return nil, false, false
	}
	v := strings.TrimSpace(m[1])
	if v == "not supported" {
		return nil, false, true
	}
	if v == "off" {
		return intp(255), true, true
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 1 || n > 255 {
		return nil, false, false
	}
	return &n, true, true
}
