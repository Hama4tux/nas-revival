package helper

import (
	"context"
	"encoding/json"
	"net/netip"
	"os/exec"
	"strings"
)

// Runner runs a fixed command (never through a shell) and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the production Runner.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	return cmd.Output()
}

// Current is what the system uses right now.
type Current struct {
	Addresses []string `json:"addresses"`
	Gateway   *string  `json:"gateway"`
	DNS       []string `json:"dns"`
}

// ReadCurrent collects the live state of iface; missing data stays empty.
func ReadCurrent(ctx context.Context, run Runner, iface string) Current {
	c := Current{Addresses: []string{}, DNS: []string{}}
	if out, err := run(ctx, "ip", "-j", "addr", "show", "dev", iface); err == nil {
		c.Addresses = parseIPAddr(out)
	}
	if out, err := run(ctx, "ip", "-j", "route", "show", "default"); err == nil {
		c.Gateway = parseDefaultRoute(out, iface)
	}
	if out, err := run(ctx, "resolvectl", "dns", iface); err == nil {
		c.DNS = parseResolvectlDNS(string(out))
	}
	return c
}

func parseIPAddr(out []byte) []string {
	var links []struct {
		AddrInfo []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Scope     string `json:"scope"`
		} `json:"addr_info"`
	}
	res := []string{}
	if json.Unmarshal(out, &links) != nil {
		return res
	}
	for _, l := range links {
		for _, a := range l.AddrInfo {
			ip, err := netip.ParseAddr(a.Local)
			if err != nil {
				continue
			}
			if (a.Family == "inet" && !ip.IsLinkLocalUnicast()) || (a.Family == "inet6" && a.Scope == "global") {
				res = append(res, netip.PrefixFrom(ip, a.PrefixLen).String())
			}
		}
	}
	return res
}

func parseDefaultRoute(out []byte, iface string) *string {
	var routes []struct {
		Dev     string `json:"dev"`
		Gateway string `json:"gateway"`
	}
	if json.Unmarshal(out, &routes) != nil {
		return nil
	}
	for _, r := range routes {
		if a, err := netip.ParseAddr(r.Gateway); err == nil && r.Dev == iface {
			return ptr(a.String())
		}
	}
	return nil
}

// parseResolvectlDNS handles "Link 2 (enp1s0): 192.0.2.1 192.0.2.2".
func parseResolvectlDNS(s string) []string {
	res := []string{}
	_, list, ok := strings.Cut(s, ":")
	if !ok {
		return res
	}
	for _, f := range strings.Fields(list) {
		if a, err := netip.ParseAddr(f); err == nil {
			res = append(res, a.String())
		}
	}
	return res
}
