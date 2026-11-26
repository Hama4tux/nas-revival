package helper

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Network diagnostics (NET-11). Only fixed probes run: ping of one host or of
// the default gateway (4 packets), a DNS lookup, an internet check against a
// fixed host, and the NAS's own listening ports and TCP connections. One test
// runs at a time and at most diagPerMinute start per minute, so the NAS cannot
// be used to scan the network.
const (
	DiagPing        = "ping"
	DiagGateway     = "gateway"
	DiagDNS         = "dns"
	DiagInternet    = "internet"
	DiagPorts       = "ports"
	DiagConnections = "connections"

	CodeDiagBusy    = "diag_busy"
	CodeRateLimited = "rate_limited"

	pingCount     = 4
	diagPerMinute = 20
	maxDiagConns  = 200
	// InternetHost is the fixed target of the internet check; the NAS talks
	// to it anyway for Debian updates.
	InternetHost = "deb.debian.org"
	lookupTime   = 5 * time.Second
)

// PingResult summarises one ping run.
type PingResult struct {
	Address     string   `json:"address"`
	Sent        int      `json:"sent"`
	Received    int      `json:"received"`
	LossPercent float64  `json:"lossPercent"`
	MinMs       *float64 `json:"minMs"`
	AvgMs       *float64 `json:"avgMs"`
	MaxMs       *float64 `json:"maxMs"`
}

// DiagStep is one step of the internet check.
type DiagStep struct {
	Name       string `json:"name"` // dns, connect
	OK         bool   `json:"ok"`
	Detail     string `json:"detail"`
	DurationMs int64  `json:"durationMs"`
}

// ListenPort is one listening socket of the NAS.
type ListenPort struct {
	Proto   string `json:"proto"` // tcp, udp
	Address string `json:"address"`
	Port    int    `json:"port"`
	Process string `json:"process"`
}

// Connection is one established TCP connection.
type Connection struct {
	LocalPort int    `json:"localPort"`
	Peer      string `json:"peer"`
	PeerPort  int    `json:"peerPort"`
	Process   string `json:"process"`
}

// DiagResult is the result of net.diag.
type DiagResult struct {
	Test        string       `json:"test"`
	Target      string       `json:"target,omitempty"`
	OK          bool         `json:"ok"`
	Message     string       `json:"message,omitempty"`
	Ping        *PingResult  `json:"ping,omitempty"`
	Addresses   []string     `json:"addresses,omitempty"`
	Steps       []DiagStep   `json:"steps,omitempty"`
	Ports       []ListenPort `json:"ports,omitempty"`
	Connections []Connection `json:"connections,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
	DurationMs  int64        `json:"durationMs"`
}

// DiagManager runs the diagnostics.
type DiagManager struct {
	Iface  string
	Run    Runner
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	Now    func() time.Time

	mu      sync.Mutex // one test at a time
	rateMu  sync.Mutex
	started []time.Time
}

// NewDiagManager returns a DiagManager with the system resolver and dialer.
func NewDiagManager(iface string, run Runner) *DiagManager {
	var d net.Dialer
	return &DiagManager{Iface: iface, Run: run, Now: time.Now, Dial: d.DialContext,
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}}
}

// ValidDiagTarget accepts a host name or a unicast IP address.
func ValidDiagTarget(s string) bool {
	if a, err := netip.ParseAddr(s); err == nil {
		return validHostOrIP(s) && a != netip.AddrFrom4([4]byte{255, 255, 255, 255})
	}
	return validHostOrIP(s)
}

// ValidateDiag checks the test name and target before anything runs.
func ValidateDiag(test, target string) (string, error) {
	target = strings.TrimSpace(target)
	switch test {
	case DiagPing, DiagDNS:
		if target == "" {
			return "", invalid("target", "Zadejte název nebo IP adresu.")
		}
		if !ValidDiagTarget(target) {
			return "", invalid("target", "Zadejte platný název hostitele nebo IP adresu (ne broadcast ani multicast).")
		}
		if test == DiagDNS {
			if _, err := netip.ParseAddr(target); err == nil {
				return "", invalid("target", "Pro překlad zadejte název, ne IP adresu.")
			}
		}
		return target, nil
	case DiagGateway, DiagInternet, DiagPorts, DiagConnections:
		if target != "" {
			return "", invalid("target", "Tento test nemá cíl.")
		}
		return "", nil
	}
	return "", invalid("test", "Neznámý test.")
}

func (m *DiagManager) allow() bool {
	m.rateMu.Lock()
	defer m.rateMu.Unlock()
	now := m.Now()
	keep := m.started[:0]
	for _, t := range m.started {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	m.started = keep
	if len(m.started) >= diagPerMinute {
		return false
	}
	m.started = append(m.started, now)
	return true
}

// Test executes one test.
func (m *DiagManager) Test(ctx context.Context, test, target, user string) (DiagResult, error) {
	target, err := ValidateDiag(test, target)
	if err != nil {
		return DiagResult{}, err
	}
	if !m.mu.TryLock() {
		return DiagResult{}, &Error{Code: CodeDiagBusy, Message: "Jiný test sítě právě běží, zkuste to za chvíli."}
	}
	defer m.mu.Unlock()
	if !m.allow() {
		return DiagResult{}, &Error{Code: CodeRateLimited, Message: "Příliš mnoho testů za minutu, zkuste to za chvíli."}
	}
	start := m.Now()
	slog.Info("network diagnostic", "test", test, "target", target, "by", user)
	var r DiagResult
	switch test {
	case DiagPing:
		r = m.ping(ctx, target)
	case DiagGateway:
		r = m.gateway(ctx)
	case DiagDNS:
		r = m.lookup(ctx, target)
	case DiagInternet:
		r = m.internet(ctx)
	case DiagPorts:
		r, err = m.ports(ctx)
	case DiagConnections:
		r, err = m.connections(ctx)
	}
	if err != nil {
		return DiagResult{}, err
	}
	r.Test, r.Target = test, target
	r.DurationMs = m.Now().Sub(start).Milliseconds()
	return r, nil
}

func (m *DiagManager) ping(ctx context.Context, target string) DiagResult {
	// -n: no reverse lookups; -W 2: wait per reply; -w 10: hard deadline.
	out, err := m.Run(ctx, "ping", "-n", "-c", strconv.Itoa(pingCount), "-W", "2", "-w", "10", target)
	p, ok := ParsePing(string(out))
	if !ok {
		// ping reports an unknown name on stderr only (exit 2, empty stdout).
		msg := "Ping se nepodařilo spustit."
		if _, perr := netip.ParseAddr(target); perr != nil && err != nil && len(out) == 0 {
			msg = "Název se nepodařilo přeložit na adresu."
		}
		return DiagResult{OK: false, Message: msg}
	}
	r := DiagResult{Ping: &p, OK: p.Received > 0}
	if !r.OK {
		r.Message = "Cíl neodpověděl. Může být vypnutý, mimo síť, nebo odpovědi na ping blokuje."
	}
	return r
}

func (m *DiagManager) gateway(ctx context.Context) DiagResult {
	out, err := m.Run(ctx, "ip", "-j", "route", "show", "default")
	var gw *string
	if err == nil {
		gw = parseDefaultRoute(out, m.Iface)
	}
	if gw == nil {
		return DiagResult{OK: false, Message: "NAS nemá výchozí bránu."}
	}
	r := m.ping(ctx, *gw)
	r.Addresses = []string{*gw}
	if !r.OK && r.Ping != nil {
		r.Message = "Brána neodpovídá. Zkontrolujte kabel, switch a router."
	}
	return r
}

func (m *DiagManager) lookup(ctx context.Context, name string) DiagResult {
	lctx, cancel := context.WithTimeout(ctx, lookupTime)
	defer cancel()
	addrs, err := m.Lookup(lctx, name)
	if err != nil || len(addrs) == 0 {
		return DiagResult{OK: false, Message: "Název se nepodařilo přeložit. Zkontrolujte DNS servery v nastavení sítě."}
	}
	list := make([]string, 0, len(addrs))
	for _, a := range addrs {
		list = append(list, a.Unmap().String())
	}
	sort.Strings(list)
	return DiagResult{OK: true, Addresses: list}
}

func (m *DiagManager) internet(ctx context.Context) DiagResult {
	r := DiagResult{}
	t0 := m.Now()
	lctx, cancel := context.WithTimeout(ctx, lookupTime)
	addrs, err := m.Lookup(lctx, InternetHost)
	cancel()
	dns := DiagStep{Name: "dns", DurationMs: m.Now().Sub(t0).Milliseconds()}
	if err != nil || len(addrs) == 0 {
		dns.Detail = InternetHost
		r.Steps = []DiagStep{dns}
		r.Message = "DNS nefunguje – NAS nepřeloží název " + InternetHost + "."
		return r
	}
	// Prefer IPv4: many home networks have no working IPv6 route.
	sort.SliceStable(addrs, func(i, j int) bool { return addrs[i].Unmap().Is4() && !addrs[j].Unmap().Is4() })
	a := addrs[0].Unmap()
	dns.OK, dns.Detail = true, InternetHost+" → "+a.String()
	t1 := m.Now()
	cctx, cancel := context.WithTimeout(ctx, lookupTime)
	conn, err := m.Dial(cctx, "tcp", netip.AddrPortFrom(a, 443).String())
	cancel()
	conn443 := DiagStep{Name: "connect", Detail: netip.AddrPortFrom(a, 443).String(), DurationMs: m.Now().Sub(t1).Milliseconds()}
	if err == nil {
		conn.Close()
		conn443.OK = true
	}
	r.Steps = []DiagStep{dns, conn443}
	r.OK = conn443.OK
	if !r.OK {
		r.Message = "DNS funguje, ale spojení do internetu (HTTPS) se nenavázalo. Zkontrolujte bránu a router."
	}
	return r
}

var (
	pingAddrRe  = regexp.MustCompile(`^PING \S+ \(([0-9A-Fa-f:.]+)\)`)
	pingStatsRe = regexp.MustCompile(`(\d+) packets transmitted, (\d+) received`)
	pingRttRe   = regexp.MustCompile(`= ([0-9.]+)/([0-9.]+)/([0-9.]+)/`)
)

// ParsePing reads the summary of iputils ping.
func ParsePing(out string) (PingResult, bool) {
	var p PingResult
	if m := pingAddrRe.FindStringSubmatch(out); m != nil {
		p.Address = m[1]
	}
	m := pingStatsRe.FindStringSubmatch(out)
	if m == nil {
		return p, false
	}
	p.Sent, _ = strconv.Atoi(m[1])
	p.Received, _ = strconv.Atoi(m[2])
	if p.Sent > 0 {
		p.LossPercent = float64(p.Sent-p.Received) * 100 / float64(p.Sent)
	}
	if r := pingRttRe.FindStringSubmatch(out); r != nil {
		f := func(s string) *float64 { v, _ := strconv.ParseFloat(s, 64); return &v }
		p.MinMs, p.AvgMs, p.MaxMs = f(r[1]), f(r[2]), f(r[3])
	}
	return p, true
}

func (m *DiagManager) ports(ctx context.Context) (DiagResult, error) {
	out, err := m.Run(ctx, "ss", "-H", "-tulnp")
	if err != nil {
		return DiagResult{}, internalErr("Seznam portů se nepodařilo zjistit.")
	}
	return DiagResult{OK: true, Ports: ParseListen(string(out))}, nil
}

func (m *DiagManager) connections(ctx context.Context) (DiagResult, error) {
	out, err := m.Run(ctx, "ss", "-H", "-tnp", "state", "established")
	if err != nil {
		return DiagResult{}, internalErr("Seznam spojení se nepodařilo zjistit.")
	}
	c := ParseEstablished(string(out))
	r := DiagResult{OK: true, Connections: c}
	if len(c) > maxDiagConns {
		r.Connections, r.Truncated = c[:maxDiagConns], true
	}
	return r, nil
}

// splitHostPort handles "0.0.0.0:445", "[::]:22", "*:111" and "[fe80::1]%eth0:5353".
func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	host := s[:i]
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if a, err := netip.ParseAddr(host); err == nil {
		host = a.Unmap().String()
	}
	return host, port, true
}

var ssProcRe = regexp.MustCompile(`\("([^"]+)",pid=`)

// processName returns the first process of users:(("sshd",pid=1,fd=3),…).
// Samba names its children "smbd[client]"; the client part is cut off.
func processName(field string) string {
	m := ssProcRe.FindStringSubmatch(field)
	if m == nil {
		return ""
	}
	name, _, _ := strings.Cut(m[1], "[")
	return name
}

// ParseListen reads `ss -H -tulnp`: Netid State Recv-Q Send-Q Local Peer [Process].
func ParseListen(out string) []ListenPort {
	res := []ListenPort{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || (f[0] != "tcp" && f[0] != "udp") {
			continue
		}
		host, port, ok := splitHostPort(f[4])
		if !ok {
			continue
		}
		p := ListenPort{Proto: f[0], Address: host, Port: port}
		if len(f) > 6 {
			p.Process = processName(strings.Join(f[6:], " "))
		}
		k := p.Proto + " " + p.Address + " " + strconv.Itoa(p.Port)
		if seen[k] {
			continue
		}
		seen[k] = true
		res = append(res, p)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Port != res[j].Port {
			return res[i].Port < res[j].Port
		}
		if res[i].Proto != res[j].Proto {
			return res[i].Proto < res[j].Proto
		}
		return res[i].Address < res[j].Address
	})
	return res
}

// ParseEstablished reads `ss -H -tnp state established`: Recv-Q Send-Q Local Peer [Process].
func ParseEstablished(out string) []Connection {
	res := []Connection{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		_, lport, ok1 := splitHostPort(f[2])
		peer, pport, ok2 := splitHostPort(f[3])
		if !ok1 || !ok2 {
			continue
		}
		c := Connection{LocalPort: lport, Peer: peer, PeerPort: pport}
		if len(f) > 4 {
			c.Process = processName(strings.Join(f[4:], " "))
		}
		res = append(res, c)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].LocalPort != res[j].LocalPort {
			return res[i].LocalPort < res[j].LocalPort
		}
		return res[i].Peer < res[j].Peer
	})
	return res
}
