package helper

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"nas-revival/backend/internal/sshd"
)

func newPubLine(t *testing.T, comment string) (string, string) {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " " + comment, ssh.FingerprintSHA256(k)
}

func errCode(err error) string {
	var he *Error
	if errors.As(err, &he) {
		return he.Code + "/" + he.Field
	}
	return "?"
}

func TestSshKeysAddListRemove(t *testing.T) {
	m, _ := newSshFixture(t)
	ctx := context.Background()
	l, err := m.KeysList(ctx)
	if err != nil || len(l.Users) != 1 || l.Users[0].Name != "jana" || len(l.Users[0].Keys) != 0 || l.Users[0].Sftp {
		t.Fatalf("initial list %+v %v", l, err)
	}
	a, fpA := newPubLine(t, "jana@notebook")
	b, fpB := newPubLine(t, "jana@phone")
	if _, err := m.KeyAdd(ctx, "jana", a, "admin"); err != nil {
		t.Fatal(err)
	}
	l, err = m.KeyAdd(ctx, "jana", "  "+b+"\n", "admin")
	if err != nil || len(l.Users[0].Keys) != 2 || l.Users[0].Keys[1].Fingerprint != fpB {
		t.Fatalf("after add %+v %v", l, err)
	}
	path := filepath.Join(m.KeysDir, "jana")
	got, _ := os.ReadFile(path)
	if string(got) != a+"\n"+b+"\n" {
		t.Fatalf("keys file %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("keys file mode %v", fi.Mode())
	}
	if fi, _ := os.Stat(m.KeysDir); fi.Mode().Perm() != 0o755 {
		t.Errorf("keys dir mode %v", fi.Mode())
	}
	if _, err := m.KeyAdd(ctx, "jana", a, "admin"); errCode(err) != "invalid/key" {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := m.KeyRemove(ctx, "jana", "SHA256:nope", "admin"); errCode(err) != "not_found/" {
		t.Errorf("unknown fingerprint: %v", err)
	}
	l, err = m.KeyRemove(ctx, "jana", fpA, "admin")
	if err != nil || len(l.Users[0].Keys) != 1 || l.Users[0].Keys[0].Fingerprint != fpB {
		t.Fatalf("after remove %+v %v", l, err)
	}
	if _, err := m.KeyRemove(ctx, "jana", fpB, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("empty keys file not removed: %v", err)
	}
}

func TestSshKeysOnlyNASUsers(t *testing.T) {
	m, _ := newSshFixture(t)
	ctx := context.Background()
	line, _ := newPubLine(t, "x")
	for _, name := range []string{"root", "debian", "nas-revival", "nobody", "../etc", ""} {
		if _, err := m.KeyAdd(ctx, name, line, "admin"); errCode(err) != "not_found/" {
			t.Errorf("%q: %v", name, err)
		}
	}
	if _, err := m.KeyAdd(ctx, "jana", "ssh-dss AAAA", "admin"); errCode(err) != "invalid/key" {
		t.Errorf("bad key: %v", err)
	}
	if entries, _ := os.ReadDir(m.KeysDir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

func TestSshKeysLimitAndInvalidLines(t *testing.T) {
	m, _ := newSshFixture(t)
	ctx := context.Background()
	os.MkdirAll(m.KeysDir, 0o755)
	first, _ := newPubLine(t, "first")
	os.WriteFile(filepath.Join(m.KeysDir, "jana"), []byte("command=\"x\" "+first+"\n"+first+"\n"), 0o644)
	l, _ := m.KeysList(ctx)
	if l.Users[0].Invalid != 1 || len(l.Users[0].Keys) != 1 {
		t.Fatalf("list %+v", l)
	}
	for i := 1; i < sshd.MaxKeysPerUser; i++ {
		k, _ := newPubLine(t, "k")
		if _, err := m.KeyAdd(ctx, "jana", k, "admin"); err != nil {
			t.Fatal(i, err)
		}
	}
	k, _ := newPubLine(t, "one too many")
	if _, err := m.KeyAdd(ctx, "jana", k, "admin"); errCode(err) != "invalid/key" {
		t.Errorf("limit: %v", err)
	}
	// The hand-edited line with options is gone after the first write.
	b, _ := os.ReadFile(filepath.Join(m.KeysDir, "jana"))
	if strings.Contains(string(b), "command=") {
		t.Errorf("invalid line kept: %q", b)
	}
}

func TestSshKeysSftpFlag(t *testing.T) {
	m, _ := newSshFixture(t)
	ctx := context.Background()
	for groups, want := range map[string]bool{"nas-users": true, "rodina": true, "": false} {
		s := sshd.Default()
		if groups != "" {
			s.SftpGroups = []string{groups}
		}
		if err := sshd.Save(m.Path, s); err != nil {
			t.Fatal(err)
		}
		if l, _ := m.KeysList(ctx); l.Users[0].Sftp != want {
			t.Errorf("groups %q: sftp %v", groups, l.Users[0].Sftp)
		}
	}
}

func TestRemoveUserKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "jana"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "keep"), []byte("x\n"), 0o644)
	if err := RemoveUserKeys(dir, "jana"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveUserKeys(dir, "jana"); err != nil {
		t.Errorf("missing file: %v", err)
	}
	RemoveUserKeys(dir, "../"+filepath.Base(dir)+"/keep")
	RemoveUserKeys("", "keep")
	if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
		t.Errorf("wrong file removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jana")); !os.IsNotExist(err) {
		t.Errorf("jana kept: %v", err)
	}
}
