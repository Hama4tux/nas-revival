package helper

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const capsIdle = `{"smartctl":{"exit_status":0},"ata_smart_data":{"self_test":{"status":{"value":0},
"polling_minutes":{"short":1,"extended":206}},"capabilities":{"self_tests_supported":true}}}`

type smartCall struct{ args string }

func newSmart(t *testing.T, caps string, startOut string) (*SmartManager, *[]smartCall) {
	t.Helper()
	calls := &[]smartCall{}
	m := &SmartManager{
		Resolve: func(id string) (SmartDisk, bool, error) {
			switch id {
			case "wwn-0x1":
				return SmartDisk{Name: "sda", Capable: true}, true, nil
			case "usb-stick":
				return SmartDisk{Name: "sdb"}, true, nil
			}
			return SmartDisk{}, false, nil
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "smartctl" {
				t.Fatalf("unexpected command %s", name)
			}
			*calls = append(*calls, smartCall{strings.Join(args, " ")})
			if args[1] == "-c" {
				return []byte(caps), nil
			}
			return []byte(startOut), nil
		},
	}
	return m, calls
}

func TestSmartTestStartsLongTest(t *testing.T) {
	m, calls := newSmart(t, capsIdle, `{"smartctl":{"exit_status":0}}`)
	r, err := m.Test(context.Background(), "wwn-0x1", "long")
	if err != nil || r.Type != "long" || r.EstimatedMinutes == nil || *r.EstimatedMinutes != 206 {
		t.Fatalf("%+v %v", r, err)
	}
	if len(*calls) != 2 || (*calls)[0].args != "-j -c /dev/sda" || (*calls)[1].args != "-j -t long /dev/sda" {
		t.Fatalf("calls %+v", *calls)
	}
}

func TestSmartTestRejects(t *testing.T) {
	running := strings.Replace(capsIdle, `"value":0`, `"value":249`, 1)
	cases := []struct {
		name, id, typ, caps, code string
	}{
		{"bad type", "wwn-0x1", "conveyance", capsIdle, CodeInvalid},
		{"path in id", "../../dev/sda", "short", capsIdle, CodeInvalid},
		{"unknown disk", "wwn-0x2", "short", capsIdle, CodeInvalid},
		{"usb stick", "usb-stick", "short", capsIdle, CodeInvalid},
		{"already running", "wwn-0x1", "short", running, CodeTestRunning},
		{"no self-tests", "wwn-0x1", "short", `{"smartctl":{"exit_status":0},"ata_smart_data":{"capabilities":{"self_tests_supported":false}}}`, CodeInvalid},
		{"device not opened", "wwn-0x1", "short", `{"smartctl":{"exit_status":2}}`, CodeInternal},
	}
	for _, c := range cases {
		m, calls := newSmart(t, c.caps, `{"smartctl":{"exit_status":0}}`)
		_, err := m.Test(context.Background(), c.id, c.typ)
		var he *Error
		if !errors.As(err, &he) || he.Code != c.code {
			t.Errorf("%s: err %v, want %s", c.name, err, c.code)
		}
		for _, call := range *calls {
			if strings.Contains(call.args, "-t") {
				t.Errorf("%s: test must not start (%s)", c.name, call.args)
			}
		}
	}
}

// A non-zero exit with only health bits set (e.g. bit 6: errors in the device
// log) is not a failure of the start command.
func TestSmartTestIgnoresHealthBits(t *testing.T) {
	m, _ := newSmart(t, capsIdle, `{"smartctl":{"exit_status":64}}`)
	m.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[1] == "-c" {
			return []byte(capsIdle), nil
		}
		return []byte(`{"smartctl":{"exit_status":64}}`), &exec.ExitError{}
	}
	if _, err := m.Test(context.Background(), "wwn-0x1", "short"); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcherSmartTest(t *testing.T) {
	m, _ := newSmart(t, capsIdle, `{"smartctl":{"exit_status":0}}`)
	d := &Dispatcher{Smart: m}
	res, err := d.Handle(context.Background(), request{Op: "smart.test", Args: []byte(`{"diskId":"wwn-0x1","type":"short"}`)})
	if err != nil || res.(SmartTestResult).Type != "short" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "smart.test", Args: []byte(`{"diskId":"wwn-0x1","type":"short","path":"/dev/sdb"}`)}); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}
