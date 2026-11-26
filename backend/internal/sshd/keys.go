package sshd

import (
	"crypto/rsa"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

// Limits for the public keys of NAS users (SSH-02).
const (
	MaxKeysPerUser = 10
	MaxKeyLen      = 16 << 10 // one authorized_keys line; RSA 16384 is about 2.8 kB
	MaxCommentLen  = 100
	MinRSABits     = 2048
)

// Key is one public key of a NAS user as the UI shows it.
type Key struct {
	Type        string `json:"type"`
	Bits        int    `json:"bits,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
}

// allowedTypes are the key types modern OpenSSH accepts by default. DSA is
// disabled in OpenSSH and certificates need a CA setup the NAS does not have.
var allowedTypes = map[string]bool{
	ssh.KeyAlgoED25519:    true,
	ssh.KeyAlgoECDSA256:   true,
	ssh.KeyAlgoECDSA384:   true,
	ssh.KeyAlgoECDSA521:   true,
	ssh.KeyAlgoSKED25519:  true,
	ssh.KeyAlgoSKECDSA256: true,
	ssh.KeyAlgoRSA:        true,
}

func keyErr(msg string) error { return &FieldError{"key", msg} }

// ParseKey checks one pasted public key (an id_*.pub line) and returns it
// with its authorized_keys line rebuilt as "type base64 comment". Options
// (command=, from=…) are refused, so a key never changes what sshd allows.
func ParseKey(text string) (Key, string, error) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	if text == "" {
		return Key{}, "", keyErr("Vložte veřejný klíč.")
	}
	if len(text) > MaxKeyLen {
		return Key{}, "", keyErr("Klíč je příliš dlouhý.")
	}
	if strings.Contains(text, "PRIVATE KEY") {
		return Key{}, "", keyErr("Toto je soukromý klíč – ten nikdy nikam nevkládejte. Vložte obsah souboru .pub.")
	}
	if strings.Contains(text, "BEGIN SSH2 PUBLIC KEY") {
		return Key{}, "", keyErr("Toto je klíč ve formátu PuTTY. V PuTTYgen zkopírujte text z pole „Public key for pasting into OpenSSH authorized_keys file“.")
	}
	if strings.ContainsAny(text, "\r\n") {
		return Key{}, "", keyErr("Vložte jen jeden klíč na jednom řádku.")
	}
	pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil {
		return Key{}, "", keyErr("Nejde o platný veřejný klíč SSH (očekává se např. „ssh-ed25519 AAAA… jméno“).")
	}
	if len(options) > 0 {
		return Key{}, "", keyErr("Klíč nesmí obsahovat volby (command=, from= ap.); vložte jen typ, klíč a popis.")
	}
	if !allowedTypes[pub.Type()] {
		return Key{}, "", keyErr("Typ klíče " + pub.Type() + " není podporovaný; použijte ed25519, ECDSA nebo RSA.")
	}
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > MaxCommentLen || !utf8.ValidString(comment) {
		return Key{}, "", keyErr("Popis klíče může mít nejvýše 100 znaků.")
	}
	for _, r := range comment {
		if unicode.IsControl(r) {
			return Key{}, "", keyErr("Popis klíče nesmí obsahovat řídicí znaky.")
		}
	}
	k := Key{Type: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Comment: comment}
	if cp, ok := pub.(ssh.CryptoPublicKey); ok {
		if r, ok := cp.CryptoPublicKey().(*rsa.PublicKey); ok {
			k.Bits = r.N.BitLen()
			if k.Bits < MinRSABits {
				return Key{}, "", keyErr("Klíč RSA musí mít aspoň 2048 bitů.")
			}
		}
	}
	line := strings.TrimSpace(strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(pub)), "\n") + " " + comment)
	return k, line, nil
}

// ParseKeysFile reads a keys file the helper wrote. Lines it cannot parse
// (edited by hand) are counted as invalid and dropped on the next write.
func ParseKeysFile(b []byte) (keys []Key, lines []string, invalid int) {
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, _, err := ParseKey(l)
		if err != nil {
			invalid++
			continue
		}
		keys = append(keys, k)
		lines = append(lines, l)
	}
	return keys, lines, invalid
}
