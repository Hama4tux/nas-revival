package sshd

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// authorizedLine returns the .pub line of a freshly generated key.
func authorizedLine(t *testing.T, pub any, comment string) (string, ssh.PublicKey) {
	t.Helper()
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " " + comment, k
}

func TestParseKeyAccepted(t *testing.T) {
	edPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	for _, c := range []struct {
		pub  any
		typ  string
		bits int
	}{{edPub, "ssh-ed25519", 0}, {&ec.PublicKey, "ecdsa-sha2-nistp256", 0}, {&rk.PublicKey, "ssh-rsa", 2048}} {
		line, pk := authorizedLine(t, c.pub, "jana@notebook")
		k, norm, err := ParseKey("\uFEFF  " + line + "\n")
		if err != nil {
			t.Fatalf("%s: %v", c.typ, err)
		}
		if k.Type != c.typ || k.Bits != c.bits || k.Comment != "jana@notebook" || k.Fingerprint != ssh.FingerprintSHA256(pk) {
			t.Errorf("%s: %+v", c.typ, k)
		}
		if norm != line {
			t.Errorf("%s: normalised %q, want %q", c.typ, norm, line)
		}
	}
	// No comment: the line ends with the key.
	line, _ := authorizedLine(t, edPub, "")
	if _, norm, err := ParseKey(line); err != nil || strings.HasSuffix(norm, " ") {
		t.Errorf("no comment: %q %v", norm, err)
	}
}

func TestParseKeyRefused(t *testing.T) {
	edPub, _, _ := ed25519.GenerateKey(rand.Reader)
	good, _ := authorizedLine(t, edPub, "x")
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	weakLine, _ := authorizedLine(t, &weak.PublicKey, "weak")
	for name, in := range map[string]string{
		"empty":        "  ",
		"private key":  "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----",
		"putty":        "---- BEGIN SSH2 PUBLIC KEY ----\nComment: \"x\"\nAAAAC3Nz\n---- END SSH2 PUBLIC KEY ----",
		"two keys":     good + "\n" + good,
		"options":      `command="/bin/sh" ` + good,
		"from option":  `from="10.0.0.1" ` + good,
		"garbage":      "ssh-ed25519 notbase64!!",
		"rsa 1024":     weakLine,
		"long comment": good + strings.Repeat("a", 101),
		"control":      good + "\x07",
		"too long":     good + strings.Repeat("a", MaxKeyLen),
	} {
		if _, _, err := ParseKey(in); err == nil {
			t.Errorf("%s: accepted", name)
		} else if fe, ok := err.(*FieldError); !ok || fe.Field != "key" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseKeysFile(t *testing.T) {
	a, _, _ := ed25519.GenerateKey(rand.Reader)
	b, _, _ := ed25519.GenerateKey(rand.Reader)
	la, _ := authorizedLine(t, a, "a")
	lb, _ := authorizedLine(t, b, "b")
	keys, lines, invalid := ParseKeysFile([]byte("# comment\n" + la + "\n\nno-pty " + lb + "\nbroken\n" + lb + "\n"))
	if len(keys) != 2 || invalid != 2 || lines[0] != la || lines[1] != lb || keys[1].Comment != "b" {
		t.Fatalf("keys %+v lines %q invalid %d", keys, lines, invalid)
	}
}
