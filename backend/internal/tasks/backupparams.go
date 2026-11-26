package tasks

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limits of a backup-pull task.
const (
	MaxBackupPaths    = 8
	MaxBackupExcludes = 16
	MaxBackupPathLen  = 128
	MaxBwlimitKiB     = 1000000
	MaxKeepDays       = 90
	MaxFreshHours     = 720
	DefaultSSHPort    = 22
)

var (
	hostRe    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))*$`)
	userRe    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	relPathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	excludeRe = regexp.MustCompile(`^[A-Za-z0-9._*?/\[\]-]+$`)
)

// FreshParams says which file proves that the export on the server is current.
type FreshParams struct {
	File        string `json:"file"`
	MaxAgeHours int    `json:"maxAgeHours"`
}

// BackupParams are the parameters of a backup-pull task.
type BackupParams struct {
	Host       string       `json:"host"`
	Port       int          `json:"port"`
	User       string       `json:"user"`
	Paths      []string     `json:"paths"`
	KeepDays   int          `json:"keepDays"`
	Fresh      *FreshParams `json:"fresh,omitempty"`
	BwlimitKiB int          `json:"bwlimitKiB"`
	Excludes   []string     `json:"excludes"`
}

// ValidHost accepts an IPv4/IPv6 literal (no zone) or a DNS name that cannot
// be mistaken for an option.
func ValidHost(h string) bool {
	if h == "" || len(h) > 253 || strings.HasPrefix(h, "-") {
		return false
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Zone() == ""
	}
	return hostRe.MatchString(h)
}

// ValidPort reports 1–65535.
func ValidPort(p int) bool { return p >= 1 && p <= 65535 }

// ValidRemoteUser checks the user name on the server.
func ValidRemoteUser(u string) bool { return userRe.MatchString(u) }

// ValidRelPath checks a path relative to the server's rrsync root.
func ValidRelPath(p string) bool {
	if len(p) > MaxBackupPathLen || !relPathRe.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// validateBackup checks and normalises the parameters (port 0 becomes 22,
// nil lists become empty).
func validateBackup(p string, in *BackupParams) (*BackupParams, error) {
	if in == nil {
		return nil, bad(p[:len(p)-1], "Chybí parametry zálohy.")
	}
	b := *in
	if !ValidHost(b.Host) {
		return nil, bad(p+"host", "Neplatný název nebo adresa serveru.")
	}
	if b.Port == 0 {
		b.Port = DefaultSSHPort
	}
	if !ValidPort(b.Port) {
		return nil, bad(p+"port", "Port musí být 1 až 65535.")
	}
	if !ValidRemoteUser(b.User) {
		return nil, bad(p+"user", "Neplatné jméno uživatele na serveru.")
	}
	if n := len(b.Paths); n < 1 || n > MaxBackupPaths {
		return nil, bad(p+"paths", fmt.Sprintf("Zadejte 1 až %d složek.", MaxBackupPaths))
	}
	seen := map[string]bool{}
	for _, x := range b.Paths {
		if !ValidRelPath(x) {
			return nil, bad(p+"paths", "Neplatná cesta ke složce (jen písmena, číslice, . _ - a /, bez „..“).")
		}
		if seen[x] {
			return nil, bad(p+"paths", "Cesta se opakuje.")
		}
		seen[x] = true
	}
	if b.KeepDays < 1 || b.KeepDays > MaxKeepDays {
		return nil, bad(p+"keepDays", fmt.Sprintf("Verze lze držet 1 až %d dní.", MaxKeepDays))
	}
	if b.BwlimitKiB < 0 || b.BwlimitKiB > MaxBwlimitKiB {
		return nil, bad(p+"bwlimitKiB", fmt.Sprintf("Limit rychlosti je 0 až %d KiB/s.", MaxBwlimitKiB))
	}
	if b.Fresh != nil {
		f := *b.Fresh
		if !ValidRelPath(f.File) {
			return nil, bad(p+"fresh.file", "Neplatná cesta k souboru.")
		}
		under := false
		for _, x := range b.Paths {
			if strings.HasPrefix(f.File, x+"/") {
				under = true
			}
		}
		if !under {
			return nil, bad(p+"fresh.file", "Soubor musí ležet pod jednou ze složek zálohy.")
		}
		if f.MaxAgeHours < 1 || f.MaxAgeHours > MaxFreshHours {
			return nil, bad(p+"fresh.maxAgeHours", fmt.Sprintf("Stáří souboru je 1 až %d hodin.", MaxFreshHours))
		}
		b.Fresh = &f
	}
	if len(b.Excludes) > MaxBackupExcludes {
		return nil, bad(p+"excludes", fmt.Sprintf("Nejvýše %d vyloučení.", MaxBackupExcludes))
	}
	for _, e := range b.Excludes {
		if n := utf8.RuneCountInString(e); n < 1 || n > 100 || strings.HasPrefix(e, "-") || !excludeRe.MatchString(e) {
			return nil, bad(p+"excludes", "Neplatný vzor vyloučení.")
		}
	}
	b.Paths = append([]string{}, b.Paths...)
	b.Excludes = append([]string{}, b.Excludes...)
	return &b, nil
}
