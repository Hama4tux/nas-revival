package helper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/services"
)

// Log Center (ADR 0015): journal.query, journal.retention.get/set.

const (
	// DefaultJournalDropin is the journald drop-in the helper owns.
	DefaultJournalDropin = "/etc/systemd/journald.conf.d/50-nas-revival.conf"
	journaldUnit         = "systemd-journald.service"
	queryTimeout         = 20 * time.Second
)

// Retention limits the journal offers.
var (
	retentionSizes = []string{"32M", "64M", "128M", "256M"}
	retentionAges  = []string{"none", "1week", "1month", "3month", "1year"}
)

// JournalRetention is the size and age limit of the persistent journal.
type JournalRetention struct {
	MaxUse       string `json:"maxUse"`       // 32M, 64M, 128M, 256M
	MaxRetention string `json:"maxRetention"` // none, 1week, 1month, 3month, 1year
	// UsageBytes is the current journal size; nil when journalctl cannot tell.
	UsageBytes *uint64 `json:"usageBytes,omitempty"`
}

// JournalResult is the answer of journal.query.
type JournalResult struct {
	Entries []services.QueryEntry `json:"entries"`
	More    bool                  `json:"more"`
}

// LogManager serves journal.query and journal.retention.*.
type LogManager struct {
	Run    Runner
	Dropin string // DefaultJournalDropin

	mu sync.Mutex // one retention change at a time
}

// exitCoder is implemented by *exec.ExitError.
type exitCoder interface{ ExitCode() int }

// Query runs one validated journal query.
func (m *LogManager) Query(ctx context.Context, q services.Query) (JournalResult, error) {
	names := make([]string, len(services.Catalog))
	for i, d := range services.Catalog {
		names[i] = d.Name
	}
	args, err := services.BuildJournalArgs(q, names)
	if err != nil {
		var qe *services.QueryError
		if errors.As(err, &qe) {
			return JournalResult{}, invalid(qe.Field, qe.Msg)
		}
		return JournalResult{}, internalErr("Záznamy nelze načíst.")
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	raw, err := m.Run(ctx, "journalctl", args...)
	if err != nil {
		// journalctl exits with 1 and prints nothing when --grep matches no entry.
		var ec exitCoder
		if !(errors.As(err, &ec) && ec.ExitCode() == 1 && len(bytes.TrimSpace(raw)) == 0 && q.Text != "") && len(raw) == 0 {
			slog.Warn("helper: journalctl query failed", "err", err)
			return JournalResult{}, internalErr("Záznamy nelze načíst.")
		}
	}
	entries, more := services.ParseQueryJournal(raw, q.EffectiveLimit())
	return JournalResult{Entries: entries, More: more}, nil
}

func (m *LogManager) dropin() string {
	if m.Dropin != "" {
		return m.Dropin
	}
	return DefaultJournalDropin
}

func oneOf(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ParseJournalDropin reads the limits from the drop-in; missing or unknown
// values give the defaults (64M, size only).
func ParseJournalDropin(raw []byte) JournalRetention {
	r := JournalRetention{MaxUse: "64M", MaxRetention: "none"}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "SystemMaxUse":
			if oneOf(v, retentionSizes) {
				r.MaxUse = v
			}
		case "MaxRetentionSec":
			if v == "0" {
				v = "none"
			}
			if oneOf(v, retentionAges) {
				r.MaxRetention = v
			}
		}
	}
	return r
}

// RenderJournalDropin is the content of the drop-in for r (the same text as
// deploy/debian/50-nas-revival-journald.conf for the defaults).
func RenderJournalDropin(r JournalRetention) []byte {
	var b strings.Builder
	b.WriteString("# /etc/systemd/journald.conf.d/50-nas-revival.conf\n" +
		"# The journal shares the only disk with the data (ADR 0007): flush non-critical\n" +
		"# messages every 15 min instead of 5 (crit/alert/emerg are still synced at once)\n" +
		"# and cap the persistent journal so it cannot fill the root filesystem.\n" +
		"# Size and age limits are managed by the web administration (Log Center, ADR 0015).\n" +
		"[Journal]\nSyncIntervalSec=15min\nSystemMaxUse=" + r.MaxUse + "\n")
	if r.MaxRetention != "none" {
		b.WriteString("MaxRetentionSec=" + r.MaxRetention + "\n")
	}
	return []byte(b.String())
}

var diskUsageRE = regexp.MustCompile(`take up ([0-9]+(?:\.[0-9]+)?)([BKMGT]?)`)

// ParseDiskUsage reads `journalctl --disk-usage` ("... take up 59.1M in ...").
func ParseDiskUsage(out string) (uint64, bool) {
	m := diskUsageRE.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	mult := map[string]float64{"": 1, "B": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}[m[2]]
	return uint64(f*mult + 0.5), true
}

// RetentionGet reports the configured limits and the current journal size.
func (m *LogManager) RetentionGet(ctx context.Context) (JournalRetention, error) {
	raw, err := os.ReadFile(m.dropin())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("helper: journald drop-in unreadable", "err", err)
		return JournalRetention{}, internalErr("Nastavení záznamů nelze přečíst.")
	}
	r := ParseJournalDropin(raw)
	if out, err := m.Run(ctx, "journalctl", "--disk-usage"); err == nil {
		if n, ok := ParseDiskUsage(string(out)); ok {
			r.UsageBytes = &n
		}
	}
	return r, nil
}

// RetentionSet validates and stores new limits, restarts journald and
// returns the new state. The caller records the audit entry.
func (m *LogManager) RetentionSet(ctx context.Context, s JournalRetention) (JournalRetention, error) {
	if !oneOf(s.MaxUse, retentionSizes) {
		return JournalRetention{}, invalid("maxUse", "Velikost musí být 32M, 64M, 128M nebo 256M.")
	}
	if !oneOf(s.MaxRetention, retentionAges) {
		return JournalRetention{}, invalid("maxRetention", "Doba uchování musí být none, 1week, 1month, 3month nebo 1year.")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := atomicfile.Write(m.dropin(), RenderJournalDropin(s), 0o644); err != nil {
		slog.Warn("helper: journald drop-in not written", "err", err)
		return JournalRetention{}, internalErr("Nastavení záznamů nelze uložit.")
	}
	if _, err := m.Run(ctx, "systemctl", "restart", "--", journaldUnit); err != nil {
		slog.Warn("helper: journald restart failed", "err", err)
		return JournalRetention{}, internalErr("Nastavení je uloženo, ale systemd-journald se nepodařilo restartovat.")
	}
	return m.RetentionGet(ctx)
}
