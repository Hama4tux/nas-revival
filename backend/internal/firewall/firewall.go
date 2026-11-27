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
	"unicode"
	"unicode/utf8"

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
	// Rules is an ordered rule table evaluated before the services; the
	// first matching rule wins.
	Rules []Rule `json:"rules"`
}

// MaxRules is the most rules.
const MaxRules = 64

// Rule accepts or drops traffic from one source.
type Rule struct {
	Source string `json:"source"` // one IPv4 address or network
	Ports  string `json:"ports"`  // "" = all, "8080" or "6000-6010"
	Proto  string `json:"proto"`  // tcp, udp or both
	Action string `json:"action"` // accept or drop
	Note   string `json:"note"`   // label only, never rendered into the script
}

// matches reports whether r applies to traffic of proto to port
// (the source is checked by the caller).
func (r Rule) matches(proto string, port int) bool {
	if r.Proto != "both" && r.Proto != proto {
		return false
	}
	if r.Ports == "" {
		return true
	}
	lo, hi, ok := portRange(r.Ports)
	return ok && port >= lo && port <= hi
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
	return Settings{Allow: a, Rules: []Rule{}}
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

// normSources parses, sorts and de-duplicates sources and drops those a wider
// one covers; field names the error.
func normSources(field string, in []string) ([]netip.Prefix, []string, error) {
	if len(in) > MaxSources {
		return nil, nil, &FieldError{field, "Nejvýše " + strconv.Itoa(MaxSources) + " adres nebo sítí."}
	}
	seen := map[netip.Prefix]bool{}
	var list []netip.Prefix
	for _, v := range in {
		p, err := parseSource(v)
		if err != nil {
			return nil, nil, &FieldError{field, "Neplatná adresa nebo síť " + strconv.Quote(v) + " (jen IPv4, např. 192.168.1.0/24)."}
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
	strs := make([]string, len(list))
	for i, p := range list {
		strs[i] = formatSource(p)
	}
	return list, strs, nil
}

// portRange parses "8080" or "6000-6010" (digits only, 1..65535, lo<hi).
func portRange(v string) (lo, hi int, ok bool) {
	v = strings.TrimSpace(v)
	a, b, isRange := strings.Cut(v, "-")
	num := func(x string) (int, bool) {
		x = strings.TrimSpace(x)
		if x == "" || strings.Trim(x, "0123456789") != "" || len(x) > 6 {
			return 0, false
		}
		n, _ := strconv.Atoi(x)
		return n, n >= 1 && n <= 65535
	}
	lo, ok = num(a)
	if !ok {
		return 0, 0, false
	}
	if !isRange {
		return lo, lo, true
	}
	hi, ok = num(b)
	return lo, hi, ok && lo < hi
}

func normPorts(v string) (string, bool) {
	lo, hi, ok := portRange(v)
	if !ok {
		return "", false
	}
	if lo == hi {
		return strconv.Itoa(lo), true
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi), true
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
		list, strs, err := normSources(field, in)
		if err != nil {
			return s, err
		}
		prefixes[svc.Key] = list
		out.Allow[svc.Key] = strs
	}
	if len(s.Rules) > MaxRules {
		return s, &FieldError{"rules", "Nejvýše " + strconv.Itoa(MaxRules) + " pravidel."}
	}
	for i, r := range s.Rules {
		f := "rules." + strconv.Itoa(i) + "."
		p, err := parseSource(r.Source)
		if err != nil {
			return s, &FieldError{f + "source", "Neplatná adresa nebo síť " + strconv.Quote(r.Source) + " (jen IPv4, např. 192.168.1.0/24)."}
		}
		ports := ""
		if strings.TrimSpace(r.Ports) != "" {
			var ok bool
			if ports, ok = normPorts(r.Ports); !ok {
				return s, &FieldError{f + "ports", "Neplatný port nebo rozsah (např. 8080 nebo 6000-6010, 1 až 65535, prázdné = všechny)."}
			}
		}
		if r.Proto != "tcp" && r.Proto != "udp" && r.Proto != "both" {
			return s, &FieldError{f + "proto", "Protokol musí být tcp, udp nebo both."}
		}
		if r.Action != "accept" && r.Action != "drop" {
			return s, &FieldError{f + "action", "Akce musí být accept nebo drop."}
		}
		note := strings.TrimSpace(r.Note)
		if utf8.RuneCountInString(note) > 40 {
			return s, &FieldError{f + "note", "Poznámka může mít nejvýše 40 znaků."}
		}
		if strings.ContainsFunc(note, unicode.IsControl) {
			return s, &FieldError{f + "note", "Poznámka nesmí obsahovat řídicí znaky."}
		}
		out.Rules = append(out.Rules, Rule{Source: formatSource(p), Ports: ports, Proto: r.Proto, Action: r.Action, Note: note})
	}
	if out.Enabled && clientAddr != "" {
		c, err := netip.ParseAddr(clientAddr)
		c = c.Unmap() // ::ffff:a.b.c.d from a dual-stack listener
		if err != nil || !c.Is4() {
			return s, &FieldError{"allow.web", "Adresu vašeho počítače (" + clientAddr + ") nelze ověřit, firewall nelze zapnout."}
		}
		for _, k := range []struct {
			key, what string
			port      int
		}{{"web", "webové správy", 8443}, {"ssh", "SSH", 22}} {
			decided := false
			for i, r := range out.Rules {
				rp, _ := parseSource(r.Source)
				if !rp.Contains(c) || !r.matches("tcp", k.port) {
					continue
				}
				if r.Action == "drop" {
					return s, &FieldError{"rules." + strconv.Itoa(i) + ".action", "Toto pravidlo by odpojilo váš počítač (adresa " + clientAddr + ") od " + k.what + "."}
				}
				decided = true
				break
			}
			if decided {
				continue
			}
			ok := false
			for _, p := range prefixes[k.key] {
				if p.Contains(c) {
					ok = true
					break
				}
			}
			if !ok {
				return s, &FieldError{"allow." + k.key, "Toto nastavení by odpojilo váš počítač (adresa " + clientAddr + ")."}
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
	for _, r := range s.Rules {
		var m string
		switch {
		case r.Ports != "" && r.Proto == "both":
			m = "meta l4proto { tcp, udp } th dport " + r.Ports
		case r.Ports != "":
			m = r.Proto + " dport " + r.Ports
		case r.Proto == "both":
			m = "ip protocol { tcp, udp }"
		default:
			m = "ip protocol " + r.Proto
		}
		fmt.Fprintf(&b, "\t\tip saddr %s %s %s\n", r.Source, m, r.Action)
	}
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
