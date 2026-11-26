// Package shares holds the model of the shared folders (W2.3): validation,
// effective access, POSIX ACL encoding, the Samba fragment and the recycle bin
// cleanup. It does no privileged work on its own; the helper drives it.
package shares

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/atomicfile"
)

// DefaultConfigPath is the share configuration (0644, root).
const DefaultConfigPath = "/etc/nas-revival/shares.json"

// VolumeRoot is the directory whose mountpoints may carry shares; tests
// point it at a temporary directory.
var VolumeRoot = "/srv/"

// Limits.
const (
	MaxDescription = 64
	MaxPermissions = 64
	MaxShares      = 64
	DefaultDays    = 30
	MaxDays        = 365
)

// Access levels and principal kinds.
const (
	AccessRW   = "rw"
	AccessRO   = "ro"
	AccessNone = "none"

	KindUser  = "user"
	KindGroup = "group"
)

type Permission struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

type Recycle struct {
	Enabled bool `json:"enabled"`
	Days    int  `json:"days"`
}

type Share struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Volume      string       `json:"volume"`
	Folder      string       `json:"folder"`
	ReadOnly    bool         `json:"readOnly"`
	Hidden      bool         `json:"hidden"`
	Recycle     Recycle      `json:"recycle"`
	Permissions []Permission `json:"permissions"`
}

// Config is the content of shares.json.
type Config struct {
	Version int     `json:"version"`
	Shares  []Share `json:"shares"`
}

// Path is the folder of the share: volume plus one validated component.
func (s Share) Path() string { return s.Volume + "/" + s.Folder }

// FieldError is a validation failure of one request field.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func bad(field, msg string) error { return &FieldError{field, msg} }

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
	principalR = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	reserved   = map[string]bool{"global": true, "homes": true, "printers": true, "print$": true,
		"ipc$": true, "netlogon": true, "profiles": true}
)

// ValidName checks a share name or folder name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// BackupShareName is the fixed name of the optional read-only share of the
// backup versions (W4.3); with "apps" and "versions" (the backup data
// directories on every volume) it is not available for ordinary shares.
const BackupShareName = "zalohy"

var backupReserved = map[string]bool{"apps": true, "versions": true, BackupShareName: true}

// ReservedForBackup reports names the backups use (case-insensitive).
func ReservedForBackup(s string) bool { return backupReserved[strings.ToLower(s)] }

// RenderBackupShare is the fixed Samba section of the backup versions. It is
// read-only on purpose: the versions are hardlinks, a write would change
// every version that shares the file.
func RenderBackupShare(path string) string {
	var b strings.Builder
	b.WriteString("\n[" + BackupShareName + "]\n")
	for _, kv := range [][2]string{
		{"comment", "Zálohy ze serverů (jen ke čtení)"},
		{"path", path},
		{"read only", "yes"},
		{"force user", "nas-backup"},
		{"force group", "nas-backup"},
		{"valid users", "@nas-admin"},
		{"browseable", "yes"},
		{"guest ok", "no"},
	} {
		b.WriteString("   " + kv[0] + " = " + kv[1] + "\n")
	}
	return b.String()
}

// ValidateRequest is ValidateShape plus the names reserved for backups. It
// runs on create and update only, so an older shares.json that already holds
// such a name still loads.
func ValidateRequest(s Share) (Share, error) {
	n, err := ValidateShape(s)
	if err != nil {
		return n, err
	}
	if ReservedForBackup(n.Name) {
		return n, bad("name", "Název je vyhrazen pro zálohy.")
	}
	if ReservedForBackup(n.Folder) {
		return n, bad("folder", "Název je vyhrazen pro zálohy.")
	}
	return n, nil
}

// Reserved reports names Samba gives a special meaning (case-insensitive).
func Reserved(s string) bool { return reserved[strings.ToLower(s)] }

// ValidateShape checks everything that needs no system state and returns the
// normalised share (trimmed description, default folder and days). The web
// process runs it before the helper is asked; the helper adds the checks that
// need the account database and the volume list.
func ValidateShape(s Share) (Share, error) {
	if !ValidName(s.Name) {
		return s, bad("name", "Název: písmena bez diakritiky, číslice, _ nebo -, začíná písmenem nebo číslicí, nejvýše 32 znaků.")
	}
	if Reserved(s.Name) {
		return s, bad("name", "Tento název je vyhrazený.")
	}
	s.Description = strings.TrimSpace(s.Description)
	if utf8.RuneCountInString(s.Description) > MaxDescription || !utf8.ValidString(s.Description) {
		return s, bad("description", "Popis může mít nejvýše 64 znaků.")
	}
	for _, r := range s.Description {
		if unicode.IsControl(r) || r == '\\' {
			return s, bad("description", "Popis nesmí obsahovat řídicí znaky ani zpětné lomítko.")
		}
	}
	if s.Folder == "" {
		s.Folder = s.Name
	}
	if !ValidName(s.Folder) {
		return s, bad("folder", "Složka: písmena bez diakritiky, číslice, _ nebo -, nejvýše 32 znaků.")
	}
	if !validVolumePath(s.Volume) {
		return s, bad("volume", "Neplatný svazek.")
	}
	switch {
	case s.Recycle.Enabled && s.Recycle.Days == 0:
		s.Recycle.Days = DefaultDays
	case s.Recycle.Enabled && (s.Recycle.Days < 1 || s.Recycle.Days > MaxDays):
		return s, bad("recycle.days", "Koš lze uchovávat 1 až 365 dní.")
	case !s.Recycle.Enabled && (s.Recycle.Days < 1 || s.Recycle.Days > MaxDays):
		s.Recycle.Days = 0
	}
	if s.Permissions == nil {
		s.Permissions = []Permission{}
	}
	if len(s.Permissions) > MaxPermissions {
		return s, bad("permissions", "Příliš mnoho oprávnění (nejvýše 64).")
	}
	seen := map[string]bool{}
	for _, p := range s.Permissions {
		if p.Kind != KindUser && p.Kind != KindGroup {
			return s, bad("permissions", "Neznámý druh oprávnění.")
		}
		if !principalR.MatchString(p.Name) {
			return s, bad("permissions", "Neplatné jméno uživatele nebo skupiny.")
		}
		switch {
		case p.Access == AccessRW || p.Access == AccessRO:
		case p.Access == AccessNone && p.Kind == KindUser:
		case p.Access == AccessNone:
			return s, bad("permissions", "Skupině nelze odepřít přístup; nastavte „bez přístupu“ jednotlivým uživatelům.")
		default:
			return s, bad("permissions", "Neznámá úroveň přístupu.")
		}
		k := p.Kind + ":" + p.Name
		if seen[k] {
			return s, bad("permissions", "Oprávnění pro "+p.Name+" je uvedené dvakrát.")
		}
		seen[k] = true
	}
	return s, nil
}

func validVolumePath(v string) bool {
	return strings.HasPrefix(v, VolumeRoot) && filepath.Clean(v) == v && !strings.ContainsAny(v, "\x00\n\r\t ")
}

// CheckPrincipals verifies that every permission names a NAS user or an
// assignable group (or nas-users).
func CheckPrincipals(s Share, db accounts.DB) error {
	for _, p := range s.Permissions {
		switch p.Kind {
		case KindUser:
			if !db.IsNASUser(p.Name) {
				return bad("permissions", "Uživatel "+p.Name+" neexistuje.")
			}
		case KindGroup:
			_, nfs := db.Group(accounts.NfsGroup)
			if p.Name != accounts.UsersGroup && !(p.Name == accounts.NfsGroup && nfs) && !db.IsEditableGroup(p.Name) {
				return bad("permissions", "Skupina "+p.Name+" neexistuje nebo ji nelze použít.")
			}
		}
	}
	return nil
}

// UserAccess is the effective access of one NAS user (the permissions matrix).
type UserAccess struct {
	User   string `json:"user"`
	Access string `json:"access"`
	// Via is "user", "group:<name>" or "".
	Via string `json:"via"`
}

func rank(a string) int {
	switch a {
	case AccessRW:
		return 2
	case AccessRO:
		return 1
	}
	return 0
}

func inGroup(db accounts.DB, user, group string) bool {
	if group == accounts.UsersGroup {
		return db.IsNASUser(user)
	}
	g, ok := db.Group(group)
	if !ok {
		return false
	}
	for _, m := range g.Members {
		if m == user {
			return true
		}
	}
	return false
}

// Effective computes the access of every NAS user: a user entry wins, else the
// best matching group entry, else none; a read-only share caps rw to ro.
func Effective(s Share, db accounts.DB) []UserAccess {
	out := []UserAccess{}
	for _, u := range db.NASUsers() {
		ua := UserAccess{User: u.Name, Access: AccessNone}
		found := false
		for _, p := range s.Permissions {
			if p.Kind == KindUser && p.Name == u.Name {
				ua.Access, ua.Via, found = p.Access, "user", true
				break
			}
		}
		if !found {
			for _, p := range s.Permissions {
				if p.Kind == KindGroup && rank(p.Access) > rank(ua.Access) && inGroup(db, u.Name, p.Name) {
					ua.Access, ua.Via = p.Access, "group:"+p.Name
				}
			}
		}
		if s.ReadOnly && ua.Access == AccessRW {
			ua.Access = AccessRO
		}
		if ua.Access == AccessNone && ua.Via == "" {
			ua.Via = ""
		}
		out = append(out, ua)
	}
	return out
}

// Conflicts lists user entries that are lower than a grant the user would get
// from a group (warnings, not errors).
func Conflicts(s Share, db accounts.DB) []string {
	out := []string{}
	for _, p := range s.Permissions {
		if p.Kind != KindUser || !db.IsNASUser(p.Name) {
			continue
		}
		for _, g := range s.Permissions {
			if g.Kind == KindGroup && rank(g.Access) > rank(p.Access) && inGroup(db, p.Name, g.Name) {
				out = append(out, fmt.Sprintf("Uživatel %s: %s přepisuje %s ze skupiny %s.", p.Name, p.Access, g.Access, g.Name))
			}
		}
	}
	return out
}

// Load reads shares.json; a missing file is an empty configuration.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Version: 1, Shares: []Share{}}, nil
	}
	if err != nil {
		return Config{}, err
	}
	return Parse(b)
}

// Parse decodes and checks the shape of a shares.json.
func Parse(b []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, fmt.Errorf("shares: unsupported version %d", c.Version)
	}
	if c.Shares == nil {
		c.Shares = []Share{}
	}
	seen := map[string]bool{}
	for i, s := range c.Shares {
		n, err := ValidateShape(s)
		if err != nil {
			return c, fmt.Errorf("share %q: %w", s.Name, err)
		}
		if seen[strings.ToLower(n.Name)] {
			return c, fmt.Errorf("share %q is duplicated", n.Name)
		}
		seen[strings.ToLower(n.Name)] = true
		c.Shares[i] = n
	}
	return c, nil
}

// Save writes the configuration (sorted by name) atomically with mode 0644.
func Save(path string, c Config) error {
	c.Version = 1
	sort.Slice(c.Shares, func(i, j int) bool { return strings.ToLower(c.Shares[i].Name) < strings.ToLower(c.Shares[j].Name) })
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'), 0o644)
}

// Find returns the index of the share with this name (case-insensitive).
func (c Config) Find(name string) int {
	for i, s := range c.Shares {
		if strings.EqualFold(s.Name, name) {
			return i
		}
	}
	return -1
}
