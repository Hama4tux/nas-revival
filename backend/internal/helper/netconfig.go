package helper

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

const (
	ModeDHCP   = "dhcp"
	ModeStatic = "static"
)

// NetConfig is the part of 20-wired.network the UI may change.
type NetConfig struct {
	Mode         string   `json:"mode"`
	Address      *string  `json:"address"`
	PrefixLength *int     `json:"prefixLength"`
	Gateway      *string  `json:"gateway"`
	DNS          []string `json:"dns"`
	UseDhcpDns   bool     `json:"useDhcpDns"`
}

// DefaultNetConfig is DHCP with DHCP-provided DNS.
func DefaultNetConfig() NetConfig {
	return NetConfig{Mode: ModeDHCP, DNS: []string{}, UseDhcpDns: true}
}

var ifaceRe = regexp.MustCompile(`^[a-z][a-z0-9]{1,14}$`)

// ValidIfaceName reports whether name is an acceptable interface name.
func ValidIfaceName(name string) bool { return ifaceRe.MatchString(name) }

func ptr[T any](v T) *T { return &v }

// ipv4Problem returns a Czech reason when a is not a usable unicast IPv4.
func ipv4Problem(a netip.Addr) string {
	switch {
	case !a.Is4():
		return "musí být adresa IPv4"
	case a.IsUnspecified():
		return "nesmí být 0.0.0.0"
	case a.IsLoopback():
		return "nesmí být adresa loopback"
	case a.IsMulticast():
		return "nesmí být multicastová adresa"
	case a.IsLinkLocalUnicast():
		return "nesmí být link-local adresa (169.254.x.x)"
	}
	return ""
}

func broadcastOf(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := uint32(0xffffffff) >> uint(p.Bits())
	for i := 0; i < 4; i++ {
		b[i] |= byte(host >> (24 - 8*uint(i)))
	}
	return netip.AddrFrom4(b)
}

// ValidateNetConfig checks c and returns it normalized (non-nil DNS, fields
// unused by the mode cleared). The error is always an *Error.
func ValidateNetConfig(c NetConfig) (NetConfig, error) {
	out := NetConfig{Mode: c.Mode, DNS: []string{}, UseDhcpDns: c.UseDhcpDns}
	switch c.Mode {
	case ModeDHCP, ModeStatic:
	default:
		return out, invalid("mode", "Režim musí být „dhcp“ nebo „static“.")
	}
	if len(c.DNS) > 3 {
		return out, invalid("dns", "Lze zadat nejvýše 3 servery DNS.")
	}
	for _, d := range c.DNS {
		a, err := netip.ParseAddr(d)
		if err != nil || a.Zone() != "" || a.Is4In6() || a.IsUnspecified() || a.IsMulticast() || a.IsLoopback() {
			return out, invalid("dns", "Neplatná adresa serveru DNS: "+quoteShort(d))
		}
		out.DNS = append(out.DNS, a.String())
	}
	if c.Mode == ModeDHCP {
		if !c.UseDhcpDns && len(out.DNS) == 0 {
			return out, invalid("dns", "Bez DNS z DHCP zadejte alespoň jeden server DNS.")
		}
		return out, nil
	}
	out.UseDhcpDns = true
	if c.Address == nil {
		return out, invalid("address", "Zadejte adresu IPv4.")
	}
	addr, err := netip.ParseAddr(*c.Address)
	if err != nil {
		return out, invalid("address", "Neplatná adresa IPv4.")
	}
	if p := ipv4Problem(addr); p != "" {
		return out, invalid("address", "Adresa "+p+".")
	}
	if c.PrefixLength == nil || *c.PrefixLength < 8 || *c.PrefixLength > 30 {
		return out, invalid("prefixLength", "Délka prefixu musí být 8 až 30.")
	}
	pfx := netip.PrefixFrom(addr, *c.PrefixLength)
	if addr == pfx.Masked().Addr() {
		return out, invalid("address", "Adresa nesmí být adresa sítě.")
	}
	if addr == broadcastOf(pfx) {
		return out, invalid("address", "Adresa nesmí být broadcast sítě.")
	}
	if c.Gateway == nil {
		return out, invalid("gateway", "Zadejte bránu.")
	}
	gw, err := netip.ParseAddr(*c.Gateway)
	if err != nil {
		return out, invalid("gateway", "Neplatná adresa brány.")
	}
	if p := ipv4Problem(gw); p != "" {
		return out, invalid("gateway", "Brána "+p+".")
	}
	if !pfx.Contains(gw) || gw == pfx.Masked().Addr() || gw == broadcastOf(pfx) {
		return out, invalid("gateway", "Brána musí ležet ve stejné podsíti.")
	}
	if gw == addr {
		return out, invalid("gateway", "Brána se nesmí rovnat adrese zařízení.")
	}
	if len(out.DNS) == 0 {
		return out, invalid("dns", "Se statickou adresou zadejte alespoň jeden server DNS.")
	}
	out.Address, out.PrefixLength, out.Gateway = ptr(addr.String()), ptr(*c.PrefixLength), ptr(gw.String())
	return out, nil
}

func quoteShort(s string) string {
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return strconv.QuoteToASCII(s)
}

// RenderNetwork renders a validated config as 20-wired.network.
func RenderNetwork(c NetConfig) string {
	var b strings.Builder
	b.WriteString("[Match]\nName=e*\n\n[Network]\n")
	if c.Mode == ModeStatic && c.Address != nil && c.PrefixLength != nil && c.Gateway != nil {
		fmt.Fprintf(&b, "Address=%s/%d\nGateway=%s\n", *c.Address, *c.PrefixLength, *c.Gateway)
	} else {
		b.WriteString("DHCP=yes\n")
	}
	for _, d := range c.DNS {
		b.WriteString("DNS=" + d + "\n")
	}
	if c.Mode != ModeStatic && !c.UseDhcpDns {
		b.WriteString("\n[DHCPv4]\nUseDNS=no\n")
	}
	return b.String()
}

// ParseNetwork reads an existing network file back. It is tolerant: unknown
// lines are ignored and whatever can be understood is reported.
func ParseNetwork(data string) NetConfig {
	c := DefaultNetConfig()
	section := ""
	dhcp := false
	var addr, gw string
	var pfx int
	hasAddr := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			section = strings.ToLower(strings.Trim(line, "[] "))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case section == "network" && k == "DHCP":
			switch strings.ToLower(v) {
			case "yes", "true", "on", "1", "ipv4", "both":
				dhcp = true
			}
		case section == "network" && k == "Address" && !hasAddr:
			if p, err := netip.ParsePrefix(v); err == nil && p.Addr().Is4() {
				hasAddr, addr, pfx = true, p.Addr().String(), p.Bits()
			}
		case section == "network" && k == "Gateway" && gw == "":
			if a, err := netip.ParseAddr(v); err == nil && a.Is4() {
				gw = a.String()
			}
		case section == "network" && k == "DNS":
			for _, f := range strings.Fields(v) {
				if a, err := netip.ParseAddr(f); err == nil && a.Zone() == "" {
					c.DNS = append(c.DNS, a.String())
				}
			}
		case section == "dhcpv4" && k == "UseDNS":
			switch strings.ToLower(v) {
			case "no", "false", "off", "0":
				c.UseDhcpDns = false
			}
		}
	}
	if hasAddr && !dhcp {
		c.Mode = ModeStatic
		c.Address, c.PrefixLength = ptr(addr), ptr(pfx)
		if gw != "" {
			c.Gateway = ptr(gw)
		}
		c.UseDhcpDns = true
	}
	return c
}
