package system

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type Service struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	State string `json:"state"`
}

// Unit is an allowlisted systemd unit shown on the dashboard.
type Unit struct {
	Name  string
	Label string
}

// DefaultUnits are the services relevant for this NAS. Units that are not
// installed are omitted from the response rather than shown as failed.
var DefaultUnits = []Unit{
	{"ssh.service", "SSH"},
	{"smbd.service", "Samba"},
	{"nfs-server.service", "NFS"},
	{"systemd-resolved.service", "DNS"},
	{"systemd-timesyncd.service", "Čas (NTP)"},
	{"nsa310-leds.service", "LED"},
	{"nsa310-fan.service", "Ventilátor"},
	{"nsa310-button.service", "Tlačítko COPY"},
	{"smartmontools.service", "SMART dohled"},
	{"cron.service", "Plánovač"},
}

// ServiceChecker queries systemd with a fixed argument list; unit names never
// come from a request.
type ServiceChecker struct {
	Units []Unit
	Run   func(ctx context.Context, args ...string) ([]byte, error)
}

func NewServiceChecker(units []Unit) *ServiceChecker {
	return &ServiceChecker{Units: units, Run: runSystemctl}
}

func runSystemctl(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", args...).Output()
}

func (s *ServiceChecker) Check(ctx context.Context) []Service {
	out := []Service{}
	if len(s.Units) == 0 {
		return out
	}
	args := []string{"show", "--property=Id,LoadState,ActiveState", "--"}
	for _, u := range s.Units {
		args = append(args, u.Name)
	}
	raw, err := s.Run(ctx, args...)
	if err != nil && len(raw) == 0 {
		slog.Warn("systemctl show failed", "err", err)
		for _, u := range s.Units {
			out = append(out, Service{Name: u.Name, Label: u.Label, State: "unknown"})
		}
		return out
	}
	// systemctl prints one property block per unit, in argument order,
	// separated by blank lines.
	blocks := parseShow(raw)
	byID := map[string]map[string]string{}
	for _, b := range blocks {
		byID[b["Id"]] = b
	}
	for i, u := range s.Units {
		b, ok := byID[u.Name]
		if !ok && i < len(blocks) {
			b, ok = blocks[i], true
		}
		if !ok {
			out = append(out, Service{Name: u.Name, Label: u.Label, State: "unknown"})
			continue
		}
		if b["LoadState"] == "not-found" {
			continue
		}
		out = append(out, Service{Name: u.Name, Label: u.Label, State: mapActiveState(b["ActiveState"])})
	}
	return out
}

func parseShow(raw []byte) []map[string]string {
	var blocks []map[string]string
	cur := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = map[string]string{}
			}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur[k] = v
		}
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	return blocks
}

func mapActiveState(s string) string {
	switch s {
	case "active", "reloading":
		return "active"
	case "inactive":
		return "inactive"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}
