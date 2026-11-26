package smb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	s, err := Validate(Settings{Workgroup: " home-lan ", Description: " Domácí NAS ", MinProtocol: ProtoSMB3})
	if err != nil || s.Workgroup != "HOME-LAN" || s.Description != "Domácí NAS" || s.Version != 1 {
		t.Fatalf("got %+v, %v", s, err)
	}
	bad := []struct {
		s     Settings
		field string
	}{
		{Settings{Workgroup: "", MinProtocol: ProtoSMB2}, "workgroup"},
		{Settings{Workgroup: "SIXTEEN-CHARS-AB", MinProtocol: ProtoSMB2}, "workgroup"},
		{Settings{Workgroup: "SKUPINA Č", MinProtocol: ProtoSMB2}, "workgroup"},
		{Settings{Workgroup: "WG", MinProtocol: "NT1"}, "minProtocol"},
		{Settings{Workgroup: "WG", MinProtocol: ""}, "minProtocol"},
		{Settings{Workgroup: "WG", MinProtocol: ProtoSMB2, Description: "a%hb"}, "description"},
		{Settings{Workgroup: "WG", MinProtocol: ProtoSMB2, Description: "a\nworkgroup = X"}, "description"},
		{Settings{Workgroup: "WG", MinProtocol: ProtoSMB2, Description: "line\\"}, "description"},
		{Settings{Workgroup: "WG", MinProtocol: ProtoSMB2, Description: strings.Repeat("x", 65)}, "description"},
	}
	for _, c := range bad {
		var fe *FieldError
		if _, err := Validate(c.s); !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%+v: err %v, want field %s", c.s, err, c.field)
		}
	}
}

func TestRender(t *testing.T) {
	s := Default()
	s.Enabled, s.Workgroup, s.MinProtocol = true, "HOME", ProtoSMB31
	out := Render(s)
	for _, want := range []string{
		"[global]\n", "workgroup = HOME\n", "server string = NAS Revival\n",
		"server min protocol = SMB3_11\n", "server max protocol = SMB3\n",
		"map to guest = never\n", "restrict anonymous = 2\n", "usershare max shares = 0\n",
		"load printers = no\n", "disable netbios = yes\n", "smb ports = 445\n",
		"\ninclude = /etc/samba/nas-revival-shares.conf\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "[homes]") || strings.Contains(out, "guest ok") || strings.Contains(out, "NT1") {
		t.Fatalf("unsafe section or option:\n%s", out)
	}
	s.NetBIOS = true
	out = Render(s)
	if !strings.Contains(out, "disable netbios = no\n") || !strings.Contains(out, "smb ports = 445 139\n") {
		t.Fatalf("netbios:\n%s", out)
	}
}

func TestLoadSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "smb.json")
	s, err := Load(p)
	if err != nil || s != Default() || s.Enabled {
		t.Fatalf("missing file = %+v, %v", s, err)
	}
	s.Enabled, s.NetBIOS = true, true
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got != s {
		t.Fatalf("round trip %+v, %v", got, err)
	}
	os.WriteFile(p, []byte(`{"version":2}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("version 2 accepted")
	}
	os.WriteFile(p, []byte(`{"version":1,"workgroup":"WG","minProtocol":"NT1"}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("SMB1 accepted from file")
	}
}
