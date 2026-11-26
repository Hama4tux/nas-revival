package system

import (
	"time"

	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/hardware"
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
	ProblemBackupFailed   = "backupFailed"
	ProblemBackupStale    = "backupStale"
	ProblemTempWarn       = "tempWarn"
	ProblemTempCrit       = "tempCrit"
	ProblemDiskTempWarn   = "diskTempWarn"
	ProblemDiskTempCrit   = "diskTempCrit"
	// PWR-04: SMART wear counters grow faster than the daily limit.
	ProblemDiskLoadCycles = "diskLoadCycles"
	ProblemDiskStartStop  = "diskStartStop"
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

// Evaluate derives health from a snapshot, the SMART summary (may be nil) and
// the temperature thresholds (HW-01). Crit problems are listed first.
func Evaluate(o Overview, rep *smart.Report, th hardware.Thresholds, at time.Time) Health {
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
	for _, t := range o.Sensors.Temperatures {
		switch {
		case t.MilliCelsius >= int64(th.BoardCritC)*1000:
			add(HealthCrit, ProblemTempCrit, t.Label)
		case t.MilliCelsius >= int64(th.BoardWarnC)*1000:
			add(HealthWarn, ProblemTempWarn, t.Label)
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
			if c := d.TemperatureCelsius; c != nil && *c >= int64(th.DiskCritC) {
				add(HealthCrit, ProblemDiskTempCrit, d.Name)
			} else if c != nil && *c >= int64(th.DiskWarnC) {
				add(HealthWarn, ProblemDiskTempWarn, d.Name)
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

	for _, b := range o.Backups {
		if !b.Enabled {
			continue
		}
		if b.LastResult == "failed" {
			add(HealthCrit, ProblemBackupFailed, b.Name)
		} else if b.StaleAfter > 0 && b.LastOKEnd != nil && at.Sub(*b.LastOKEnd) > b.StaleAfter {
			add(HealthWarn, ProblemBackupStale, b.Name)
		}
	}

	for _, w := range o.DiskWear {
		switch w.Code {
		case diskpower.WarnLoadCycles:
			add(HealthWarn, ProblemDiskLoadCycles, w.Name)
		case diskpower.WarnStartStop:
			add(HealthWarn, ProblemDiskStartStop, w.Name)
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
