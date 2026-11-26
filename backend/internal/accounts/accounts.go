// Package accounts reads the Linux account databases (passwd, group, shadow)
// and keeps the web password hashes of NAS users (ADR 0004). It only parses
// and stores; changes to the Linux accounts are made by the helper with the
// shadow tools.
package accounts

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Built-in groups. Every NAS account has UsersGroup as its primary group; the
// role groups (W2.2) are reserved and never editable as ordinary groups.
const (
	UsersGroup  = "nas-users"
	AdminGroup  = "nas-admin"
	ViewerGroup = "nas-viewer"
)

// First and last id of ordinary (non-system) accounts and groups.
const (
	MinID = 1000
	MaxID = 59999
)

// PasswdEntry is one line of /etc/passwd.
type PasswdEntry struct {
	Name  string
	UID   int
	GID   int
	Gecos string
}

// GroupEntry is one line of /etc/group.
type GroupEntry struct {
	Name    string
	GID     int
	Members []string
}

// ShadowEntry is one line of /etc/shadow.
type ShadowEntry struct {
	Name   string
	Hash   string
	Expire string // days since epoch, "" = never
}

// lines yields the non-empty, non-comment lines split on ':'.
func lines(data []byte, minFields int) [][]string {
	var out [][]string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || l[0] == '#' || l[0] == '+' || l[0] == '-' {
			continue
		}
		f := strings.Split(l, ":")
		if len(f) >= minFields {
			out = append(out, f)
		}
	}
	return out
}

func ParsePasswd(data []byte) []PasswdEntry {
	var out []PasswdEntry
	for _, f := range lines(data, 7) {
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, PasswdEntry{Name: f[0], UID: uid, GID: gid, Gecos: f[4]})
	}
	return out
}

func ParseGroup(data []byte) []GroupEntry {
	var out []GroupEntry
	for _, f := range lines(data, 4) {
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		g := GroupEntry{Name: f[0], GID: gid, Members: []string{}}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.Members = append(g.Members, m)
			}
		}
		out = append(out, g)
	}
	return out
}

func ParseShadow(data []byte) []ShadowEntry {
	var out []ShadowEntry
	for _, f := range lines(data, 8) {
		out = append(out, ShadowEntry{Name: f[0], Hash: f[1], Expire: f[7]})
	}
	return out
}

// HasPassword reports whether a usable password hash is stored (ignoring a
// lock prefix).
func (s ShadowEntry) HasPassword() bool {
	h := strings.TrimLeft(s.Hash, "!")
	return h != "" && h != "*" && h != "x"
}

// Locked reports a disabled account: locked hash or an expiry date in the past.
func (s ShadowEntry) Locked(today int) bool {
	if strings.HasPrefix(s.Hash, "!") {
		return true
	}
	if s.Expire == "" {
		return false
	}
	d, err := strconv.Atoi(s.Expire)
	return err == nil && d <= today
}

// Fingerprint identifies the current shadow hash without storing it; the
// lock prefix is ignored, so disabling an account does not look like a
// password change.
func (s ShadowEntry) Fingerprint() string {
	h := strings.TrimLeft(s.Hash, "!")
	if h == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(h))
	return hex.EncodeToString(sum[:])
}

// DB is a parsed snapshot of the three account files.
type DB struct {
	Passwd []PasswdEntry
	Groups []GroupEntry
	Shadow map[string]ShadowEntry
}

// Paths of the account files; tests use a temporary directory.
type Paths struct {
	Passwd, Group, Shadow string
}

var SystemPaths = Paths{Passwd: "/etc/passwd", Group: "/etc/group", Shadow: "/etc/shadow"}

// Load reads the three files. A missing shadow file (tests, no root) yields
// accounts without password information.
func Load(p Paths) (DB, error) {
	var db DB
	pw, err := os.ReadFile(p.Passwd)
	if err != nil {
		return db, err
	}
	gr, err := os.ReadFile(p.Group)
	if err != nil {
		return db, err
	}
	db.Passwd, db.Groups = ParsePasswd(pw), ParseGroup(gr)
	db.Shadow = map[string]ShadowEntry{}
	if p.Shadow == "" { // the web process cannot read shadow and does not try
		return db, nil
	}
	if sh, err := os.ReadFile(p.Shadow); err == nil {
		for _, s := range ParseShadow(sh) {
			db.Shadow[s.Name] = s
		}
	} else if !os.IsNotExist(err) {
		return db, err
	}
	return db, nil
}

func (db DB) User(name string) (PasswdEntry, bool) {
	for _, u := range db.Passwd {
		if u.Name == name {
			return u, true
		}
	}
	return PasswdEntry{}, false
}

func (db DB) Group(name string) (GroupEntry, bool) {
	for _, g := range db.Groups {
		if g.Name == name {
			return g, true
		}
	}
	return GroupEntry{}, false
}

// IsNASUser: an ordinary uid whose primary group is nas-users. System
// accounts, root and the service account never qualify.
func (db DB) IsNASUser(name string) bool {
	u, ok := db.User(name)
	if !ok || u.UID < MinID || u.UID > MaxID {
		return false
	}
	g, ok := db.Group(UsersGroup)
	return ok && u.GID == g.GID
}

// Reserved reports group names that are never editable: the nas-* built-ins.
func Reserved(name string) bool { return strings.HasPrefix(name, "nas-") }

// IsEditableGroup: an ordinary gid that is neither a built-in nor the
// private group of a non-NAS account (e.g. the installer's first user).
func (db DB) IsEditableGroup(name string) bool {
	g, ok := db.Group(name)
	if !ok || g.GID < MinID || g.GID > MaxID || Reserved(name) {
		return false
	}
	if u, ok := db.User(name); ok && u.GID == g.GID {
		return false
	}
	return true
}

// NASUsers returns the NAS accounts sorted by name.
func (db DB) NASUsers() []PasswdEntry {
	var out []PasswdEntry
	for _, u := range db.Passwd {
		if db.IsNASUser(u.Name) {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EditableGroupsOf lists the editable groups the user is a member of.
func (db DB) EditableGroupsOf(name string) []string {
	out := []string{}
	for _, g := range db.Groups {
		if db.IsEditableGroup(g.Name) && contains(g.Members, name) {
			out = append(out, g.Name)
		}
	}
	sort.Strings(out)
	return out
}

// OtherGroupsOf lists the supplementary groups that the UI does not manage;
// they are preserved when the membership is changed.
func (db DB) OtherGroupsOf(name string) []string {
	out := []string{}
	for _, g := range db.Groups {
		if !db.IsEditableGroup(g.Name) && contains(g.Members, name) {
			out = append(out, g.Name)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// FullName is the first GECOS field.
func FullName(gecos string) string {
	n, _, _ := strings.Cut(gecos, ",")
	return n
}

// Role of a NAS account in the web administration (ADR 0004).
type Role string

const (
	RoleAdmin  Role = "admin"  // member of nas-admin: everything
	RoleViewer Role = "viewer" // member of nas-viewer: read-only administration
	RoleUser   Role = "user"   // standard user: own account only
)

// ValidRole accepts the three roles.
func ValidRole(r Role) bool { return r == RoleAdmin || r == RoleViewer || r == RoleUser }

// RoleGroup is the Linux group that grants r; RoleUser has none.
func RoleGroup(r Role) string {
	switch r {
	case RoleAdmin:
		return AdminGroup
	case RoleViewer:
		return ViewerGroup
	}
	return ""
}

// RoleOf derives the role from the role groups; nas-admin wins if an account
// was put into both by hand.
func (db DB) RoleOf(name string) Role {
	if g, ok := db.Group(AdminGroup); ok && contains(g.Members, name) {
		return RoleAdmin
	}
	if g, ok := db.Group(ViewerGroup); ok && contains(g.Members, name) {
		return RoleViewer
	}
	return RoleUser
}

// Allows reports whether role r includes the rights of min
// (admin ⊃ viewer ⊃ user).
func (r Role) Allows(min Role) bool { return roleRank(r) >= roleRank(min) && roleRank(min) > 0 }

func roleRank(r Role) int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleViewer:
		return 2
	case RoleUser:
		return 1
	}
	return 0
}
