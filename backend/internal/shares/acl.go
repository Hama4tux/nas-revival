package shares

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"nas-revival/backend/internal/accounts"
)

// POSIX ACL xattr format v2 (what libacl reads and writes).
const (
	XattrAccess  = "system.posix_acl_access"
	XattrDefault = "system.posix_acl_default"

	aclVersion = 2

	TagUserObj  = 0x01
	TagUser     = 0x02
	TagGroupObj = 0x04
	TagGroup    = 0x08
	TagMask     = 0x10
	TagOther    = 0x20

	permR = 4
	permW = 2
	permX = 1

	idNone = 0xFFFFFFFF
)

// ACLEntry is one entry of an ACL.
type ACLEntry struct {
	Tag  uint16
	Perm uint16
	ID   uint32
}

// EncodeACL serialises entries sorted by tag then id (the kernel requires
// that order).
func EncodeACL(entries []ACLEntry) []byte {
	es := append([]ACLEntry{}, entries...)
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Tag != es[j].Tag {
			return es[i].Tag < es[j].Tag
		}
		return es[i].ID < es[j].ID
	})
	b := make([]byte, 4+8*len(es))
	binary.LittleEndian.PutUint32(b, aclVersion)
	for i, e := range es {
		o := 4 + 8*i
		binary.LittleEndian.PutUint16(b[o:], e.Tag)
		binary.LittleEndian.PutUint16(b[o+2:], e.Perm)
		binary.LittleEndian.PutUint32(b[o+4:], e.ID)
	}
	return b
}

func permFor(access string, dir bool) uint16 {
	switch access {
	case AccessRW:
		if dir {
			return permR | permW | permX
		}
		return permR | permW
	case AccessRO:
		if dir {
			return permR | permX
		}
		return permR
	}
	return 0
}

// BuildACL builds the access ACL of a directory (dir) or file. Principals the
// account database no longer knows are skipped. A read-only share writes
// rw entries as ro.
func BuildACL(s Share, db accounts.DB, dir bool) []ACLEntry {
	owner := uint16(permR | permW)
	if dir {
		owner = permR | permW | permX
	}
	es := []ACLEntry{
		{TagUserObj, owner, idNone},
		{TagGroupObj, 0, idNone},
		{TagMask, owner, idNone},
		{TagOther, 0, idNone},
	}
	if s.BackupTarget {
		// The backup runner is the only writer; people read through the
		// permissions below (never rw, validation refuses it).
		if u, ok := db.User(BackupAccount); ok {
			es = append(es, ACLEntry{TagUser, permFor(AccessRW, dir), uint32(u.UID)})
		}
	}
	for _, p := range s.Permissions {
		acc := p.Access
		if (s.ReadOnly || s.BackupTarget) && acc == AccessRW {
			acc = AccessRO
		}
		switch p.Kind {
		case KindUser:
			if u, ok := db.User(p.Name); ok {
				es = append(es, ACLEntry{TagUser, permFor(acc, dir), uint32(u.UID)})
			}
		case KindGroup:
			if g, ok := db.Group(p.Name); ok {
				es = append(es, ACLEntry{TagGroup, permFor(acc, dir), uint32(g.GID)})
			}
		}
	}
	return es
}

// XattrSetter sets one extended attribute; tests inject a recorder.
type XattrSetter func(path, name string, data []byte) error

// SysSetxattr is the production setter.
func SysSetxattr(path, name string, data []byte) error { return syscall.Setxattr(path, name, data, 0) }

// Errors of ApplyTree.
var ErrNoACL = errors.New("filesystem does not support ACLs")

// IncompleteError: the walk did not reach every entry.
type IncompleteError struct{ Done int }

func (e *IncompleteError) Error() string { return "ACL applied only to some entries" }

func isNoSupport(err error) bool {
	return errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)
}

// ApplyTree sets the ACLs on root and everything below it, never following
// symlinks. Directories get access and default ACL, regular files an access
// ACL without execute bits. It returns the number of processed entries.
func ApplyTree(ctx context.Context, root string, s Share, db accounts.DB, set XattrSetter) (int, error) {
	dirACL := EncodeACL(BuildACL(s, db, true))
	fileACL := EncodeACL(BuildACL(s, db, false))
	done, failed := 0, 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if werr != nil {
			if errors.Is(werr, fs.ErrNotExist) {
				return nil
			}
			slog.Warn("shares: walk", "path", p, "err", werr)
			failed++
			return nil
		}
		var serr error
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		case d.IsDir():
			if serr = set(p, XattrAccess, dirACL); serr == nil {
				serr = set(p, XattrDefault, dirACL)
			}
		case d.Type().IsRegular():
			serr = set(p, XattrAccess, fileACL)
		default:
			return nil
		}
		switch {
		case serr == nil:
			done++
		case isNoSupport(serr):
			return ErrNoACL
		case errors.Is(serr, fs.ErrNotExist):
		default:
			slog.Warn("shares: set ACL", "path", p, "err", serr)
			failed++
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrNoACL):
		return done, ErrNoACL
	case err != nil:
		return done, &IncompleteError{Done: done}
	case failed > 0:
		return done, &IncompleteError{Done: done}
	}
	return done, nil
}

// PrepareDir creates the folder or adopts an existing real directory (never a
// symlink). It reports whether the folder existed. Ownership and mode are set
// by the caller (ACL or fallback mode).
func PrepareDir(path string) (adopted bool, err error) {
	fi, err := os.Lstat(path)
	switch {
	case err == nil:
		if !fi.IsDir() {
			return false, errors.New("not a directory")
		}
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, os.Mkdir(path, 0o770)
	}
	return false, err
}

// ChownFunc changes owner and group (-1 keeps); tests inject a recorder.
type ChownFunc func(path string, uid, gid int) error

// SysLchown is the production ChownFunc (does not follow symlinks).
func SysLchown(path string, uid, gid int) error { return os.Lchown(path, uid, gid) }

// SetupRootACL sets the share root for ACL mode: root:root, mode 0770.
func SetupRootACL(path string, chown ChownFunc) error {
	if err := chown(path, 0, 0); err != nil {
		return err
	}
	return os.Chmod(path, 0o770)
}

// SetupRootFallback sets the share root for fallback mode (no ACL support):
// root:<nas-users>, mode 0770. No setgid bit: the helper unit has
// RestrictSUIDSGID=yes, and Samba's force group already gives new files the
// group nas-users.
func SetupRootFallback(path string, gid int, chown ChownFunc) error {
	if err := chown(path, 0, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o770)
}

// AdoptFallback walks an existing tree (no symlink following) and gives group
// gid read/write (directories also execute). Entries that already have
// these bits are left alone; a changed entry keeps its permission and sticky
// bits but loses setuid/setgid, which RestrictSUIDSGID would refuse to set. It returns the number of processed entries.
func AdoptFallback(ctx context.Context, root string, gid int, chown ChownFunc) (int, error) {
	done := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if werr != nil {
			if errors.Is(werr, fs.ErrNotExist) {
				return nil
			}
			return werr
		}
		if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if err := chown(p, -1, gid); err != nil {
			return err
		}
		need := fs.FileMode(0o060)
		if d.IsDir() {
			need = 0o070
		}
		if fi.Mode().Perm()&need != need {
			if err := os.Chmod(p, fi.Mode().Perm()|need|fi.Mode()&fs.ModeSticky); err != nil {
				return err
			}
		}
		done++
		return nil
	})
	if err != nil {
		return done, &IncompleteError{Done: done}
	}
	return done, nil
}
