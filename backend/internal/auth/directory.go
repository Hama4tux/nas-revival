package auth

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"nas-revival/backend/internal/accounts"
)

// Directory answers who may log in and with which role (ADR 0004):
//   - a NAS account (uid ≥ 1000 in nas-users) with a web hash in
//     credentials.json that is not disabled; the role comes from nas-admin /
//     nas-viewer membership in /etc/group;
//   - otherwise the recovery administrator from admin.json (set over SSH with
//     `nas-revival set-password`), always an administrator.
//
// A NAS account with the same name as the recovery administrator replaces it
// completely, so creating that account is the migration path off admin.json.
// The files are re-read when their size or mtime changes, so a role change,
// disable or delete (also from the console) applies to the next request.
type Directory struct {
	Paths accounts.Paths // Shadow is ignored: the web process cannot read it
	Web   accounts.WebPasswords
	Admin *Credentials // nil: no recovery administrator

	mu    sync.Mutex
	stamp [3]fileStamp
	db    accounts.DB
	web   map[string]accounts.WebPassword
	err   error
}

type fileStamp struct {
	size  int64
	mtime time.Time
	ok    bool
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{size: st.Size(), mtime: st.ModTime(), ok: true}
}

func (d *Directory) refreshLocked() {
	now := [3]fileStamp{stampOf(d.Paths.Passwd), stampOf(d.Paths.Group), stampOf(d.Web.Path)}
	if now == d.stamp && (d.db.Passwd != nil || d.err != nil) {
		return
	}
	p := d.Paths
	p.Shadow = ""
	db, err := accounts.Load(p)
	var web map[string]accounts.WebPassword
	if err == nil {
		web, err = d.Web.Load()
	}
	if err != nil {
		// Fail closed for NAS accounts; the recovery administrator still works.
		slog.Error("read accounts for login", "err", err)
		db, web = accounts.DB{}, nil
	}
	d.stamp, d.db, d.web, d.err = now, db, web, err
}

// Account resolves a username to its web hash and role.
func (d *Directory) Account(name string) (hash string, role accounts.Role, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	if d.db.IsNASUser(name) {
		w, found := d.web[name]
		if !found || w.Hash == "" || w.Disabled {
			return "", "", false
		}
		return w.Hash, d.db.RoleOf(name), true
	}
	if d.Admin != nil && name == d.Admin.Username {
		return d.Admin.Hash, accounts.RoleAdmin, true
	}
	return "", "", false
}

// Hash is the HashLookup for NewLookupVerifier.
func (d *Directory) Hash(name string) (string, bool) {
	h, _, ok := d.Account(name)
	return h, ok
}

// Role is the current role of a logged-in user; ok=false ends the session
// (account deleted or disabled).
func (d *Directory) Role(name string) (accounts.Role, bool) {
	_, r, ok := d.Account(name)
	return r, ok
}

// IsNASUser reports whether name is a NAS account (its password is changed
// through the helper) rather than the recovery administrator.
func (d *Directory) IsNASUser(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	return d.db.IsNASUser(name)
}
