package helper

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os/exec"
	"regexp"
	"sync"
)

// CodeTestRunning: a self-test already runs on the disk (one test per disk).
const CodeTestRunning = "test_running"

// SmartDisk is what the helper knows about a disk resolved from its stable ID.
type SmartDisk struct {
	Name    string // kernel name, e.g. sda
	Capable bool   // internal SATA disk that smartctl addresses directly
}

// SmartManager starts SMART self-tests (op smart.test, SMART-11).
type SmartManager struct {
	// Resolve maps a stable disk ID to the kernel device; the request never
	// carries a path.
	Resolve func(id string) (SmartDisk, bool, error)
	Run     Runner
	mu      sync.Mutex
}

type SmartTestResult struct {
	DiskID           string `json:"diskId"`
	Type             string `json:"type"`
	EstimatedMinutes *int   `json:"estimatedMinutes"`
}

// by-id names: letters, digits and . _ : - only.
var diskIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type smartctlCapabilities struct {
	Smartctl struct {
		ExitStatus int `json:"exit_status"`
	} `json:"smartctl"`
	ATAData *struct {
		SelfTest *struct {
			Status *struct {
				Value int `json:"value"`
			} `json:"status"`
			Polling *struct {
				Short    *int `json:"short"`
				Extended *int `json:"extended"`
			} `json:"polling_minutes"`
		} `json:"self_test"`
		Capabilities *struct {
			SelfTests bool `json:"self_tests_supported"`
		} `json:"capabilities"`
	} `json:"ata_smart_data"`
}

// smartctl's exit status is a bit mask; bits 0–2 mean the command itself
// failed (bad arguments, device not opened, SMART command failed). Higher bits
// describe the disk's health and are not an error of the operation.
const smartctlCommandFailed = 0x07

func (m *SmartManager) smartctl(ctx context.Context, args ...string) (smartctlCapabilities, error) {
	var c smartctlCapabilities
	out, err := m.Run(ctx, "smartctl", args...)
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || len(out) == 0) {
		return c, internalErr("Nástroj smartctl nelze spustit.")
	}
	if json.Unmarshal(out, &c) != nil || c.Smartctl.ExitStatus&smartctlCommandFailed != 0 {
		return c, internalErr("Disk odmítl příkaz SMART.")
	}
	return c, nil
}

// Test starts a short or long self-test. Starting wakes a sleeping disk; that
// is the user's explicit request.
func (m *SmartManager) Test(ctx context.Context, id, typ string) (SmartTestResult, error) {
	if typ != "short" && typ != "long" {
		return SmartTestResult{}, invalid("type", "Typ testu musí být short nebo long.")
	}
	if !diskIDRe.MatchString(id) {
		return SmartTestResult{}, invalid("diskId", "Neplatné ID disku.")
	}
	disk, ok, err := m.Resolve(id)
	if err != nil {
		return SmartTestResult{}, internalErr("Seznam disků nelze načíst.")
	}
	if !ok {
		return SmartTestResult{}, invalid("diskId", "Disk nebyl nalezen.")
	}
	if !disk.Capable {
		return SmartTestResult{}, invalid("diskId", "Disk nepodporuje samotest SMART.")
	}
	dev := "/dev/" + disk.Name

	m.mu.Lock()
	defer m.mu.Unlock()
	caps, err := m.smartctl(ctx, "-j", "-c", dev)
	if err != nil {
		return SmartTestResult{}, err
	}
	a := caps.ATAData
	if a == nil || a.Capabilities == nil || !a.Capabilities.SelfTests {
		return SmartTestResult{}, invalid("diskId", "Disk nepodporuje samotest SMART.")
	}
	if a.SelfTest != nil && a.SelfTest.Status != nil && a.SelfTest.Status.Value>>4 == 0xF {
		return SmartTestResult{}, &Error{Code: CodeTestRunning, Message: "Na disku už běží samotest. Počkejte na jeho dokončení."}
	}
	if _, err := m.smartctl(ctx, "-j", "-t", typ, dev); err != nil {
		return SmartTestResult{}, err
	}
	res := SmartTestResult{DiskID: id, Type: typ}
	if a.SelfTest != nil && a.SelfTest.Polling != nil {
		res.EstimatedMinutes = a.SelfTest.Polling.Short
		if typ == "long" {
			res.EstimatedMinutes = a.SelfTest.Polling.Extended
		}
	}
	slog.Info("helper: smart.test started", "disk", id, "device", disk.Name, "type", typ)
	return res, nil
}
