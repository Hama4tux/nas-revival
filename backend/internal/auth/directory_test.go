package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nas-revival/backend/internal/accounts"
)

const (
	dirPasswd = "root:x:0:0:root:/root:/bin/bash\n" +
		"debian:x:1000:1000::/home/debian:/bin/bash\n" +
		"jana:x:1001:990::/nonexistent:/usr/sbin/nologin\n" +
		"petr:x:1002:990::/nonexistent:/usr/sbin/nologin\n" +
		"admin:x:1003:990::/nonexistent:/usr/sbin/nologin\n" +
		"eva:x:1004:990::/nonexistent:/usr/sbin/nologin\n"
	dirGroup = "root:x:0:\nnas-users:x:990:\ndebian:x:1000:\nnas-admin:x:991:jana\nnas-viewer:x:992:petr,jana\n"
)

func newDirectory(t *testing.T, web map[string]accounts.WebPassword, admin *Credentials) (*Directory, accounts.Paths) {
	t.Helper()
	dir := t.TempDir()
	p := accounts.Paths{Passwd: filepath.Join(dir, "passwd"), Group: filepath.Join(dir, "group")}
	os.WriteFile(p.Passwd, []byte(dirPasswd), 0o644)
	os.WriteFile(p.Group, []byte(dirGroup), 0o644)
	w := accounts.WebPasswords{Path: filepath.Join(dir, "credentials.json"), OwnerUID: -1, OwnerGID: -1}
	if err := w.Save(web); err != nil {
		t.Fatal(err)
	}
	return &Directory{Paths: p, Web: w, Admin: admin}, p
}

func TestDirectoryRolesAndRecoveryAdmin(t *testing.T) {
	d, _ := newDirectory(t, map[string]accounts.WebPassword{
		"jana": {Hash: "hj"}, "petr": {Hash: "hp"}, "eva": {Hash: "he"},
		"debian": {Hash: "hd"}, // not a NAS account: ignored
		"admin":  {Hash: "ha", Disabled: true},
	}, &Credentials{Username: "admin", Hash: "recovery"})
	for _, c := range []struct {
		name string
		hash string
		role accounts.Role
		ok   bool
	}{
		{"jana", "hj", accounts.RoleAdmin, true}, // admin wins over viewer
		{"petr", "hp", accounts.RoleViewer, true},
		{"eva", "he", accounts.RoleUser, true},
		{"debian", "", "", false},
		{"root", "", "", false},
		// A NAS account named like the recovery administrator replaces it,
		// also when that account is disabled.
		{"admin", "", "", false},
		{"nobody", "", "", false},
	} {
		h, r, ok := d.Account(c.name)
		if h != c.hash || r != c.role || ok != c.ok {
			t.Errorf("%s = %q %q %v", c.name, h, r, ok)
		}
	}
}

func TestDirectoryRecoveryAdminWithoutNASAccount(t *testing.T) {
	d, _ := newDirectory(t, nil, &Credentials{Username: "root-web", Hash: "recovery"})
	if h, r, ok := d.Account("root-web"); h != "recovery" || r != accounts.RoleAdmin || !ok {
		t.Fatalf("recovery = %q %q %v", h, r, ok)
	}
	if d.IsNASUser("root-web") || !d.IsNASUser("jana") {
		t.Error("IsNASUser")
	}
}

func TestDirectoryReloadsChangedFiles(t *testing.T) {
	d, p := newDirectory(t, map[string]accounts.WebPassword{"eva": {Hash: "he"}}, nil)
	if r, ok := d.Role("eva"); !ok || r != accounts.RoleUser {
		t.Fatalf("eva = %q %v", r, ok)
	}
	os.WriteFile(p.Group, []byte(dirGroup+"x:x:5:\n"), 0o644) // size changes
	later := time.Now().Add(time.Minute)
	os.Chtimes(p.Group, later, later)
	os.WriteFile(p.Group, []byte("root:x:0:\nnas-users:x:990:\nnas-admin:x:991:eva\n"), 0o644)
	if r, _ := d.Role("eva"); r != accounts.RoleAdmin {
		t.Errorf("after group change eva = %q", r)
	}
	if err := d.Web.Save(map[string]accounts.WebPassword{"eva": {Hash: "he", Disabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Role("eva"); ok {
		t.Error("disabled account still has a role")
	}
}

func TestDirectoryFailsClosedOnBrokenCredentials(t *testing.T) {
	d, _ := newDirectory(t, nil, &Credentials{Username: "rescue", Hash: "recovery"})
	os.WriteFile(d.Web.Path, []byte("{broken"), 0o600)
	if _, ok := d.Role("jana"); ok {
		t.Error("NAS account accepted with unreadable credentials.json")
	}
	if _, ok := d.Role("rescue"); !ok {
		t.Error("recovery administrator must still work")
	}
}

func TestLookupVerifier(t *testing.T) {
	h, _ := HashPassword("secret-pass")
	v, err := NewLookupVerifier(func(u string) (string, bool) { return h, u == "eva" })
	if err != nil {
		t.Fatal(err)
	}
	if v.Verify("eva", "secret-pass") != nil || v.Verify("eva", "wrong") == nil || v.Verify("jana", "secret-pass") == nil {
		t.Error("verify")
	}
	broken, _ := NewLookupVerifier(func(string) (string, bool) { return "not-a-hash", true })
	if broken.Verify("eva", "x") == nil {
		t.Error("unparsable stored hash accepted")
	}
}
