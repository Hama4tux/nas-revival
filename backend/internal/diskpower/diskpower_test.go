package diskpower

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStandbyCode(t *testing.T) {
	cases := map[int]int{0: 0, 10: 120, 20: 240, 30: 241, 60: 242, 120: 244, 300: 250}
	for min, want := range cases {
		if got, err := StandbyCode(min); err != nil || got != want {
			t.Errorf("StandbyCode(%d) = %d, %v; want %d", min, got, err, want)
		}
	}
	for _, bad := range []int{-1, 7, 45, 330, 1000} {
		if _, err := StandbyCode(bad); err == nil {
			t.Errorf("StandbyCode(%d) accepted", bad)
		}
	}
}

func TestProfiles(t *testing.T) {
	sys := Default(true)
	if sys.Profile != Balanced || sys.StandbyMinutes != 0 || sys.APM == nil || *sys.APM != 254 {
		t.Fatalf("system default %+v", sys)
	}
	if data := Default(false); data.StandbyMinutes != 60 {
		t.Fatalf("data default %+v", data)
	}
	if _, err := Resolve("turbo", 0, nil, false); err == nil {
		t.Fatal("unknown profile accepted")
	}
	// A fixed profile ignores the custom values.
	s, err := Resolve(Eco, 300, nil, false)
	if err != nil || s.StandbyMinutes != 20 || *s.APM != 128 {
		t.Fatalf("eco %+v %v", s, err)
	}
	if _, err := Resolve(Custom, 10, intp(0), false); err == nil {
		t.Fatal("APM 0 accepted")
	}
	s, err = Resolve(Custom, 120, nil, true)
	if err != nil || s.APM != nil || s.StandbyMinutes != 120 {
		t.Fatalf("custom %+v %v", s, err)
	}
}

func TestParseState(t *testing.T) {
	cases := map[string]string{
		"\n/dev/sda:\n drive state is:  active/idle\n": StateActive,
		"\n/dev/sda:\n drive state is:  standby\n":     StateStandby,
		"\n/dev/sda:\n drive state is:  sleeping\n":    StateSleep,
		"\n/dev/sda:\n drive state is:  unknown\n":     StateUnknown,
		"garbage": StateUnknown,
	}
	for in, want := range cases {
		if got := ParseState(in); got != want {
			t.Errorf("ParseState(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestParseAPM(t *testing.T) {
	if l, sup, ok := ParseAPM("\n/dev/sda:\n APM_level\t= 254\n"); !ok || !sup || *l != 254 {
		t.Fatalf("254: %v %v %v", l, sup, ok)
	}
	if l, sup, ok := ParseAPM("\n/dev/sdb:\n APM_level\t= not supported\n"); !ok || sup || l != nil {
		t.Fatalf("not supported: %v %v %v", l, sup, ok)
	}
	if l, _, ok := ParseAPM(" APM_level = off\n"); !ok || *l != 255 {
		t.Fatalf("off: %v %v", l, ok)
	}
	if _, _, ok := ParseAPM("nothing"); ok {
		t.Fatal("garbage parsed")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disks.json")
	c, err := Load(path)
	if err != nil || len(c.Disks) != 0 {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	c.Disks["wwn-0x1"] = Settings{Profile: Custom, StandbyMinutes: 30, APM: intp(200)}
	c.Disks["bad"] = Settings{Profile: Custom, StandbyMinutes: 7}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", st.Mode())
	}
	got, err := Load(path)
	if err != nil || len(got.Disks) != 1 || *got.Disks["wwn-0x1"].APM != 200 {
		t.Fatalf("load %+v %v", got, err)
	}
}

func TestReaderDoesNotAskSleepingDriveForAPM(t *testing.T) {
	state := " drive state is:  active/idle\n"
	var calls []string
	r := &Reader{Now: time.Now, Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "-C" {
			return []byte(state), nil
		}
		return []byte(" APM_level\t= 254\n"), nil
	}}
	l := r.Read(context.Background(), "sda")
	if l.State != StateActive || l.APMLevel == nil || *l.APMLevel != 254 || !*l.APMSupported {
		t.Fatalf("active %+v", l)
	}
	state, calls = " drive state is:  standby\n", nil
	l = r.Read(context.Background(), "sda")
	if l.State != StateStandby || len(calls) != 1 || *l.APMLevel != 254 {
		t.Fatalf("standby %+v %q", l, calls)
	}
}
