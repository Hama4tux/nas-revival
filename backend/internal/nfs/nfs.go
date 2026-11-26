// Package nfs holds the NFS service settings (NFS-01): which shared folders
// are exported to which client addresses. It renders the exports file the
// helper owns and parses the NFSv4 client list of the kernel. Only NFSv4 is
// served, so the server needs TCP 2049 and no rpcbind (BKP-12: Proxmox
// mounts with vers=4.2).
package nfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/shares"
)

// DefaultConfigPath is the NFS settings file (0644, root, written by the helper).
const DefaultConfigPath = "/etc/nas-revival/nfs.json"

// ExportsPath is the exports file the helper owns; /etc/exports stays untouched.
const ExportsPath = "/etc/exports.d/nas-revival.exports"

// Squash modes. Root squash is the default; no_root_squash is never offered
// because a client's root could then plant setuid files on the NAS.
const (
	SquashRoot = "root" // client root → nobody, other users keep their IDs
	SquashAll  = "all"  // every client user → nobody
)

// Limits.
const (
	MaxExports = 32
	MaxClients = 16
	// Narrowest allowed networks: a whole /8 or the IPv6 internet is not a client.
	MinPrefix4 = 16
	MinPrefix6 = 48
)

// NobodyUID is the anonymous user squashed clients write as.
const NobodyUID = 65534

// Client is one address or network allowed to mount an export.
type Client struct {
	Host     string `json:"host"`
	ReadOnly bool   `json:"readOnly"`
}

// Export publishes one shared folder (by name, never by path).
type Export struct {
	Share   string   `json:"share"`
	Clients []Client `json:"clients"`
	Squash  string   `json:"squash"`
}

// Settings is nfs.json.
type Settings struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Exports []Export `json:"exports"`
}

// Default is what an unconfigured NAS has: NFS off, nothing exported.
func Default() Settings { return Settings{Version: 1, Exports: []Export{}} }

// FieldError names the invalid field.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func bad(field, msg string) error { return &FieldError{field, msg} }

// ParseHost normalises an IPv4/IPv6 address or network. Host names and
// wildcards are refused: a name is only as trustworthy as DNS.
func ParseHost(h string) (string, error) {
	h = strings.TrimSpace(h)
	if strings.Contains(h, "/") {
		p, err := netip.ParsePrefix(h)
		if err != nil || p.Addr().Zone() != "" {
			return "", errors.New("Klient musí být IP adresa nebo síť (např. 192.168.1.20 nebo 192.168.1.0/24).")
		}
		p = p.Masked()
		minBits := MinPrefix6
		if p.Addr().Is4() {
			minBits = MinPrefix4
		}
		if p.Bits() < minBits {
			return "", fmt.Errorf("Síť %s je příliš velká; nejširší povolená je /%d.", p, minBits)
		}
		if p.IsSingleIP() {
			return p.Addr().String(), nil
		}
		return p.String(), nil
	}
	a, err := netip.ParseAddr(h)
	if err != nil || a.Zone() != "" {
		return "", errors.New("Klient musí být IP adresa nebo síť (např. 192.168.1.20 nebo 192.168.1.0/24).")
	}
	if a.IsUnspecified() || a.IsMulticast() {
		return "", errors.New("Tuto adresu nelze použít jako klienta.")
	}
	return a.Unmap().String(), nil
}

// Validate normalises s and checks every field that needs no system state.
func Validate(s Settings) (Settings, error) {
	s.Version = 1
	if s.Exports == nil {
		s.Exports = []Export{}
	}
	if len(s.Exports) > MaxExports {
		return s, bad("exports", fmt.Sprintf("Nejvýše %d exportů.", MaxExports))
	}
	out := make([]Export, 0, len(s.Exports))
	seen := map[string]bool{}
	for _, e := range s.Exports {
		if !shares.ValidName(e.Share) {
			return s, bad("exports", "Neplatný název sdílené složky.")
		}
		if seen[e.Share] {
			return s, bad("exports", "Sdílená složka "+e.Share+" je exportovaná dvakrát.")
		}
		seen[e.Share] = true
		if e.Squash == "" {
			e.Squash = SquashRoot
		}
		if e.Squash != SquashRoot && e.Squash != SquashAll {
			return s, bad("exports", "Neznámé mapování uživatelů.")
		}
		if len(e.Clients) == 0 {
			return s, bad("exports", "Export "+e.Share+" nemá žádného klienta.")
		}
		if len(e.Clients) > MaxClients {
			return s, bad("exports", fmt.Sprintf("Export %s má víc než %d klientů.", e.Share, MaxClients))
		}
		cs := make([]Client, 0, len(e.Clients))
		hosts := map[string]bool{}
		for _, c := range e.Clients {
			h, err := ParseHost(c.Host)
			if err != nil {
				return s, bad("exports", e.Share+": "+err.Error())
			}
			if hosts[h] {
				return s, bad("exports", e.Share+": klient "+h+" je uvedený dvakrát.")
			}
			hosts[h] = true
			cs = append(cs, Client{Host: h, ReadOnly: c.ReadOnly})
		}
		e.Clients = cs
		out = append(out, e)
	}
	s.Exports = out
	return s, nil
}

// Load reads nfs.json; a missing file gives Default.
func Load(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return Parse(b)
}

// Parse decodes and validates nfs.json.
func Parse(b []byte) (Settings, error) {
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version != 1 {
		return s, fmt.Errorf("nfs: unsupported version %d", s.Version)
	}
	return Validate(s)
}

// Save writes nfs.json atomically.
func Save(path string, s Settings) error {
	s.Version = 1
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o644)
}

// Resolved is an export joined with its shared folder.
type Resolved struct {
	Export
	Path string
	// ShareReadOnly forces every client to read-only.
	ShareReadOnly bool
}

// Resolve joins the exports with shares.json. Exports whose folder no longer
// exists are returned in missing and are not rendered.
func Resolve(s Settings, cfg shares.Config) (ok []Resolved, missing []string) {
	for _, e := range s.Exports {
		i := cfg.Find(e.Share)
		if i < 0 {
			missing = append(missing, e.Share)
			continue
		}
		sh := cfg.Shares[i]
		ok = append(ok, Resolved{Export: e, Path: sh.Path(), ShareReadOnly: sh.ReadOnly})
	}
	return ok, missing
}

// Render returns the exports file. When NFS is off it exports nothing, so
// starting nfs-server by hand (Services app) cannot publish folders.
// Squashed clients write as nobody with the group nas-users (anonGID), which
// the share folders grant read/write; the folder permissions of SMB users
// therefore stay intact.
func Render(enabled bool, exps []Resolved, anonGID int) string {
	var b strings.Builder
	b.WriteString("# Generated by NAS Revival from " + DefaultConfigPath + ". Do not edit; changes are overwritten.\n")
	b.WriteString("# Change NFS exports in the web UI (Services → NFS).\n")
	if !enabled {
		return b.String()
	}
	for _, e := range exps {
		b.WriteString(e.Path)
		for _, c := range e.Clients {
			mode := "rw"
			if c.ReadOnly || e.ShareReadOnly {
				mode = "ro"
			}
			squash := "root_squash"
			if e.Squash == SquashAll {
				squash = "all_squash"
			}
			b.WriteString(" " + c.Host + "(" + mode + ",sync,secure,no_subtree_check,sec=sys," + squash +
				",anonuid=" + strconv.Itoa(NobodyUID) + ",anongid=" + strconv.Itoa(anonGID) + ")")
		}
		b.WriteString("\n")
	}
	return b.String()
}
