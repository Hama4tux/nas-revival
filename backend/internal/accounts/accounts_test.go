package accounts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShadowStates(t *testing.T) {
	today := 20418
	cases := []struct {
		e              ShadowEntry
		locked, hasPwd bool
	}{
		{ShadowEntry{Hash: "$y$abc"}, false, true},
		{ShadowEntry{Hash: "!$y$abc"}, true, true},
		{ShadowEntry{Hash: "$y$abc", Expire: "1"}, true, true},
		{ShadowEntry{Hash: "$y$abc", Expire: "30000"}, false, true},
		{ShadowEntry{Hash: "!"}, true, false},
		{ShadowEntry{Hash: "*"}, false, false},
	}
	for _, c := range cases {
		if c.e.Locked(today) != c.locked || c.e.HasPassword() != c.hasPwd {
			t.Errorf("%+v: locked=%v has=%v", c.e, c.e.Locked(today), c.e.HasPassword())
		}
	}
	a, b := ShadowEntry{Hash: "$y$abc"}, ShadowEntry{Hash: "!$y$abc"}
	if a.Fingerprint() != b.Fingerprint() || a.Fingerprint() == (ShadowEntry{Hash: "$y$other"}).Fingerprint() {
		t.Error("fingerprint must ignore the lock prefix only")
	}
}

func TestParseSkipsMalformedLines(t *testing.T) {
	p := ParsePasswd([]byte("# comment\n+nis\nbad:line\nok:x:1001:990:Full,Room:/h:/s\nnum:x:abc:1::/h:/s\n"))
	if len(p) != 1 || p[0].Name != "ok" || FullName(p[0].Gecos) != "Full" {
		t.Fatalf("passwd = %+v", p)
	}
	g := ParseGroup([]byte("g:x:1002:a, b,,\n"))
	if len(g) != 1 || len(g[0].Members) != 2 || g[0].Members[1] != "b" {
		t.Fatalf("group = %+v", g)
	}
}

func TestParseSkipsOutOfRangeIDs(t *testing.T) {
	p := ParsePasswd([]byte("neg:x:-1:1::/h:/s\nbig:x:4294967295:1::/h:/s\nbiggid:x:1:2147483648::/h:/s\nok:x:2147483647:0::/h:/s\n"))
	if len(p) != 1 || p[0].Name != "ok" || p[0].UID != 2147483647 {
		t.Fatalf("passwd = %+v", p)
	}
	if g := ParseGroup([]byte("neg:x:-5:\nbig:x:99999999999:\n")); len(g) != 0 {
		t.Fatalf("group = %+v", g)
	}
}

func TestWebPasswordsRoundTrip(t *testing.T) {
	w := WebPasswords{Path: filepath.Join(t.TempDir(), "sub", "credentials.json"), OwnerUID: -1, OwnerGID: -1}
	if m, err := w.Load(); err != nil || len(m) != 0 {
		t.Fatalf("missing file: %v %v", m, err)
	}
	if err := w.Save(map[string]WebPassword{"jana": {Hash: "h", Changed: "c", Shadow: "s"}}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(w.Path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	m, err := w.Load()
	if err != nil || m["jana"].Hash != "h" {
		t.Fatalf("load: %v %v", m, err)
	}
}
