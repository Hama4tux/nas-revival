// Package smart reads a SMART health summary through smartctl's JSON output.
package smart

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Disk struct {
	Name               string  `json:"name"`
	Model              *string `json:"model"`
	Health             string  `json:"health"`
	TemperatureCelsius *int64  `json:"temperatureCelsius"`
	PowerOnHours       *int64  `json:"powerOnHours"`
	ReallocatedSectors *int64  `json:"reallocatedSectors"`
	PendingSectors     *int64  `json:"pendingSectors"`
	StartStopCount     *int64  `json:"startStopCount"` // attribute 4
	LoadCycleCount     *int64  `json:"loadCycleCount"` // attribute 193
	Standby            bool    `json:"standby"`
}

type Report struct {
	CheckedAt time.Time `json:"checkedAt"`
	Disks     []Disk    `json:"disks"`
	Error     string    `json:"error,omitempty"`
}

// Runner executes smartctl for one device name such as "sda". It returns the
// JSON output; smartctl's non-zero exit bitmask is not treated as failure.
type Runner func(ctx context.Context, device string) ([]byte, error)

var errUnavailable = errors.New("smartctl unavailable")

func ExecRunner(ctx context.Context, device string) ([]byte, error) {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// -n standby: do not spin up a sleeping disk just to draw a dashboard.
	out, err := exec.CommandContext(ctx, path, "-j", "-n", "standby", "-H", "-i", "-A", "/dev/"+device).Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, err
	}
	return out, nil
}

// Service caches reports so the dashboard cannot trigger a SMART query storm.
type Service struct {
	Run      Runner
	Devices  func() []string
	TTL      time.Duration
	Now      func() time.Time
	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

func (s *Service) Report(ctx context.Context) Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.Now().Sub(s.cachedAt) < s.TTL {
		return *s.cached
	}
	r := Report{CheckedAt: s.Now().UTC(), Disks: []Disk{}}
	for _, dev := range s.Devices() {
		raw, err := s.Run(ctx, dev)
		if errors.Is(err, errUnavailable) {
			r.Error = "smartctl_unavailable"
			r.Disks = []Disk{}
			break
		}
		if err != nil {
			slog.Warn("smartctl failed", "device", dev, "err", err)
			r.Disks = append(r.Disks, Disk{Name: dev, Health: "unknown"})
			continue
		}
		r.Disks = append(r.Disks, Parse(dev, raw))
	}
	s.cached, s.cachedAt = &r, s.Now()
	return r
}

type smartctlJSON struct {
	Smartctl struct {
		ExitStatus int `json:"exit_status"`
		Messages   []struct {
			String string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
	ModelName   string `json:"model_name"`
	SmartStatus *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current int64 `json:"current"`
	} `json:"temperature"`
	PowerOnTime *struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	ATAAttributes *struct {
		Table []struct {
			ID  int `json:"id"`
			Raw struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

// Parse converts smartctl JSON into a Disk; unknown values stay nil.
func Parse(name string, raw []byte) Disk {
	d := Disk{Name: name, Health: "unknown"}
	var j smartctlJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return d
	}
	for _, m := range j.Smartctl.Messages {
		if strings.Contains(strings.ToUpper(m.String), "STANDBY") {
			d.Standby = true
		}
	}
	if j.ModelName != "" {
		d.Model = &j.ModelName
	}
	if j.SmartStatus != nil {
		if j.SmartStatus.Passed {
			d.Health = "passed"
		} else {
			d.Health = "failed"
		}
	}
	if j.Temperature != nil {
		v := j.Temperature.Current
		d.TemperatureCelsius = &v
	}
	if j.PowerOnTime != nil {
		v := j.PowerOnTime.Hours
		d.PowerOnHours = &v
	}
	if j.ATAAttributes != nil {
		for _, a := range j.ATAAttributes.Table {
			v := a.Raw.Value
			switch a.ID {
			case 5:
				d.ReallocatedSectors = &v
			case 197:
				d.PendingSectors = &v
			case 4:
				d.StartStopCount = &v
			case 193:
				d.LoadCycleCount = &v
			}
		}
	}
	return d
}
