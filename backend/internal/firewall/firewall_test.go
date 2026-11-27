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
		[]string{"192.168.1.0/24"}), "192.168.1.9")
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
