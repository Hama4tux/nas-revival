package firewall

import (
	"net/netip"
	"sort"
)

// Socket is one listening socket as reported by ss.
type Socket struct {
	Proto   string // tcp, udp
	Address string
	Port    int
	Process string
}

// Listen is a listening port with the verdict of a rule set.
type Listen struct {
	Proto   string `json:"proto"`
	Port    int    `json:"port"`
	Process string `json:"process"`
	Allowed bool   `json:"allowed"`
}

// Allowed reports whether s accepts proto/port from at least one source:
// a matching accept rule (a drop rule for everyone ends the walk), a
// catalogue service with a non-empty allow list, or the DHCP client (UDP 68). It does not look at s.Enabled.
func Allowed(s Settings, proto string, port int) bool {
	for _, r := range s.Rules {
		if !r.matches(proto, port) {
			continue
		}
		if r.Action == "accept" {
			return true
		}
		if rp, err := parseSource(r.Source); err == nil && rp.Bits() == 0 {
			return false
		}
	}
	if proto == "udp" && port == 68 {
		return true
	}
	for _, svc := range Catalogue {
		if len(s.Allow[svc.Key]) == 0 {
			continue
		}
		for _, p := range svc.Ports {
			if p.Proto == proto && p.Port == port {
				return true
			}
		}
	}
	return false
}

// Listening turns sockets into the sorted, de-duplicated list of ports
// reachable from the network (loopback-only sockets are skipped) with the
// verdict of s.
func Listening(s Settings, socks []Socket) []Listen {
	out := []Listen{}
	seen := map[[2]any]int{}
	for _, k := range socks {
		if a, err := netip.ParseAddr(k.Address); err == nil && a.IsLoopback() {
			continue
		}
		if k.Address == "lo" {
			continue
		}
		key := [2]any{k.Proto, k.Port}
		if i, ok := seen[key]; ok {
			if out[i].Process == "" {
				out[i].Process = k.Process
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, Listen{Proto: k.Proto, Port: k.Port, Process: k.Process, Allowed: Allowed(s, k.Proto, k.Port)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Port < out[j].Port
	})
	return out
}
