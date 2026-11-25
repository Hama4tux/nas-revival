package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/diskpower"
)

type fakeDrive struct {
	state string // hdparm -C output
	apm   string // hdparm -B output
	caps  string // smartctl -c output
	fail  string // command prefix that fails
	calls []string
}

func (f *fakeDrive) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		return nil, errors.New("exit 1")
	}
	switch {
	case call == "hdparm -C /dev/sda":
		return []byte(f.state), nil
	case call == "hdparm -B /dev/sda":
		return []byte(f.apm), nil
	case name == "smartctl":
		return []byte(f.caps), nil
	}
	return nil, nil
}

func newPower(t *testing.T, f *fakeDrive) *DiskPowerManager {
	t.Helper()
	disks := []PowerDisk{{ID: "wwn-0x1", Name: "sda", Capable: true, System: true}, {ID: "usb-stick", Name: "sdb"}}
	return &DiskPowerManager{
		Resolve: func(id string) (PowerDisk, bool, error) {
			for _, d := range disks {
				if d.ID == id {
					return d, true, nil
				}
			}
			return PowerDisk{}, false, nil
		},
		Disks:      func() ([]PowerDisk, error) { return disks, nil },
		Run:        f.run,
		ConfigPath: filepath.Join(t.TempDir(), "disks.json"),
	}
}

const apm254 = "\n/dev/sda:\n APM_level\t= 254\n"

func TestSetPowerAppliesAndStores(t *testing.T) {
	f := &fakeDrive{apm: apm254}
	m := newPower(t, f)
	s, _ := diskpower.ForProfile(diskpower.Eco, true)
	r, err := m.SetPower(context.Background(), "wwn-0x1", s)
	if err != nil || !r.APMApplied {
		t.Fatalf("%+v %v", r, err)
	}
	want := []string{"hdparm -B /dev/sda", "hdparm -B 128 /dev/sda", "hdparm -S 240 /dev/sda"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", f.calls)
	}
	cfg, err := diskpower.Load(m.ConfigPath)
	if err != nil || cfg.Disks["wwn-0x1"].Profile != diskpower.Eco {
		t.Fatalf("stored %+v %v", cfg, err)
	}
}

func TestSetPowerWithoutAPMSetsOnlyStandby(t *testing.T) {
	f := &fakeDrive{apm: "\n/dev/sda:\n APM_level\t= not supported\n"}
	m := newPower(t, f)
	s, _ := diskpower.ForProfile(diskpower.Performance, true)
	r, err := m.SetPower(context.Background(), "wwn-0x1", s)
	if err != nil || r.APMApplied || f.calls[len(f.calls)-1] != "hdparm -S 0 /dev/sda" || len(f.calls) != 2 {
		t.Fatalf("%+v %v %q", r, err, f.calls)
	}
}

func TestSetPowerRejects(t *testing.T) {
	ok, _ := diskpower.ForProfile(diskpower.Balanced, true)
	cases := []struct {
		name, id string
		s        diskpower.Settings
		fail     string
		code     string
	}{
		{"path in id", "../sda", ok, "", CodeInvalid},
		{"unknown disk", "wwn-0x2", ok, "", CodeInvalid},
		{"usb stick", "usb-stick", ok, "", CodeInvalid},
		{"bad standby", "wwn-0x1", diskpower.Settings{Profile: diskpower.Custom, StandbyMinutes: 7}, "", CodeInvalid},
		{"hdparm fails", "wwn-0x1", ok, "hdparm -S", CodeInternal},
	}
	for _, c := range cases {
		f := &fakeDrive{apm: apm254, fail: c.fail}
		m := newPower(t, f)
		_, err := m.SetPower(context.Background(), c.id, c.s)
		var he *Error
		if !errors.As(err, &he) || he.Code != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
		if _, statErr := os.Stat(m.ConfigPath); statErr == nil {
			t.Errorf("%s: config must not be written", c.name)
		}
	}
}

func TestStandby(t *testing.T) {
	idle := `{"smartctl":{"exit_status":0},"ata_smart_data":{"self_test":{"status":{"value":0}}}}`
	f := &fakeDrive{state: " drive state is:  active/idle\n", caps: idle}
	m := newPower(t, f)
	r, err := m.Standby(context.Background(), "wwn-0x1")
	if err != nil || r.WasStandby || f.calls[len(f.calls)-1] != "hdparm -y /dev/sda" {
		t.Fatalf("%+v %v %q", r, err, f.calls)
	}

	f = &fakeDrive{state: " drive state is:  standby\n"}
	r, err = newPower(t, f).Standby(context.Background(), "wwn-0x1")
	if err != nil || !r.WasStandby || len(f.calls) != 1 {
		t.Fatalf("already asleep: %+v %v %q", r, err, f.calls)
	}

	f = &fakeDrive{state: " drive state is:  active/idle\n", caps: strings.Replace(idle, `"value":0`, `"value":249`, 1)}
	_, err = newPower(t, f).Standby(context.Background(), "wwn-0x1")
	var he *Error
	if !errors.As(err, &he) || he.Code != CodeTestRunning {
		t.Fatalf("running test: %v", err)
	}
	for _, c := range f.calls {
		if c == "hdparm -y /dev/sda" {
			t.Fatal("standby sent during a self-test")
		}
	}
}

func TestApplyAllUsesStoredOrDefault(t *testing.T) {
	f := &fakeDrive{apm: apm254}
	m := newPower(t, f)
	m.ApplyAll(context.Background())
	// System disk without an entry: balanced = APM 254, never spin down; the
	// USB stick is skipped.
	if strings.Join(f.calls, "|") != "hdparm -B /dev/sda|hdparm -B 254 /dev/sda|hdparm -S 0 /dev/sda" {
		t.Fatalf("default %q", f.calls)
	}
	diskpower.Save(m.ConfigPath, diskpower.Config{Disks: map[string]diskpower.Settings{
		"wwn-0x1": {Profile: diskpower.Custom, StandbyMinutes: 30}}})
	f.calls = nil
	m.ApplyAll(context.Background())
	if strings.Join(f.calls, "|") != "hdparm -S 241 /dev/sda" {
		t.Fatalf("stored %q", f.calls)
	}
}
