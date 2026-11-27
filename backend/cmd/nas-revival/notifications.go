package main

import (
	"slices"

	"nas-revival/backend/internal/notify"
	"nas-revival/backend/internal/system"
)

// notifyProblems turns a health evaluation into the input of the notification
// centre. collectFailed is left out: one slow collection is no event, and a
// failed collection keeps the last problems, so nothing resolves by mistake.
func notifyProblems(h system.Health) ([]notify.Problem, func(code, subject string) bool) {
	ps := make([]notify.Problem, 0, len(h.Problems))
	for _, p := range h.Problems {
		if p.Code != system.ProblemCollectFailed {
			ps = append(ps, notify.Problem{Severity: p.Severity, Code: p.Code, Subject: p.Subject})
		}
	}
	hold := func(code, subject string) bool {
		switch code {
		case system.ProblemSmartFailed, system.ProblemSmartSectors, system.ProblemDiskTempWarn, system.ProblemDiskTempCrit:
			// No SMART report, or the drive sleeps: the problem is unknown, not gone.
			return h.SmartCheckedAt == nil || slices.Contains(h.StandbyDisks, subject)
		}
		return false
	}
	return ps, hold
}
