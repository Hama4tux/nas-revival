package firewall

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func allow(web, ssh, smb, nfs []string) Settings {
	return Settings{Enabled: true, Allow: map[string][]string{"web": web, "ssh": ssh, "smb": smb, "nfs": nfs}}
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("not a FieldError: %v", err)
	}
	return fe.Field
}

func TestValidateNormalises(t *testing.T) {
	in := allow([]string{"192.168.1.77/24", "192.168.1.0/24", "10.0.0.5/32", "10.0.0.5"}, []string{"192.168.1.0/24"}, nil, nil)
	got, err := Validate(in, "192.168.1.9")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.5", "192.168.1.0/24"}
	if !reflect.DeepEqual(got.Allow["web"], want) {
		t.Fatalf("%v", got.Allow["web"])
	}
	if got.Allow["smb"] == nil || got.Allow["nfs"] == nil || len(got.Allow) != 4 {
		t.Fatalf("keys missing: %v", got.Allow)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "null") {
		t.Fatal(string(b))
	}
}

func TestValidateErrors(t *testing.T) {
	lan := []string{"192.168.1.0/24"}
	many := make([]string, 17)
	for i := range many {
		many[i] = "10.0.0." + string(rune('1'+i%9)) + strings.Repeat("0", i/9)
	}
	cases := []struct {
		name  string
		s     Settings
		field string
	}{
		{"unknown key", Settings{Allow: map[string][]string{"ftp": lan}}, "allow"},
		{"ipv6", allow(lan, []string{"fd00::1"}, nil, nil), "allow.ssh"},
		{"garbage", allow(lan, lan, []string{"abc"}, nil), "allow.smb"},
		{"ipv6 net", allow(lan, lan, nil, []string{"fd00::/64"}), "allow.nfs"},
		{"too many", allow(many, lan, nil, nil), "allow.web"},
	}
	for _, c := range cases {
		_, err := Validate(c.s, "192.168.1.9")
		if err == nil || fieldOf(t, err) != c.field {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestValidateLockout(t *testing.T) {
	lan := []string{"192.168.1.0/24"}
	_, err := Validate(allow(lan, lan, nil, nil), "203.0.113.9")
	if err == nil || fieldOf(t, err) != "allow.web" || !strings.Contains(err.Error(), "203.0.113.9") {
		t.Fatalf("%v", err)
	}
	_, err = Validate(allow(lan, []string{"10.0.0.0/8"}, nil, nil), "192.168.1.9")
	if err == nil || fieldOf(t, err) != "allow.ssh" {
		t.Fatalf("%v", err)
	}
	if _, err := Validate(allow(nil, nil, nil, nil), "fd00::1"); err == nil {
		t.Fatal("ipv6 client accepted")
	}
	// Disabled: nothing to lock out.
	off := allow(nil, nil, nil, nil)
	off.Enabled = false
	if _, err := Validate(off, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	// Stored settings are checked without a client.
	if _, err := Validate(allow(lan, lan, nil, nil), ""); err != nil {
		t.Fatal(err)
	}
}

func TestRenderDisabled(t *testing.T) {
	want := "table ip nas_revival {}\ndelete table ip nas_revival\n"
	if got := Render(Default()); got != want {
		t.Fatalf("%q", got)
	}
}

func TestRenderGolden(t *testing.T) {
	s, err := Validate(allow([]string{"192.168.1.0/24"}, []string{"192.168.1.0/24", "10.0.0.5"}, nil,
		[]string{"192.168.1.0/24"}).withRules(
		Rule{Source: "10.0.0.7/32", Ports: "445", Proto: "tcp", Action: "drop"},
		Rule{Source: "192.168.1.0/24", Ports: "6000-6010", Proto: "both", Action: "accept"},
		Rule{Source: "10.0.0.0/8", Proto: "udp", Action: "drop", Note: "x"},
		Rule{Source: "192.168.1.5", Proto: "both", Action: "accept"},
		Rule{Source: "192.168.1.0/24", Ports: "8080", Proto: "udp", Action: "accept"}), "192.168.1.9")
	if err != nil {
		t.Fatal(err)
	}
	want := `table ip nas_revival {}
delete table ip nas_revival
table ip nas_revival {
	chain input {
		type filter hook input priority 0; policy drop;
		iifname "lo" accept
		ct state established,related accept
		ct state invalid drop
		icmp type echo-request limit rate 10/second accept
		udp sport 67 udp dport 68 accept
		ip saddr 10.0.0.7 tcp dport 445 drop
		ip saddr 192.168.1.0/24 meta l4proto { tcp, udp } th dport 6000-6010 accept
		ip saddr 10.0.0.0/8 ip protocol udp drop
		ip saddr 192.168.1.5 ip protocol { tcp, udp } accept
		ip saddr 192.168.1.0/24 udp dport 8080 accept
		ip saddr 192.168.1.0/24 tcp dport 8443 accept
		ip saddr { 10.0.0.5, 192.168.1.0/24 } tcp dport 22 accept
		ip saddr 192.168.1.0/24 tcp dport { 2049, 111 } accept
	}
}
`
	if got := Render(s); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestLoadSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "firewall.json")
	d, err := Load(p)
	if err != nil || d.Enabled || len(d.Allow) != 4 {
		t.Fatalf("%+v %v", d, err)
	}
	lan := []string{"192.168.1.0/24"}
	s, _ := Validate(allow(lan, lan, nil, nil), "192.168.1.9")
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestValidateDropsCoveredSources(t *testing.T) {
	in := allow([]string{"192.168.1.5", "192.168.1.0/24", "10.0.0.0/8", "10.1.0.0/16"}, []string{"192.168.1.0/24"}, nil, nil)
	out, err := Validate(in, "192.168.1.5")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.Allow["web"], []string{"10.0.0.0/8", "192.168.1.0/24"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("web = %v, want %v", got, want)
	}
}

func TestValidateMappedClientAddress(t *testing.T) {
	in := allow([]string{"192.168.1.0/24"}, []string{"192.168.1.0/24"}, nil, nil)
	if _, err := Validate(in, "::ffff:192.168.1.5"); err != nil {
		t.Fatalf("mapped IPv4 client refused: %v", err)
	}
}

func (s Settings) withRules(r ...Rule) Settings { s.Rules = r; return s }

func TestValidateRules(t *testing.T) {
	lan := []string{"192.168.1.0/24"}
	base := allow(lan, lan, nil, nil)
	got, err := Validate(base.withRules(
		Rule{Source: "10.1.2.3/8", Ports: " 06000 - 6010 ", Proto: "udp", Action: "drop", Note: "  Hra  "},
		Rule{Source: "0.0.0.0/0", Proto: "both", Action: "accept"}), "192.168.1.9")
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{"10.0.0.0/8", "6000-6010", "udp", "drop", "Hra"}, {"0.0.0.0/0", "", "both", "accept", ""}}
	if !reflect.DeepEqual(got.Rules, want) {
		t.Fatalf("%+v", got.Rules)
	}
	if g, _ := Validate(base, ""); g.Rules == nil {
		t.Fatal("rules must be non-nil")
	}
	var old Settings
	if err := json.Unmarshal([]byte(`{"enabled":false,"allow":{}}`), &old); err != nil {
		t.Fatal(err)
	}
	if g, err := Validate(old, ""); err != nil || g.Rules == nil || len(g.Rules) != 0 {
		t.Fatalf("%+v %v", g, err)
	}
	ok := Rule{Source: "10.0.0.1", Proto: "tcp", Action: "accept"}
	many := make([]Rule, 65)
	for i := range many {
		many[i] = ok
	}
	bad := func(r Rule) []Rule { return []Rule{ok, r} }
	cases := []struct {
		name  string
		r     []Rule
		field string
	}{
		{"no source", bad(Rule{Proto: "tcp", Action: "drop"}), "rules.1.source"},
		{"ipv6", bad(Rule{Source: "fd00::1", Proto: "tcp", Action: "drop"}), "rules.1.source"},
		{"proto", bad(Rule{Source: "10.0.0.2", Proto: "sctp", Action: "drop"}), "rules.1.proto"},
		{"action", bad(Rule{Source: "10.0.0.2", Proto: "tcp", Action: "reject"}), "rules.1.action"},
		{"port 0", bad(Rule{Source: "10.0.0.2", Ports: "0", Proto: "tcp", Action: "drop"}), "rules.1.ports"},
		{"port big", bad(Rule{Source: "10.0.0.2", Ports: "65536", Proto: "tcp", Action: "drop"}), "rules.1.ports"},
		{"range order", bad(Rule{Source: "10.0.0.2", Ports: "90-80", Proto: "tcp", Action: "drop"}), "rules.1.ports"},
		{"garbage", bad(Rule{Source: "10.0.0.2", Ports: "80,81", Proto: "tcp", Action: "drop"}), "rules.1.ports"},
		{"sign", bad(Rule{Source: "10.0.0.2", Ports: "+80", Proto: "tcp", Action: "drop"}), "rules.1.ports"},
		{"long note", bad(Rule{Source: "10.0.0.2", Proto: "tcp", Action: "drop", Note: strings.Repeat("a", 41)}), "rules.1.note"},
		{"control note", bad(Rule{Source: "10.0.0.2", Proto: "tcp", Action: "drop", Note: "a\nb"}), "rules.1.note"},
		{"too many", many, "rules"},
	}
	for _, c := range cases {
		_, err := Validate(base.withRules(c.r...), "192.168.1.9")
		if err == nil || fieldOf(t, err) != c.field {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestValidateRulesLockout(t *testing.T) {
	lan := []string{"192.168.1.0/24"}
	base := allow(lan, lan, nil, nil)
	client := "192.168.1.9"
	drop := func(src, ports string) Rule { return Rule{Source: src, Ports: ports, Proto: "tcp", Action: "drop"} }
	// drop of web / ssh / everything for the client is refused, field names the rule
	for _, c := range []struct {
		r    Rule
		what string
	}{{drop(client, "8443"), "webové"}, {drop("192.168.1.0/24", "22"), "SSH"}, {drop("0.0.0.0/0", ""), "webové"},
		{Rule{Source: client, Proto: "both", Action: "drop"}, "webové"}} {
		_, err := Validate(base.withRules(drop("10.0.0.1", "8443"), c.r), client)
		if err == nil || fieldOf(t, err) != "rules.1.action" || !strings.Contains(err.Error(), c.what) {
			t.Errorf("%+v: %v", c.r, err)
		}
	}
	// other sources, ports or protocols do not matter
	for _, r := range []Rule{drop("10.0.0.0/8", "8443"), drop(client, "445"), {Source: client, Ports: "8443", Proto: "udp", Action: "drop"}} {
		if _, err := Validate(base.withRules(r), client); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	// an earlier accept wins over a later drop
	acc := Rule{Source: client, Proto: "tcp", Action: "accept"}
	if _, err := Validate(base.withRules(acc, drop(client, "8443")), client); err != nil {
		t.Fatal(err)
	}
	// an accept rule replaces the service list for that service
	if _, err := Validate(allow(nil, nil, nil, nil).withRules(acc), client); err != nil {
		t.Fatal(err)
	}
	// without a matching rule the service lists still decide
	if _, err := Validate(allow(nil, lan, nil, nil).withRules(drop("10.0.0.1", "")), client); err == nil || fieldOf(t, err) != "allow.web" {
		t.Fatalf("%v", err)
	}
}

func TestAllowed(t *testing.T) {
	s := Default()
	s.Allow["smb"] = []string{"192.168.1.0/24"}
	s.Allow["nfs"] = nil
	s.Rules = []Rule{
		{Source: "10.0.0.1", Ports: "6000-6010", Proto: "udp", Action: "accept"},
		{Source: "10.0.0.1", Ports: "8080", Proto: "both", Action: "accept"},
		{Source: "10.0.0.0/8", Ports: "9000", Proto: "tcp", Action: "drop"}, // not for everyone: no effect
		{Source: "10.0.0.1", Ports: "9000", Proto: "tcp", Action: "accept"},
		{Source: "0.0.0.0/0", Ports: "445", Proto: "tcp", Action: "drop"},  // blocks smb for all
		{Source: "10.0.0.1", Ports: "445", Proto: "tcp", Action: "accept"}, // never reached: only first match
		{Source: "0.0.0.0/0", Ports: "22", Proto: "tcp", Action: "drop"},
		{Source: "0.0.0.0/0", Proto: "udp", Action: "drop"}, // all udp, DHCP included
	}
	for _, c := range []struct {
		proto string
		port  int
		want  bool
	}{
		{"tcp", 445, false}, {"udp", 445, false}, {"tcp", 2049, false}, {"tcp", 22, false},
		{"udp", 6005, true}, {"tcp", 6005, false}, {"udp", 6011, false},
		{"tcp", 8080, true}, {"udp", 8080, true}, {"tcp", 9000, true}, {"udp", 68, false}, {"tcp", 68, false},
	} {
		if got := Allowed(s, c.proto, c.port); got != c.want {
			t.Errorf("%s/%d = %t", c.proto, c.port, got)
		}
	}
	// without the drop-all rules the catalogue and DHCP decide
	s.Rules = nil
	if !Allowed(s, "tcp", 445) || !Allowed(s, "udp", 68) || Allowed(s, "tcp", 2049) {
		t.Error("fallback")
	}
}

func TestListening(t *testing.T) {
	s := Default()
	s.Allow["ssh"] = []string{"192.168.1.0/24"}
	got := Listening(s, []Socket{
		{"udp", "127.0.0.54", 53, "resolved"},
		{"tcp", "127.0.0.1", 631, "cups"},
		{"tcp", "::1", 25, "smtp"},
		{"udp", "127.0.0.53", 5353, "lo"},
		{"tcp", "0.0.0.0", 22, "sshd"},
		{"tcp", "::", 22, ""},
		{"tcp", "*", 445, "smbd"},
		{"tcp", "192.168.1.60", 8443, "nas-revival"},
		{"udp", "0.0.0.0", 68, "dhclient"},
		{"udp", "::", 5353, "avahi"},
	})
	want := []Listen{
		{"tcp", 22, "sshd", true}, {"tcp", 445, "smbd", false}, {"tcp", 8443, "nas-revival", false},
		{"udp", 68, "dhclient", true}, {"udp", 5353, "avahi", false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if g := Listening(s, nil); g == nil || len(g) != 0 {
		t.Fatalf("%#v", g)
	}
}
