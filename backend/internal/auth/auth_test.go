package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=16384,t=3,p=1$") {
		t.Fatalf("hash %s", h)
	}
	v, err := NewVerifier(Credentials{Username: "admin", Hash: h})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify("admin", "correct horse"); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"admin", "wrong"}, {"root", "correct horse"}, {"", ""}} {
		if err := v.Verify(c[0], c[1]); err != ErrInvalidCredentials {
			t.Fatalf("%v accepted", c)
		}
	}
}

func TestParseHashRejectsHostileParameters(t *testing.T) {
	for _, h := range []string{
		"$argon2id$v=19$m=1048576,t=3,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=16384,t=1000,p=1$c2FsdA$a2V5",
		"$argon2i$v=19$m=16384,t=3,p=1$c2FsdA$a2V5",
		"plain",
	} {
		if _, err := parseHash(h); err == nil {
			t.Errorf("accepted %s", h)
		}
	}
}

func TestCredentialsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "admin.json")
	h, _ := HashPassword("x")
	if err := SaveCredentials(p, Credentials{Username: "admin", Hash: h}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	c, err := LoadCredentials(p)
	if err != nil || c.Username != "admin" {
		t.Fatalf("%v %+v", err, c)
	}
	os.Chmod(p, 0o644)
	if _, err := LoadCredentials(p); err == nil {
		t.Fatal("world-readable file must be refused")
	}
}

func TestSessions(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	s := NewSessions()
	s.Now = func() time.Time { return now }
	tok, err := s.Create("admin", Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if u, _, ok := s.Lookup(tok); !ok || u != "admin" {
		t.Fatal("lookup failed")
	}
	if _, _, ok := s.Lookup(tok + "x"); ok {
		t.Fatal("tampered token accepted")
	}
	// Activity every 20 min keeps it alive until the absolute limit.
	for i := 0; i < 35; i++ {
		now = now.Add(20 * time.Minute)
		_, _, ok := s.Lookup(tok)
		if want := now.Sub(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)) < AbsoluteTimeout; ok != want {
			t.Fatalf("step %d: ok=%v want %v", i, ok, want)
		}
	}
	tok2, _ := s.Create("admin", Meta{})
	now = now.Add(IdleTimeout)
	if _, _, ok := s.Lookup(tok2); ok {
		t.Fatal("idle session must expire")
	}
	tok3, _ := s.Create("admin", Meta{})
	s.Delete(tok3)
	if _, _, ok := s.Lookup(tok3); ok {
		t.Fatal("deleted session accepted")
	}
}

func TestSessionsBounded(t *testing.T) {
	s := NewSessions()
	first, _ := s.Create("admin", Meta{})
	for i := 0; i < maxSessions; i++ {
		s.Create("admin", Meta{})
	}
	if len(s.m) > maxSessions {
		t.Fatalf("%d sessions", len(s.m))
	}
	if _, _, ok := s.Lookup(first); ok {
		t.Fatal("oldest session should have been evicted")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	l := NewLimiter()
	l.Now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, b := l.Blocked("a"); b {
			t.Fatalf("blocked after %d", i)
		}
		l.Fail("a")
		now = now.Add(time.Second)
	}
	wait, b := l.Blocked("a")
	if !b || wait != 5*time.Minute-5*time.Second {
		t.Fatalf("blocked=%v wait=%v", b, wait)
	}
	if _, b := l.Blocked("b"); b {
		t.Fatal("other client blocked")
	}
	l.Fail("b")
	if n := l.BlockedCount(); n != 1 {
		t.Fatalf("blocked count %d", n)
	}
	l.Reset("b")
	now = now.Add(wait)
	if _, b := l.Blocked("a"); b {
		t.Fatal("window should have slid")
	}
	l.Fail("a")
	l.Reset("a")
	if len(l.fails) != 0 {
		t.Fatal("reset failed")
	}
}

func TestSessionsListRevoke(t *testing.T) {
	s := NewSessions()
	a, _ := s.Create("admin", Meta{Client: "10.0.0.2", Agent: "Firefox\x1b[31m"})
	b, _ := s.Create("jana", Meta{Client: "10.0.0.3"})
	c, _ := s.Create("jana", Meta{})
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("list %+v", list)
	}
	for _, v := range list {
		if v.ID == "" || v.ID == a || v.ID == b || v.ID == c {
			t.Fatalf("id must be set and unrelated to the token: %+v", v)
		}
		if v.Username == "admin" && v.Agent != "Firefox[31m" {
			t.Fatalf("control characters kept: %q", v.Agent)
		}
	}
	if u, ok := s.Revoke(s.ID(b)); !ok || u != "jana" {
		t.Fatalf("revoke: %q %v", u, ok)
	}
	if _, _, ok := s.Lookup(b); ok {
		t.Fatal("revoked session accepted")
	}
	if _, ok := s.Revoke("nope"); ok {
		t.Fatal("unknown id revoked")
	}
	if n := s.RevokeOthers(a); n != 1 {
		t.Fatalf("revoke others ended %d", n)
	}
	if _, _, ok := s.Lookup(a); !ok {
		t.Fatal("own session ended")
	}
	if _, _, ok := s.Lookup(c); ok {
		t.Fatal("other session survived")
	}
}

func TestSessionsPersist(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	p := filepath.Join(t.TempDir(), "sessions.json")
	clock := func() time.Time { return now }
	s, err := OpenSessions(p, clock)
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := s.Create("admin", Meta{Client: "10.0.0.2"})
	old, _ := s.Create("jana", Meta{})
	gone, _ := s.Create("petr", Meta{})
	s.Delete(gone)
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("state file: %v %v", st, err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), keep) || strings.Contains(string(b), old) {
		t.Fatal("raw token written to the state file")
	}
	now = now.Add(20 * time.Minute)
	s.Lookup(keep)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	now = now.Add(15 * time.Minute) // jana idle 35 min, admin 15 min
	r, err := OpenSessions(p, clock)
	if err != nil {
		t.Fatal(err)
	}
	if u, _, ok := r.Lookup(keep); !ok || u != "admin" {
		t.Fatal("session lost across restart")
	}
	if _, _, ok := r.Lookup(old); ok {
		t.Fatal("expired session restored")
	}
	if _, _, ok := r.Lookup(gone); ok {
		t.Fatal("deleted session restored")
	}
	if l := r.List(); len(l) != 1 || l[0].Client != "10.0.0.2" {
		t.Fatalf("restored list %+v", l)
	}
}

func TestOpenSessionsCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sessions.json")
	os.WriteFile(p, []byte("{"), 0o600)
	s, err := OpenSessions(p, time.Now)
	if err == nil || s == nil || len(s.List()) != 0 {
		t.Fatalf("corrupt file: %v", err)
	}
}
