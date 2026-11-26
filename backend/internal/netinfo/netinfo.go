// Package netinfo reads the network overview (NET-10): host name, search
// domains, interfaces with link details and the routing table. Everything is
// readable without privileges (sysfs, rtnetlink through ip -j, resolved over
// D-Bus), so the web process reads it directly instead of asking the helper.
package netinfo

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Runner runs a fixed command without a shell.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Interface is one network interface (loopback excluded).
type Interface struct {
	Name  string `json:"name"`
	MAC   string `json:"mac"`
	MTU   int    `json:"mtu"`
	State string `json:"state"` // operstate: up, down, dormant, unknown…
	// Carrier: cable plugged in and link negotiated; nil when the interface is down.
	Carrier *bool `json:"carrier"`
	// SpeedMbps and Duplex come from the driver; nil / "" without a link.
	SpeedMbps *int   `json:"speedMbps"`
	Duplex    string `json:"duplex"`
	// LinkChanges counts carrier changes since boot (a flapping cable or port).
	LinkChanges *int     `json:"linkChanges"`
	Addresses   []string `json:"addresses"`
}

// Route is one route of the main table.
type Route struct {
	Family      string `json:"family"` // inet or inet6
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Device      string `json:"device,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Source      string `json:"source,omitempty"`
	Metric      *int   `json:"metric"`
}

// Info is the response of GET /network.
type Info struct {
	Hostname   string      `json:"hostname"`
	Domains    []string    `json:"domains"`
	Interfaces []Interface `json:"interfaces"`
	Routes     []Route     `json:"routes"`
	CheckedAt  time.Time   `json:"checkedAt"`
}

// Reader reads the overview. Root is the file system root ("/" in
// production); tests point it at a fixture tree.
type Reader struct {
	Root string
	Run  Runner
	Now  func() time.Time
}

// Read collects the overview. Missing optional parts (no resolved, no IPv6)
// leave empty lists; only a failing `ip` is an error.
func (r Reader) Read(ctx context.Context) (Info, error) {
	info := Info{Domains: []string{}, Interfaces: []Interface{}, Routes: []Route{}, CheckedAt: r.Now()}
	host, err := os.ReadFile(filepath.Join(r.Root, "proc/sys/kernel/hostname"))
	if err != nil {
		return info, err
	}
	info.Hostname = strings.TrimSpace(string(host))
	out, err := r.Run(ctx, "ip", "-j", "addr", "show")
	if err != nil {
		return info, err
	}
	links, err := parseLinks(out)
	if err != nil {
		return info, err
	}
	for _, l := range links {
		info.Interfaces = append(info.Interfaces, r.withSysfs(l))
	}
	for _, fam := range []string{"-4", "-6"} {
		out, err := r.Run(ctx, "ip", "-j", fam, "route", "show", "table", "main")
		if err != nil {
			continue // IPv6 disabled in the kernel
		}
		routes, err := parseRoutes(out, fam)
		if err != nil {
			return info, err
		}
		info.Routes = append(info.Routes, routes...)
	}
	if out, err := r.Run(ctx, "resolvectl", "domain"); err == nil {
		info.Domains = parseDomains(string(out))
	}
	return info, nil
}

type ipLink struct {
	Name      string `json:"ifname"`
	MTU       int    `json:"mtu"`
	OperState string `json:"operstate"`
	LinkType  string `json:"link_type"`
	Address   string `json:"address"`
	AddrInfo  []struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		PrefixLen int    `json:"prefixlen"`
	} `json:"addr_info"`
}

func parseLinks(out []byte) ([]Interface, error) {
	var links []ipLink
	if err := json.Unmarshal(out, &links); err != nil {
		return nil, errors.New("ip addr: unexpected output")
	}
	res := []Interface{}
	for _, l := range links {
		if l.LinkType == "loopback" || l.Name == "" {
			continue
		}
		it := Interface{Name: l.Name, MAC: l.Address, MTU: l.MTU, State: strings.ToLower(l.OperState), Addresses: []string{}}
		for _, a := range l.AddrInfo {
			ip, err := netip.ParseAddr(a.Local)
			if err != nil || (a.Family != "inet" && a.Family != "inet6") {
				continue
			}
			it.Addresses = append(it.Addresses, netip.PrefixFrom(ip, a.PrefixLen).String())
		}
		res = append(res, it)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

// withSysfs adds the link details the driver exposes. Reading speed or carrier
// of a down interface fails with EINVAL; the fields then stay nil.
func (r Reader) withSysfs(it Interface) Interface {
	dir := filepath.Join(r.Root, "sys/class/net", filepath.Base(it.Name))
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(b)), true
	}
	if v, ok := read("carrier"); ok {
		c := v == "1"
		it.Carrier = &c
	}
	if v, ok := read("speed"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			it.SpeedMbps = &n
		}
	}
	if v, ok := read("duplex"); ok && (v == "full" || v == "half") {
		it.Duplex = v
	}
	if v, ok := read("carrier_changes"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			it.LinkChanges = &n
		}
	}
	return it
}

func parseRoutes(out []byte, fam string) ([]Route, error) {
	var raw []struct {
		Dst      string `json:"dst"`
		Gateway  string `json:"gateway"`
		Dev      string `json:"dev"`
		Protocol string `json:"protocol"`
		Scope    string `json:"scope"`
		PrefSrc  string `json:"prefsrc"`
		Metric   *int   `json:"metric"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, errors.New("ip route: unexpected output")
	}
	family := "inet"
	if fam == "-6" {
		family = "inet6"
	}
	res := []Route{}
	for _, x := range raw {
		// Multicast and other special route types are kernel housekeeping.
		if x.Type != "" && x.Type != "unicast" {
			continue
		}
		res = append(res, Route{Family: family, Destination: x.Dst, Gateway: x.Gateway, Device: x.Dev,
			Protocol: x.Protocol, Scope: x.Scope, Source: x.PrefSrc, Metric: x.Metric})
	}
	return res, nil
}

// parseDomains reads `resolvectl domain`: "Global: a b" and "Link 2 (eth0): c".
// "~." routing-only entries are not search domains.
func parseDomains(out string) []string {
	res := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		_, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for _, d := range strings.Fields(rest) {
			if strings.HasPrefix(d, "~") || seen[d] {
				continue
			}
			seen[d] = true
			res = append(res, d)
		}
	}
	return res
}
