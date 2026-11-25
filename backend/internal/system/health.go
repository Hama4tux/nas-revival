package system

import (
	"time"

	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/smart"
)

// Health statuses, worst wins. Unknown means the snapshot could not be
// refreshed and the problems are those of the last good one.
const (
	HealthOK      = "ok"
	HealthWarn    = "warn"
	HealthCrit    = "crit"
	HealthUnknown = "unknown"
)

// Problem codes; the UI keeps one Czech text per code.
const (
	ProblemSensorsFault   = "sensorsFault"
	ProblemSensorsUnknown = "sensorsUnknown"
	ProblemFanStopped     = "fanStopped"
	ProblemSmartFailed    = "smartFailed"
	ProblemSmartSectors   = "smartSectors"
	ProblemFSCritical     = "fsCritical"
	ProblemFSWarn         = "fsWarn"
	ProblemServiceFailed  = "serviceFailed"
	ProblemCollectFailed  = "collectFailed"
)

// Filesystem usage thresholds in percent (strictly above).
const (
	FSWarnPercent = 85
	FSCritPercent = 95
)

type Problem struct {
	Severity string `json:"severity"` // warn | crit
	Code     string `json:"code"`
	Subject  string `json:"subject,omitempty"` // disk, mount, service or fan label
}

type Health struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checkedAt"`
	// SmartCheckedAt is when the SMART data behind the problems was read
	// (cached for minutes); null when there is no SMART report.
	SmartCheckedAt *time.Time `json:"smartCheckedAt"`
	Problems       []Problem  `json:"problems"`
}

// Evaluate derives health from a snapshot and the SMART summary (may be nil).
// Crit problems are listed first.
func Evaluate(o Overview, rep *smart.Report, at time.Time) Health {
	h := Health{CheckedAt: at, Problems: []Problem{}}
	var crit, warn []Problem
	add := func(sev, code, subject string) {
		p := Problem{Severity: sev, Code: code, Subject: subject}
		if sev == HealthCrit {
			crit = append(crit, p)
		} else {
			warn = append(warn, p)
		}
	}

	switch o.Sensors.Status {
	case hal.SensorOK:
	case hal.SensorFault:
		add(HealthCrit, ProblemSensorsFault, "")
	default:
		add(HealthWarn, ProblemSensorsUnknown, "")
	}
	for _, f := range o.Sensors.Fans {
		if f.RPM == 0 {
			add(HealthWarn, ProblemFanStopped, f.Label)
		}
	}

	if rep != nil {
		t := rep.CheckedAt
		h.SmartCheckedAt = &t
		for _, d := range rep.Disks {
			if d.Standby {
				continue
			}
			if d.Health == "failed" {
				add(HealthCrit, ProblemSmartFailed, d.Name)
			} else if positive(d.ReallocatedSectors) || positive(d.PendingSectors) {
				add(HealthWarn, ProblemSmartSectors, d.Name)
			}
		}
	}

	for _, f := range o.Storage.Filesystems {
		if f.TotalBytes == 0 {
			continue
		}
		p := float64(f.UsedBytes) / float64(f.TotalBytes) * 100
		if p > FSCritPercent {
			add(HealthCrit, ProblemFSCritical, f.Mount)
		} else if p > FSWarnPercent {
			add(HealthWarn, ProblemFSWarn, f.Mount)
		}
	}

	for _, s := range o.Services {
		if s.State == "failed" {
			add(HealthCrit, ProblemServiceFailed, s.Label)
		}
	}

	h.Problems = append(append(h.Problems, crit...), warn...)
	switch {
	case len(crit) > 0:
		h.Status = HealthCrit
	case len(warn) > 0:
		h.Status = HealthWarn
	default:
		h.Status = HealthOK
	}
	return h
}

func positive(v *int64) bool { return v != nil && *v > 0 }
