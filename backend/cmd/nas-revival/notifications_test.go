package main

import (
	"testing"
	"time"

	"nas-revival/backend/internal/system"
)

func TestNotifyProblems(t *testing.T) {
	at := time.Now()
	h := system.Health{SmartCheckedAt: &at, StandbyDisks: []string{"sdb"}, Problems: []system.Problem{
		{Severity: "warn", Code: system.ProblemCollectFailed},
		{Severity: "crit", Code: system.ProblemBackupFailed, Subject: "web"},
	}}
	ps, hold := notifyProblems(h)
	if len(ps) != 1 || ps[0].Code != system.ProblemBackupFailed {
		t.Fatalf("problems = %+v", ps)
	}
	if !hold(system.ProblemSmartSectors, "sdb") || hold(system.ProblemSmartSectors, "sda") || hold(system.ProblemFSWarn, "sdb") {
		t.Fatal("hold wrong for standby/awake disk or non-SMART problem")
	}
	h.SmartCheckedAt = nil
	if _, hold = notifyProblems(h); !hold(system.ProblemDiskTempCrit, "sda") {
		t.Fatal("no SMART report must hold disk problems")
	}
}
