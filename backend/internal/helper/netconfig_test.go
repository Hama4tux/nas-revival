package helper

import (
	"reflect"
	"strings"
	"testing"
)

func static(addr string, pfx int, gw string, dns ...string) NetConfig {
	return NetConfig{Mode: ModeStatic, Address: &addr, PrefixLength: &pfx, Gateway: &gw, DNS: dns, UseDhcpDns: true}
}

func TestValidateNetConfig(t *testing.T) {
	tests := []struct {
		name  string
		cfg   NetConfig
		field string // "" = valid
	}{
		{"dhcp", DefaultNetConfig(), ""},
		{"dhcp custom dns", NetConfig{Mode: ModeDHCP, DNS: []string{"1.1.1.1"}, UseDhcpDns: false}, ""},
		{"dhcp no dns at all", NetConfig{Mode: ModeDHCP, UseDhcpDns: false}, "dns"},
		{"bad mode", NetConfig{Mode: "x"}, "mode"},
		{"static ok", static("192.168.1.10", 24, "192.168.1.1", "192.168.1.1", "2001:db8::1"), ""},
		{"static no dns", static("192.168.1.10", 24, "192.168.1.1"), "dns"},
		{"4 dns", static("192.168.1.10", 24, "192.168.1.1", "1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"), "dns"},
		{"bad dns", static("192.168.1.10", 24, "192.168.1.1", "foo"), "dns"},
		{"zone dns", static("192.168.1.10", 24, "192.168.1.1", "fe80::1%eth0"), "dns"},
		{"network addr", static("192.168.1.0", 24, "192.168.1.1", "1.1.1.1"), "address"},
		{"broadcast addr", static("192.168.1.255", 24, "192.168.1.1", "1.1.1.1"), "address"},
		{"loopback", static("127.0.0.2", 8, "127.0.0.1", "1.1.1.1"), "address"},
		{"multicast", static("224.0.0.5", 24, "224.0.0.1", "1.1.1.1"), "address"},
		{"link-local", static("169.254.1.5", 24, "169.254.1.1", "1.1.1.1"), "address"},
		{"unspecified", static("0.0.0.0", 24, "0.0.0.1", "1.1.1.1"), "address"},
		{"ipv6 addr", static("2001:db8::5", 24, "192.168.1.1", "1.1.1.1"), "address"},
		{"garbage addr", static("1.2.3", 24, "192.168.1.1", "1.1.1.1"), "address"},
		{"prefix 7", static("10.1.1.5", 7, "10.1.1.1", "1.1.1.1"), "prefixLength"},
		{"prefix 31", static("10.1.1.5", 31, "10.1.1.4", "1.1.1.1"), "prefixLength"},
		{"prefix 8", static("10.1.1.5", 8, "10.1.1.1", "1.1.1.1"), ""},
		{"prefix 30", static("10.1.1.5", 30, "10.1.1.6", "1.1.1.1"), ""},
		{"gw outside", static("192.168.1.10", 24, "192.168.2.1", "1.1.1.1"), "gateway"},
		{"gw == addr", static("192.168.1.10", 24, "192.168.1.10", "1.1.1.1"), "gateway"},
		{"gw broadcast", static("192.168.1.10", 24, "192.168.1.255", "1.1.1.1"), "gateway"},
		{"gw garbage", static("192.168.1.10", 24, "x;rm", "1.1.1.1"), "gateway"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateNetConfig(tc.cfg)
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			e, ok := err.(*Error)
			if !ok || e.Code != CodeInvalid || e.Field != tc.field {
				t.Fatalf("got %#v, want field %q", err, tc.field)
			}
		})
	}
	nilAddr := NetConfig{Mode: ModeStatic, DNS: []string{"1.1.1.1"}}
	if _, err := ValidateNetConfig(nilAddr); err == nil {
		t.Fatal("nil address accepted")
	}
}

func TestRenderParse(t *testing.T) {
	if got := RenderNetwork(DefaultNetConfig()); got != "[Match]\nName=e*\n\n[Network]\nDHCP=yes\n" {
		t.Fatalf("default render %q", got)
	}
	cases := []NetConfig{
		DefaultNetConfig(),
		{Mode: ModeDHCP, DNS: []string{"9.9.9.9", "2620:fe::fe"}, UseDhcpDns: true},
		{Mode: ModeDHCP, DNS: []string{"9.9.9.9"}, UseDhcpDns: false},
		static("192.168.1.10", 24, "192.168.1.1", "192.168.1.1", "1.1.1.1"),
	}
	for _, c := range cases {
		out := RenderNetwork(c)
		if !strings.HasPrefix(out, "[Match]\nName=e*\n") {
			t.Fatalf("lost match: %q", out)
		}
		if got := ParseNetwork(out); !reflect.DeepEqual(got, c) {
			t.Fatalf("roundtrip\n%s\n got %+v\nwant %+v", out, got, c)
		}
	}
	want := "[Match]\nName=e*\n\n[Network]\nAddress=192.168.1.10/24\nGateway=192.168.1.1\nDNS=1.1.1.1\n"
	if got := RenderNetwork(static("192.168.1.10", 24, "192.168.1.1", "1.1.1.1")); got != want {
		t.Fatalf("static render %q", got)
	}
	if got := RenderNetwork(cases[2]); !strings.Contains(got, "\n[DHCPv4]\nUseDNS=no\n") {
		t.Fatalf("missing UseDNS: %q", got)
	}
}

func TestParseTolerant(t *testing.T) {
	for _, in := range []string{"", "garbage\n=\n[", "[Network]\nDHCP=ipv4\nAddress=10.0.0.5/24\n"} {
		c := ParseNetwork(in)
		if c.Mode != ModeDHCP || c.DNS == nil {
			t.Fatalf("%q -> %+v", in, c)
		}
	}
	c := ParseNetwork("# c\n[Network]\nAddress=fe80::1/64\nAddress=10.0.0.5/16\nGateway=10.0.0.1\nDNS=1.1.1.1 8.8.8.8\nDNS=bogus\nFoo=bar\n[Route]\nGateway=1.2.3.4\n")
	if c.Mode != ModeStatic || *c.Address != "10.0.0.5" || *c.PrefixLength != 16 || *c.Gateway != "10.0.0.1" || len(c.DNS) != 2 {
		t.Fatalf("%+v", c)
	}
}

func TestIfaceName(t *testing.T) {
	for n, want := range map[string]bool{"enp1s0": true, "eth0": true, "a": false, "1eth": false, "en p": false, "eth0;ls": false, "../x": false, "abcdefghijklmnop": false, "": false} {
		if ValidIfaceName(n) != want {
			t.Errorf("%q", n)
		}
	}
}
