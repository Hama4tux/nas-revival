package helper

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"nas-revival/backend/internal/atomicfile"
)

// ValidateNTP checks the server list (0–4 host names or IP addresses).
func ValidateNTP(servers []string) ([]string, error) {
	if len(servers) > 4 {
		return nil, invalid("ntpServers", "Lze zadat nejvýše 4 servery.")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, s := range servers {
		if !validHostOrIP(s) {
			return nil, invalid("ntpServers", "Neplatný server NTP: "+quoteShort(s))
		}
		k := strings.ToLower(s)
		if seen[k] {
			return nil, invalid("ntpServers", "Server NTP je uveden dvakrát: "+quoteShort(s))
		}
		seen[k] = true
		out = append(out, s)
	}
	return out, nil
}

func validHostOrIP(s string) bool {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Zone() == "" && !a.IsUnspecified() && !a.IsMulticast()
	}
	if s == "" || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// RenderTimesyncd renders the drop-in; an empty list renders "".
func RenderTimesyncd(servers []string) string {
	if len(servers) == 0 {
		return ""
	}
	return "# Managed by nas-revival.\n[Time]\nNTP=" + strings.Join(servers, " ") + "\n"
}

// ParseTimesyncd returns the NTP= servers of a drop-in.
func ParseTimesyncd(data string) []string {
	out := []string{}
	for _, line := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(k) == "NTP" {
			out = append(out, strings.Fields(v)...)
		}
	}
	return out
}

// TimeState is the result of time.get / time.apply.
type TimeState struct {
	NTPServers     []string `json:"ntpServers"`
	DefaultServers bool     `json:"defaultServers"`
	Synchronized   bool     `json:"synchronized"`
	ServerName     string   `json:"serverName"`
	ServerAddress  string   `json:"serverAddress"`
	Timezone       string   `json:"timezone"`
}

// TimeManager owns the timesyncd drop-in.
type TimeManager struct {
	Dropin string // full path of the drop-in file
	Run    Runner
}

// parseKV parses "Key=value" lines as printed by systemctl show style tools.
func parseKV(out []byte) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func (t *TimeManager) Get(ctx context.Context) (TimeState, error) {
	st := TimeState{NTPServers: []string{}}
	if out, err := t.Run(ctx, "timedatectl", "show", "-p", "Timezone", "-p", "NTPSynchronized"); err == nil {
		kv := parseKV(out)
		st.Timezone = kv["Timezone"]
		st.Synchronized = kv["NTPSynchronized"] == "yes"
	}
	var fallback []string
	if out, err := t.Run(ctx, "timedatectl", "show-timesync", "-p", "ServerName", "-p", "ServerAddress",
		"-p", "SystemNTPServers", "-p", "FallbackNTPServers"); err == nil {
		kv := parseKV(out)
		st.ServerName, st.ServerAddress = kv["ServerName"], kv["ServerAddress"]
		fallback = strings.Fields(kv["SystemNTPServers"])
		if len(fallback) == 0 {
			fallback = strings.Fields(kv["FallbackNTPServers"])
		}
	}
	data, err := os.ReadFile(t.Dropin)
	custom := []string{}
	if err == nil {
		custom = ParseTimesyncd(string(data))
	}
	if len(custom) > 0 {
		st.NTPServers = custom
	} else {
		st.DefaultServers = true
		if fallback != nil {
			st.NTPServers = fallback
		}
	}
	return st, nil
}

func (t *TimeManager) Apply(ctx context.Context, servers []string) (TimeState, error) {
	servers, err := ValidateNTP(servers)
	if err != nil {
		return TimeState{}, err
	}
	if len(servers) == 0 {
		if err := os.Remove(t.Dropin); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return TimeState{}, internalErr("Nelze odstranit nastavení NTP.")
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(t.Dropin), 0o755); err != nil {
			return TimeState{}, internalErr("Nelze vytvořit adresář nastavení NTP.")
		}
		if err := atomicfile.Write(t.Dropin, []byte(RenderTimesyncd(servers)), 0o644); err != nil {
			return TimeState{}, internalErr("Nelze zapsat nastavení NTP.")
		}
	}
	if _, err := t.Run(ctx, "systemctl", "restart", "systemd-timesyncd"); err != nil {
		return TimeState{}, internalErr("Službu časové synchronizace se nepodařilo restartovat.")
	}
	return t.Get(ctx)
}
