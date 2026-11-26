package helper

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

const pingOK = `PING 192.0.2.1 (192.0.2.1) 56(84) bytes of data.
64 bytes from 192.0.2.1: icmp_seq=1 ttl=64 time=0.388 ms

--- 192.0.2.1 ping statistics ---
4 packets transmitted, 3 received, 25% packet loss, time 3004ms
rtt min/avg/max/mdev = 0.255/0.321/0.388/0.066 ms
`

const pingLost = `PING 192.0.2.9 (192.0.2.9) 56(84) bytes of data.

--- 192.0.2.9 ping statistics ---
4 packets transmitted, 0 received, 100% packet loss, time 3029ms

`

const ssListen = `udp UNCONN 0      0          127.0.0.1:111   0.0.0.0:* users:(("rpcbind",pid=4777,fd=7))
tcp LISTEN 0      50           0.0.0.0:445   0.0.0.0:* users:(("smbd",pid=5059,fd=28))
tcp LISTEN 0      4096         0.0.0.0:111   0.0.0.0:* users:(("rpcbind",pid=4777,fd=4),("systemd",pid=1,fd=76))
tcp LISTEN 0      4096         0.0.0.0:2049  0.0.0.0:*
tcp LISTEN 0      128             [::]:22       [::]:* users:(("sshd",pid=268,fd=7))
tcp LISTEN 0      128          0.0.0.0:22    0.0.0.0:* users:(("sshd",pid=268,fd=6))
tcp LISTEN 0      128          0.0.0.0:22    0.0.0.0:* users:(("sshd",pid=268,fd=6))
`

const ssEstab = `0      0      192.0.2.60:2049  192.0.2.12:805
0      0      192.0.2.60:445  192.0.2.155:48224 users:(("smbd[192.0.2.15",pid=5633,fd=31))
0      0      192.0.2.60:22   192.0.2.155:55120 users:(("sshd-session",pid=14708,fd=7),("sshd-session",pid=14695,fd=7))
0      0      [::ffff:192.0.2.60]:8443 [::ffff:192.0.2.155]:60300 users:(("nas-revival",pid=13996,fd=6))
`

func TestParsePing(t *testing.T) {
	p, ok := ParsePing(pingOK)
	if !ok || p.Address != "192.0.2.1" || p.Sent != 4 || p.Received != 3 || p.LossPercent != 25 ||
		p.MinMs == nil || *p.MinMs != 0.255 || *p.AvgMs != 0.321 || *p.MaxMs != 0.388 {
		t.Fatalf("%+v %v", p, ok)
	}
	p, ok = ParsePing(pingLost)
	if !ok || p.Received != 0 || p.LossPercent != 100 || p.AvgMs != nil {
		t.Fatalf("lost: %+v %v", p, ok)
	}
	if _, ok := ParsePing(""); ok {
		t.Fatal("empty output parsed")
	}
}

func TestParseListen(t *testing.T) {
	got := ParseListen(ssListen)
	var b strings.Builder
	for _, p := range got {
		b.WriteString(p.Proto + " " + p.Address + ":" + strconv.Itoa(p.Port) + " " + p.Process + "\n")
	}
	want := "tcp 0.0.0.0:22 sshd\ntcp :::22 sshd\ntcp 0.0.0.0:111 rpcbind\nudp 127.0.0.1:111 rpcbind\ntcp 0.0.0.0:445 smbd\ntcp 0.0.0.0:2049 \n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

func TestParseEstablished(t *testing.T) {
	got := ParseEstablished(ssEstab)
	if len(got) != 4 {
		t.Fatalf("%+v", got)
	}
	if c := got[0]; c.LocalPort != 22 || c.Peer != "192.0.2.155" || c.PeerPort != 55120 || c.Process != "sshd-session" {
		t.Errorf("ssh: %+v", c)
	}
	if c := got[1]; c.LocalPort != 445 || c.Process != "smbd" {
		t.Errorf("smbd name not cut: %+v", c)
	}
	if c := got[2]; c.LocalPort != 2049 || c.Process != "" {
		t.Errorf("kernel nfsd: %+v", c)
	}
	if c := got[3]; c.LocalPort != 8443 || c.Peer != "192.0.2.155" {
		t.Errorf("mapped v4: %+v", c)
	}
}

func TestValidateDiag(t *testing.T) {
	ok := [][2]string{{"ping", "192.0.2.1"}, {"ping", " nas.example.org "}, {"ping", "2001:db8::1"}, {"dns", "example.org"},
		{"gateway", ""}, {"internet", ""}, {"ports", ""}, {"connections", ""}}
	for _, c := range ok {
		if _, err := ValidateDiag(c[0], c[1]); err != nil {
			t.Errorf("%v rejected: %v", c, err)
		}
	}
	bad := [][2]string{{"ping", ""}, {"ping", "255.255.255.255"}, {"ping", "224.0.0.1"}, {"ping", "0.0.0.0"}, {"ping", "-f"},
		{"ping", "a b"}, {"ping", "fe80::1%eth0"}, {"dns", "192.0.2.1"}, {"gateway", "192.0.2.1"}, {"scan", ""}, {"ports", "x"}}
	for _, c := range bad {
		if _, err := ValidateDiag(c[0], c[1]); codeOf(err) != CodeInvalid {
			t.Errorf("%v accepted: %v", c, err)
		}
	}
}

type diagFake struct {
	calls []string
	out   map[string]string
}

func (f *diagFake) run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	if o, ok := f.out[cmd]; ok {
		return []byte(o), nil
	}
	return nil, errors.New("exit status 2")
}

func newDiag(f *diagFake) *DiagManager {
	return &DiagManager{Iface: "enp1s0", Run: f.run, Now: time.Now,
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			if host == "deb.debian.org" || host == "example.org" {
				return []netip.Addr{netip.MustParseAddr("2001:db8::80"), netip.MustParseAddr("192.0.2.80")}, nil
			}
			return nil, errors.New("no such host")
		},
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			if addr == "192.0.2.80:443" {
				c, s := net.Pipe()
				s.Close()
				return c, nil
			}
			return nil, errors.New("timeout")
		}}
}

func TestDiagTests(t *testing.T) {
	f := &diagFake{out: map[string]string{
		"ping -n -c 4 -W 2 -w 10 192.0.2.1": pingOK,
		"ip -j route show default":          `[{"dst":"default","gateway":"192.0.2.1","dev":"enp1s0"}]`,
		"ss -H -tulnp":                      ssListen,
	}}
	m := newDiag(f)
	ctx := context.Background()

	r, err := m.Test(ctx, "gateway", "", "admin")
	if err != nil || !r.OK || r.Ping == nil || r.Ping.Received != 3 || r.Addresses[0] != "192.0.2.1" {
		t.Fatalf("gateway: %+v %v", r, err)
	}
	r, _ = m.Test(ctx, "ping", "unknown.invalid", "admin")
	if r.OK || !strings.Contains(r.Message, "přeložit") {
		t.Errorf("unknown name: %+v", r)
	}
	r, _ = m.Test(ctx, "dns", "example.org", "admin")
	if !r.OK || strings.Join(r.Addresses, ",") != "192.0.2.80,2001:db8::80" {
		t.Errorf("dns: %+v", r)
	}
	r, _ = m.Test(ctx, "internet", "", "admin")
	if !r.OK || len(r.Steps) != 2 || r.Steps[1].Detail != "192.0.2.80:443" {
		t.Errorf("internet prefers IPv4: %+v", r)
	}
	r, err = m.Test(ctx, "ports", "", "admin")
	if err != nil || len(r.Ports) != 6 {
		t.Errorf("ports: %+v %v", r, err)
	}
	if _, err := m.Test(ctx, "connections", "", "admin"); codeOf(err) != CodeInternal {
		t.Errorf("ss failure: %v", err)
	}
	// Invalid input never reaches a command.
	f.calls = nil
	if _, err := m.Test(ctx, "ping", "-c 1000", "admin"); codeOf(err) != CodeInvalid || len(f.calls) != 0 {
		t.Errorf("invalid ping ran %q: %v", f.calls, err)
	}
}

func TestDiagNoGatewayAndNoInternet(t *testing.T) {
	m := newDiag(&diagFake{out: map[string]string{"ip -j route show default": `[]`}})
	r, err := m.Test(context.Background(), "gateway", "", "admin")
	if err != nil || r.OK || r.Message == "" {
		t.Errorf("no gateway: %+v %v", r, err)
	}
	m.Dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("timeout") }
	r, _ = m.Test(context.Background(), "internet", "", "admin")
	if r.OK || len(r.Steps) != 2 || !r.Steps[0].OK || r.Steps[1].OK {
		t.Errorf("no route out: %+v", r)
	}
}

func TestDiagRateLimitAndBusy(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	m := newDiag(&diagFake{out: map[string]string{"ss -H -tulnp": ssListen}})
	m.Now = func() time.Time { return now }
	for i := 0; i < diagPerMinute; i++ {
		if _, err := m.Test(context.Background(), "ports", "", "admin"); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if _, err := m.Test(context.Background(), "ports", "", "admin"); codeOf(err) != CodeRateLimited {
		t.Fatalf("over the limit: %v", err)
	}
	now = now.Add(61 * time.Second)
	if _, err := m.Test(context.Background(), "ports", "", "admin"); err != nil {
		t.Fatalf("after a minute: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.Test(context.Background(), "ports", "", "admin"); codeOf(err) != CodeDiagBusy {
		t.Fatalf("parallel test: %v", err)
	}
}
