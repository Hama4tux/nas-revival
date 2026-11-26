package sshd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := []int{22, 1024, 2222, 65535}
	for _, p := range ok {
		s := Default()
		s.Port = p
		if _, err := Validate(s); err != nil {
			t.Errorf("port %d: %v", p, err)
		}
	}
	for _, p := range []int{0, 21, 23, 1023, 65536, 2049, 8443} {
		s := Default()
		s.Port = p
		if _, err := Validate(s); err == nil {
			t.Errorf("port %d accepted", p)
		}
	}
	for _, g := range []string{"root", "Bad", "a b", "x;y", ""} {
		s := Default()
		s.SftpGroups = []string{g}
		if _, err := Validate(s); err == nil {
			t.Errorf("group %q accepted", g)
		}
	}
	s := Default()
	s.SftpGroups = []string{"nas-users", "foto", "nas-users"}
	n, err := Validate(s)
	if err != nil || !reflect.DeepEqual(n.SftpGroups, []string{"foto", "nas-users"}) {
		t.Fatalf("groups = %v, %v", n.SftpGroups, err)
	}
}

func TestRenderRootOnly(t *testing.T) {
	s := Default()
	s.PasswordAuth = false
	out := Render(s)
	for _, want := range []string{"Port 22\n", "PermitRootLogin prohibit-password\n", "PasswordAuthentication no\n",
		"AllowGroups root\n", "AuthorizedKeysFile .ssh/authorized_keys " + KeysDir + "/%u\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Match") {
		t.Errorf("Match block without SFTP groups:\n%s", out)
	}
}

func TestRenderSftp(t *testing.T) {
	s := Default()
	s.Port = 2222
	s.SftpGroups = []string{"foto", "nas-users"}
	out := Render(s)
	if !strings.Contains(out, "AllowGroups root foto nas-users\n") || !strings.Contains(out, "Match Group foto,nas-users\n") ||
		!strings.Contains(out, "\tChrootDirectory /srv\n") || !strings.Contains(out, "\tForceCommand internal-sftp\n") ||
		!strings.Contains(out, "\tPermitTTY no\n") {
		t.Fatalf("render:\n%s", out)
	}
	// Global options must come before the Match block.
	if strings.Index(out, "Match") < strings.Index(out, "AllowGroups") {
		t.Fatal("Match before global options")
	}
}

func TestSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ssh.json")
	if s, err := Load(p); err != nil || !reflect.DeepEqual(s, Default()) {
		t.Fatalf("missing file = %+v, %v", s, err)
	}
	s := Default()
	s.Port, s.SftpGroups = 2222, []string{"nas-users"}
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	if _, err := Parse([]byte(`{"version":2,"port":22}`)); err == nil {
		t.Fatal("version 2 accepted")
	}
}

func TestParseEffective(t *testing.T) {
	raw := "port 22\nport 2222\naddressfamily any\npasswordauthentication no\npermitrootlogin without-password\n"
	e := ParseEffective([]byte(raw))
	if !reflect.DeepEqual(e.Ports, []int{22, 2222}) || e.PasswordAuth || e.RootLogin != "without-password" {
		t.Fatalf("effective = %+v", e)
	}
}
