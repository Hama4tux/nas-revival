package helper

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"nas-revival/backend/internal/atomicfile"
)

// MaxHostname is one DNS label. Names over 15 characters work, but Windows
// shows SMB names cut to 15 (NetBIOS), so the UI warns about them.
const MaxHostname = 63

var hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateHostname lowercases name and checks it is a single RFC 1123 label
// that is not purely numeric (it would look like an IP address part).
func ValidateHostname(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return "", invalid("hostname", "Zadejte název NASu.")
	}
	if len(n) > MaxHostname {
		return "", invalid("hostname", "Název může mít nejvýše 63 znaků.")
	}
	if !hostnameRe.MatchString(n) {
		return "", invalid("hostname", "Název smí obsahovat jen písmena a–z, číslice a pomlčku (ne na začátku ani na konci).")
	}
	if strings.Trim(n, "0123456789") == "" {
		return "", invalid("hostname", "Název nesmí být jen číslo.")
	}
	if n == "localhost" {
		return "", invalid("hostname", "Název localhost není povolený.")
	}
	return n, nil
}

// HostnameState is the result of net.hostname.
type HostnameState struct {
	Hostname string `json:"hostname"`
}

// HostnameManager changes the host name through systemd-hostnamed (it writes
// /etc/hostname) and keeps the Debian 127.0.1.1 line of /etc/hosts in step, so
// sudo and local name lookups keep resolving the new name.
type HostnameManager struct {
	HostsFile string // /etc/hosts
	Run       Runner
	mu        sync.Mutex
}

// Set applies name as the static and transient host name.
func (m *HostnameManager) Set(ctx context.Context, name, user string) (HostnameState, error) {
	n, err := ValidateHostname(name)
	if err != nil {
		return HostnameState{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.Run(ctx, "hostnamectl", "--static", "--transient", "set-hostname", n); err != nil {
		slog.Error("hostnamectl failed", "err", err)
		return HostnameState{}, internalErr("Název NASu se nepodařilo změnit.")
	}
	if err := m.updateHosts(n); err != nil {
		// The name itself is set; a stale hosts line only slows local lookups.
		slog.Error("hosts update failed", "err", err)
	}
	slog.Info("hostname changed", "hostname", n, "by", user)
	return HostnameState{Hostname: n}, nil
}

func (m *HostnameManager) updateHosts(n string) error {
	data, err := os.ReadFile(m.HostsFile)
	if err != nil {
		return err
	}
	next := RenderHosts(string(data), n)
	if next == string(data) {
		return nil
	}
	return atomicfile.Write(m.HostsFile, []byte(next), 0o644)
}

// RenderHosts replaces the host names of the first 127.0.1.1 line (Debian's
// entry for the own name) or appends one; other lines stay byte for byte.
func RenderHosts(hosts, name string) string {
	lines := strings.SplitAfter(hosts, "\n")
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) > 0 && f[0] == "127.0.1.1" {
			lines[i] = "127.0.1.1\t" + name + "\n"
			return strings.Join(lines, "")
		}
	}
	if hosts != "" && !strings.HasSuffix(hosts, "\n") {
		hosts += "\n"
	}
	return hosts + "127.0.1.1\t" + name + "\n"
}
