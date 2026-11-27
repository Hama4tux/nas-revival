// Package firewall holds the firewall settings (SEC-12, ADR 0014): allowed
// IPv4 sources per service from a fixed catalogue, the anti-lockout check
// and the nft script rendered from them. It is pure; the helper loads the
// script and runs the confirm-or-rollback cycle.
package firewall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"nas-revival/backend/internal/atomicfile"
)

// DefaultConfigPath is the settings file (0644, root, written by the helper on confirm).
const DefaultConfigPath = "/etc/nas-revival/firewall.json"

// BootFile is loaded at boot by nas-revival-firewall.service.
const BootFile = "/etc/nftables.d/nas-revival.nft"

// Table is the nft table the helper owns.
const Table = "nas_revival"

// MaxSources is the most addresses or networks one service may allow.
const MaxSources = 16

// Port is one TCP/UDP port of a service.
type Port struct {
	Proto string `json:"proto"`
	Port  int    `json:"port"`
}

// Service is one entry of the fixed catalogue.
type Service struct {
	Key   string `json:"key"`
	Ports []Port `json:"ports"`
}

// Catalogue is the fixed list of services, in rule order.
var Catalogue = []Service{
	{Key: "web", Ports: []Port{{"tcp", 8443}}},
	{Key: "ssh", Ports: []Port{{"tcp", 22}}},
	{Key: "smb", Ports: []Port{{"tcp", 445}}},
	{Key: "nfs", Ports: []Port{{"tcp", 2049}, {"tcp", 111}}},
}

// Settings is firewall.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Allow maps a service key to the IPv4 addresses or networks that may
	// reach it; an empty list blocks the service.
	Allow map[string][]string `json:"allow"`
}

// FieldError names the invalid field.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

// Default is a disabled firewall with nothing allowed.
func Default() Settings {
	a := map[string][]string{}
	for _, s := range Catalogue {
		a[s.Key] = []string{}
	}
	return Settings{Allow: a}
}

func parseSource(v string) (netip.Prefix, error) {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "/") {
		p, err := netip.ParsePrefix(v)
		if err != nil || !p.Addr().Is4() {
			return netip.Prefix{}, errors.New("bad prefix")
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(v)
	if err != nil || !a.Is4() {
		return netip.Prefix{}, errors.New("bad address")
	}
	return netip.PrefixFrom(a, 32), nil
}

func formatSource(p netip.Prefix) string {
	if p.Bits() == 32 {
		return p.Addr().String()
	}
	return p.String()
}

// Validate normalises s (all four keys present, sources sorted, duplicates
// removed, networks reduced to the network address) and checks it. With the
// firewall enabled, web and ssh must allow clientAddr, otherwise the change
// would cut the administrator off; an empty clientAddr skips that check
// (stored settings).
func Validate(s Settings, clientAddr string) (Settings, error) {
	out := Default()
	out.Enabled = s.Enabled
	known := map[string]bool{}
	for _, svc := range Catalogue {
		known[svc.Key] = true
	}
	keys := make([]string, 0, len(s.Allow))
	for k := range s.Allow {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			return s, &FieldError{"allow", "Neznámá služba " + strconv.Quote(k) + "."}
		}
	}
	prefixes := map[string][]netip.Prefix{}
	for _, svc := range Catalogue {
		field := "allow." + svc.Key
		in := s.Allow[svc.Key]
		if len(in) > MaxSources {
			return s, &FieldError{field, "Nejvýše " + strconv.Itoa(MaxSources) + " adres nebo sítí."}
		}
		seen := map[netip.Prefix]bool{}
		var list []netip.Prefix
		for _, v := range in {
			p, err := parseSource(v)
			if err != nil {
				return s, &FieldError{field, "Neplatná adresa nebo síť " + strconv.Quote(v) + " (jen IPv4, např. 192.168.1.0/24)."}
			}
			if !seen[p] {
				seen[p] = true
				list = append(list, p)
			}
		}
		sort.Slice(list, func(i, j int) bool {
			if c := list[i].Addr().Compare(list[j].Addr()); c != 0 {
				return c < 0
			}
			return list[i].Bits() < list[j].Bits()
		})
		// nft refuses overlapping elements in one set: drop sources a wider one already covers.
		list = slices.DeleteFunc(list, func(p netip.Prefix) bool {
			for _, q := range list {
				if q != p && q.Bits() <= p.Bits() && q.Contains(p.Addr()) {
					return true
				}
			}
			return false
		})
		prefixes[svc.Key] = list
		strs := make([]string, len(list))
		for i, p := range list {
			strs[i] = formatSource(p)
		}
		out.Allow[svc.Key] = strs
	}
	if out.Enabled && clientAddr != "" {
		c, err := netip.ParseAddr(clientAddr)
		c = c.Unmap() // ::ffff:a.b.c.d from a dual-stack listener
		if err != nil || !c.Is4() {
			return s, &FieldError{"allow.web", "Adresu vašeho počítače (" + clientAddr + ") nelze ověřit, firewall nelze zapnout."}
		}
		for _, k := range []string{"web", "ssh"} {
			ok := false
			for _, p := range prefixes[k] {
				if p.Contains(c) {
					ok = true
					break
				}
			}
			if !ok {
				return s, &FieldError{"allow." + k, "Toto nastavení by odpojilo váš počítač (adresa " + clientAddr + ")."}
			}
		}
	}
	return out, nil
}

// Render returns the nft script for s (normalised by Validate). It always
// replaces the whole table in one transaction; a disabled firewall is only
// the removal.
func Render(s Settings) string {
	var b strings.Builder
	b.WriteString("table ip " + Table + " {}\ndelete table ip " + Table + "\n")
	if !s.Enabled {
		return b.String()
	}
	b.WriteString("table ip " + Table + " {\n\tchain input {\n\t\ttype filter hook input priority 0; policy drop;\n")
	b.WriteString("\t\tiifname \"lo\" accept\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tct state invalid drop\n")
	b.WriteString("\t\ticmp type echo-request limit rate 10/second accept\n")
	b.WriteString("\t\tudp sport 67 udp dport 68 accept\n")
	for _, svc := range Catalogue {
		src := s.Allow[svc.Key]
		if len(src) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\t\tip saddr %s %s accept\n", setOf(src), portMatch(svc.Ports))
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func setOf(v []string) string {
	if len(v) == 1 {
		return v[0]
	}
	return "{ " + strings.Join(v, ", ") + " }"
}

// portMatch renders the ports of one service; all catalogue ports are TCP.
func portMatch(ports []Port) string {
	nums := make([]string, len(ports))
	for i, p := range ports {
		nums[i] = strconv.Itoa(p.Port)
	}
	return ports[0].Proto + " dport " + setOf(nums)
}

// Load reads firewall.json; a missing file gives Default.
func Load(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, err
	}
	return Validate(s, "")
}

// Save writes firewall.json (0644).
func Save(path string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o644)
}
