// Package tlscert manages the HTTPS certificate of the web service: first-start
// self-signed generation, upload of an own certificate and hot swapping.
package tlscert

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nas-revival/backend/internal/atomicfile"
)

// ValidityDays is the lifetime of generated certificates.
const ValidityDays = 825

// Error is a validation problem that should be shown to the user.
type Error struct{ Field, Message string }

func (e *Error) Error() string { return e.Field + ": " + e.Message }

// Info describes the active certificate (GET /settings/tls).
type Info struct {
	Subject     string   `json:"subject"`
	Issuer      string   `json:"issuer"`
	SelfSigned  bool     `json:"selfSigned"`
	DNSNames    []string `json:"dnsNames"`
	IPAddresses []string `json:"ipAddresses"`
	NotBefore   string   `json:"notBefore"`
	NotAfter    string   `json:"notAfter"`
	SHA256      string   `json:"sha256"`
	KeyType     string   `json:"keyType"`
}

type active struct {
	cert *tls.Certificate
	leaf *x509.Certificate
}

// Manager serves the current certificate and replaces it atomically.
type Manager struct {
	dir string
	// Overridable for tests.
	Now       func() time.Time
	Hostname  func() (string, error)
	Addresses func() ([]net.Addr, error)

	mu  sync.Mutex // serialises Install/Generate
	cur atomic.Pointer[active]
}

func newManager(dir string) *Manager {
	return &Manager{dir: dir, Now: time.Now, Hostname: os.Hostname, Addresses: net.InterfaceAddrs}
}

// New loads cert.pem/key.pem from dir or generates a self-signed pair on first start.
func New(dir string) (*Manager, error) {
	m := newManager(dir)
	a, err := m.load()
	if err == nil {
		m.cur.Store(a)
		return m, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := m.Generate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) paths() (string, string) {
	return filepath.Join(m.dir, "cert.pem"), filepath.Join(m.dir, "key.pem")
}

func (m *Manager) load() (*active, error) {
	cp, kp := m.paths()
	c, err := tls.LoadX509KeyPair(cp, kp)
	if err != nil {
		return nil, err
	}
	return toActive(c)
}

func toActive(c tls.Certificate) (*active, error) {
	leaf := c.Leaf
	if leaf == nil {
		var err error
		if leaf, err = x509.ParseCertificate(c.Certificate[0]); err != nil {
			return nil, err
		}
	}
	return &active{cert: &c, leaf: leaf}, nil
}

// GetCertificate is for tls.Config.GetCertificate.
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.cur.Load().cert, nil
}

// Info describes the certificate used for new connections.
func (m *Manager) Info() Info {
	l := m.cur.Load().leaf
	sum := sha256.Sum256(l.Raw)
	hex := make([]string, len(sum))
	for i, b := range sum {
		hex[i] = fmt.Sprintf("%02X", b)
	}
	in := Info{
		Subject: l.Subject.String(), Issuer: l.Issuer.String(),
		SelfSigned: bytes.Equal(l.RawIssuer, l.RawSubject) &&
			l.CheckSignature(l.SignatureAlgorithm, l.RawTBSCertificate, l.Signature) == nil,
		DNSNames: append([]string{}, l.DNSNames...), IPAddresses: []string{},
		NotBefore: l.NotBefore.UTC().Format(time.RFC3339), NotAfter: l.NotAfter.UTC().Format(time.RFC3339),
		SHA256: strings.Join(hex, ":"), KeyType: keyType(l.PublicKey),
	}
	for _, ip := range l.IPAddresses {
		in.IPAddresses = append(in.IPAddresses, ip.String())
	}
	return in
}

func keyType(pub any) string {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return "unknown"
}

func bad(field, msg string) error { return &Error{Field: field, Message: msg} }

// Install validates and activates an uploaded certificate (leaf first, optional chain).
func (m *Manager) Install(certPEM, keyPEM []byte) error {
	var chain int
	for rest := certPEM; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			return bad("certificate", "Certifikát smí obsahovat jen bloky CERTIFICATE.")
		}
		chain++
	}
	if chain == 0 {
		return bad("certificate", "Certifikát není ve formátu PEM.")
	}
	if chain > 10 {
		return bad("certificate", "Řetěz certifikátů je příliš dlouhý.")
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		if strings.Contains(err.Error(), "does not match") {
			return bad("privateKey", "Soukromý klíč neodpovídá certifikátu.")
		}
		if strings.Contains(err.Error(), "private key") || strings.Contains(err.Error(), "PEM") {
			return bad("privateKey", "Soukromý klíč nelze přečíst (nešifrovaný PEM: PKCS#8, PKCS#1 nebo EC).")
		}
		return bad("certificate", "Certifikát nelze přečíst.")
	}
	a, err := toActive(c)
	if err != nil {
		return bad("certificate", "Certifikát nelze přečíst.")
	}
	now := m.Now()
	if now.Before(a.leaf.NotBefore) || now.After(a.leaf.NotAfter) {
		return bad("certificate", "Certifikát právě neplatí (mimo období platnosti).")
	}
	switch k := a.leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return bad("privateKey", "Klíč RSA musí mít alespoň 2048 bitů.")
		}
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() && k.Curve != elliptic.P384() {
			return bad("privateKey", "Klíč ECDSA musí být P-256 nebo P-384.")
		}
	case ed25519.PublicKey:
	default:
		return bad("privateKey", "Nepodporovaný typ klíče.")
	}
	var certOut []byte
	for _, der := range c.Certificate {
		certOut = append(certOut, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		return bad("privateKey", "Soukromý klíč nelze uložit.")
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return m.store(certOut, keyOut, a)
}

func (m *Manager) store(certPEM, keyPEM []byte, a *active) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	cp, kp := m.paths()
	if err := atomicfile.Write(kp, keyPEM, 0o600); err != nil {
		return err
	}
	if err := atomicfile.Write(cp, certPEM, 0o644); err != nil {
		return err
	}
	m.cur.Store(a)
	return nil
}

// Generate creates and activates a new self-signed ECDSA P-256 certificate.
func (m *Manager) Generate() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	host, _ := m.Hostname()
	if host == "" {
		host = "nas"
	}
	now := m.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(0, 0, ValidityDays),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host, host + ".local", host + ".home", "localhost"},
	}
	if addrs, err := m.Addresses(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, n.IP.To4())
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	a, err := toActive(c)
	if err != nil {
		return err
	}
	return m.store(certPEM, keyPEM, a)
}
