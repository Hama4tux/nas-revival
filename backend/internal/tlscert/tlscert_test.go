package tlscert

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkCert(t *testing.T, priv crypto.Signer, pub crypto.PublicKey, nb, na time.Time, ca *x509.Certificate, caKey crypto.Signer) (certPEM []byte, keyPEM []byte, c *x509.Certificate) {
	t.Helper()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "t"},
		NotBefore: nb, NotAfter: na, DNSNames: []string{"t"}, BasicConstraintsValid: true, IsCA: ca == nil}
	parent, pk := tmpl, priv
	if ca != nil {
		parent, pk = ca, caKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, pk)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalPKCS8PrivateKey(priv)
	c, _ = x509.ParseCertificate(der)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd}), c
}

func newMgr(t *testing.T) *Manager {
	t.Helper()
	m, err := New(filepath.Join(t.TempDir(), "tls"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFirstStartAndReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	m := newManager(dir)
	m.Hostname = func() (string, error) { return "nas", nil }
	m.Addresses = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.5"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("169.254.1.1"), Mask: net.CIDRMask(16, 32)},
			&net.IPNet{IP: net.ParseIP("fd00::1"), Mask: net.CIDRMask(64, 128)}}, nil
	}
	if err := m.Generate(); err != nil {
		t.Fatal(err)
	}
	in := m.Info()
	if !in.SelfSigned || in.KeyType != "ECDSA P-256" || len(in.IPAddresses) != 1 || in.IPAddresses[0] != "192.168.1.5" ||
		in.DNSNames[0] != "nas" || in.DNSNames[1] != "nas.local" || in.DNSNames[2] != "nas.home" {
		t.Fatalf("%+v", in)
	}
	if !strings.Contains(in.SHA256, ":") || len(in.SHA256) != 95 {
		t.Fatal(in.SHA256)
	}
	l := m.cur.Load().leaf
	if d := l.NotAfter.Sub(l.NotBefore); d < 825*24*time.Hour || d > 826*24*time.Hour+2*time.Hour {
		t.Fatal(d)
	}
	st, _ := os.Stat(filepath.Join(dir, "key.pem"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	m2, err := New(dir) // loads the same pair
	if err != nil || m2.Info().SHA256 != in.SHA256 {
		t.Fatalf("%v", err)
	}
	if c, err := m2.GetCertificate(nil); err != nil || c == nil {
		t.Fatal(err)
	}
	m.Generate()
	if m.Info().SHA256 == in.SHA256 {
		t.Fatal("generate did not replace")
	}
}

func TestInstall(t *testing.T) {
	now := time.Now()
	ec256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ec384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	ec521, _ := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	_, edpriv, _ := ed25519.GenerateKey(rand.Reader)
	rsa2048, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsa1024, _ := rsa.GenerateKey(rand.Reader, 1024)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ok := func(k crypto.Signer) ([]byte, []byte) {
		c, kp, _ := mkCert(t, k, k.Public(), now.Add(-time.Hour), now.Add(time.Hour), nil, nil)
		return c, kp
	}
	cases := []struct {
		name  string
		cert  func() ([]byte, []byte)
		field string
	}{
		{"ec256", func() ([]byte, []byte) { return ok(ec256) }, ""},
		{"ec384", func() ([]byte, []byte) { return ok(ec384) }, ""},
		{"ed25519", func() ([]byte, []byte) { return ok(edpriv) }, ""},
		{"rsa2048", func() ([]byte, []byte) { return ok(rsa2048) }, ""},
		{"ec521", func() ([]byte, []byte) { return ok(ec521) }, "privateKey"},
		{"rsa1024", func() ([]byte, []byte) { return ok(rsa1024) }, "privateKey"},
		{"expired", func() ([]byte, []byte) {
			c, k, _ := mkCert(t, ec256, ec256.Public(), now.Add(-48*time.Hour), now.Add(-time.Hour), nil, nil)
			return c, k
		}, "certificate"},
		{"not yet valid", func() ([]byte, []byte) {
			c, k, _ := mkCert(t, ec256, ec256.Public(), now.Add(time.Hour), now.Add(2*time.Hour), nil, nil)
			return c, k
		}, "certificate"},
		{"mismatch", func() ([]byte, []byte) {
			c, _ := ok(ec256)
			_, k := ok(other)
			return c, k
		}, "privateKey"},
		{"garbage cert", func() ([]byte, []byte) { _, k := ok(ec256); return []byte("nope"), k }, "certificate"},
		{"garbage key", func() ([]byte, []byte) { c, _ := ok(ec256); return c, []byte("nope") }, "privateKey"},
		{"key as cert", func() ([]byte, []byte) { _, k := ok(ec256); return k, k }, "certificate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMgr(t)
			before := m.Info().SHA256
			c, k := tc.cert()
			err := m.Install(c, k)
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				if m.Info().SHA256 == before || m.Info().SelfSigned != true {
					t.Fatal("not active")
				}
				m2, err := New(m.dir)
				if err != nil || m2.Info().SHA256 != m.Info().SHA256 {
					t.Fatalf("not persisted: %v", err)
				}
				return
			}
			e, isE := err.(*Error)
			if !isE || e.Field != tc.field {
				t.Fatalf("got %v, want field %s", err, tc.field)
			}
			if m.Info().SHA256 != before {
				t.Fatal("certificate replaced despite error")
			}
		})
	}
}

func TestInstallChain(t *testing.T) {
	now := time.Now()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caPEM, _, ca := mkCert(t, caKey, caKey.Public(), now.Add(-time.Hour), now.Add(24*time.Hour), nil, nil)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafPEM, keyPEM, _ := mkCert(t, leafKey, leafKey.Public(), now.Add(-time.Hour), now.Add(time.Hour), ca, caKey)
	m := newMgr(t)
	if err := m.Install(append(leafPEM, caPEM...), keyPEM); err != nil {
		t.Fatal(err)
	}
	in := m.Info()
	if in.SelfSigned || !strings.Contains(in.Issuer, "CN=t") {
		t.Fatalf("%+v", in)
	}
	if c, _ := m.GetCertificate(nil); len(c.Certificate) != 2 {
		t.Fatal("chain lost")
	}
	// chain first (CA before leaf): key does not match the first cert
	if err := newMgr(t).Install(append(caPEM, leafPEM...), keyPEM); err == nil {
		t.Fatal("wrong order accepted")
	}
}
