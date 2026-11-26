package netinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const addrJSON = `[{"ifindex":1,"ifname":"lo","mtu":65536,"operstate":"UNKNOWN","link_type":"loopback","address":"00:00:00:00:00:00",
"addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8}]},
{"ifindex":2,"ifname":"enp1s0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"mtu":1500,"operstate":"UP","link_type":"ether",
"address":"02:00:00:00:00:01","addr_info":[{"family":"inet","local":"192.0.2.60","prefixlen":24,"scope":"global"},
{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}]},
{"ifindex":3,"ifname":"eth1","mtu":1500,"operstate":"DOWN","link_type":"ether","address":"02:00:00:00:00:02","addr_info":[]}]`

const route4 = `[{"dst":"default","gateway":"192.0.2.1","dev":"enp1s0","protocol":"static","flags":[]},
{"dst":"192.0.2.0/24","dev":"enp1s0","protocol":"kernel","scope":"link","prefsrc":"192.0.2.60","flags":[]}]`

const route6 = `[{"dst":"fe80::/64","dev":"enp1s0","protocol":"kernel","metric":256,"flags":[],"pref":"medium"},
{"type":"multicast","dst":"ff00::/8","dev":"enp1s0","metric":256,"flags":[]}]`

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, v string) {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/sys/kernel/hostname", "nas\n")
	write("sys/class/net/enp1s0/carrier", "1\n")
	write("sys/class/net/enp1s0/speed", "1000\n")
	write("sys/class/net/enp1s0/duplex", "full\n")
	write("sys/class/net/enp1s0/carrier_changes", "3\n")
	// A down interface: the kernel reports speed -1 and unknown duplex.
	write("sys/class/net/eth1/speed", "-1\n")
	write("sys/class/net/eth1/duplex", "unknown\n")
	return root
}

func runner(ipv6 bool) Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		switch cmd {
		case "ip -j addr show":
			return []byte(addrJSON), nil
		case "ip -j -4 route show table main":
			return []byte(route4), nil
		case "ip -j -6 route show table main":
			if !ipv6 {
				return nil, errors.New("exit status 2")
			}
			return []byte(route6), nil
		case "resolvectl domain":
			return []byte("Global: home ~.\nLink 2 (enp1s0): home lan\n"), nil
		}
		return nil, errors.New("unexpected " + cmd)
	}
}

func TestRead(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	info, err := Reader{Root: fixture(t), Run: runner(true), Now: func() time.Time { return now }}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "nas" || !info.CheckedAt.Equal(now) {
		t.Errorf("hostname %q, checkedAt %v", info.Hostname, info.CheckedAt)
	}
	if strings.Join(info.Domains, ",") != "home,lan" {
		t.Errorf("domains = %q", info.Domains)
	}
	if len(info.Interfaces) != 2 || info.Interfaces[0].Name != "enp1s0" || info.Interfaces[1].Name != "eth1" {
		t.Fatalf("interfaces = %+v", info.Interfaces)
	}
	e := info.Interfaces[0]
	if e.MAC != "02:00:00:00:00:01" || e.MTU != 1500 || e.State != "up" || e.Carrier == nil || !*e.Carrier ||
		e.SpeedMbps == nil || *e.SpeedMbps != 1000 || e.Duplex != "full" || e.LinkChanges == nil || *e.LinkChanges != 3 {
		t.Errorf("enp1s0 = %+v", e)
	}
	if strings.Join(e.Addresses, ",") != "192.0.2.60/24,fe80::1/64" {
		t.Errorf("addresses = %q", e.Addresses)
	}
	d := info.Interfaces[1]
	if d.State != "down" || d.Carrier != nil || d.SpeedMbps != nil || d.Duplex != "" || len(d.Addresses) != 0 {
		t.Errorf("down interface = %+v", d)
	}
	if len(info.Routes) != 3 {
		t.Fatalf("routes = %+v (multicast must be skipped)", info.Routes)
	}
	r := info.Routes[0]
	if r.Family != "inet" || r.Destination != "default" || r.Gateway != "192.0.2.1" || r.Protocol != "static" || r.Metric != nil {
		t.Errorf("default route = %+v", r)
	}
	if info.Routes[1].Source != "192.0.2.60" || info.Routes[1].Scope != "link" {
		t.Errorf("subnet route = %+v", info.Routes[1])
	}
	if r6 := info.Routes[2]; r6.Family != "inet6" || r6.Metric == nil || *r6.Metric != 256 {
		t.Errorf("ipv6 route = %+v", r6)
	}
}

func TestReadWithoutIPv6AndResolved(t *testing.T) {
	run := runner(false)
	noResolved := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "resolvectl" {
			return nil, errors.New("not found")
		}
		return run(ctx, name, args...)
	}
	info, err := Reader{Root: fixture(t), Run: noResolved, Now: time.Now}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Routes) != 2 || len(info.Domains) != 0 || info.Domains == nil {
		t.Errorf("routes %d, domains %#v", len(info.Routes), info.Domains)
	}
}

func TestReadFailsWithoutIP(t *testing.T) {
	fail := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no ip") }
	if _, err := (Reader{Root: fixture(t), Run: fail, Now: time.Now}).Read(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}
