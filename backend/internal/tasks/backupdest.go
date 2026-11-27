package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nas-revival/backend/internal/shares"
)

// Errors of ResolveJobDir.
var (
	ErrNoDest  = errors.New("backup task has no destination")
	ErrBadDest = errors.New("backup destination is not usable")
)

// DestMessage is the Czech message of a ResolveJobDir error.
func DestMessage(err error) string {
	switch {
	case errors.Is(err, ErrNoDest):
		return MsgNoDest
	case errors.Is(err, ErrBadDest):
		return MsgBadDest
	}
	return MsgFailed
}

// ResolveJobDir returns <share folder>/<dest folder>/<task id>, the directory
// that holds current/ and versions/ of a backup task. The share must exist in
// shares.json and be a backupTarget on BackupVolume; root is where that volume
// is mounted (BackupVolume itself, a temp dir in tests). No existing element
// of the path below root may be a symlink (elements that do not exist yet
// are created by the runner).
func ResolveJobDir(root, sharesPath string, t Task) (string, error) {
	if t.Backup == nil || !ValidID(t.ID) {
		return "", errors.New("not a backup task")
	}
	d := t.Backup.Dest
	if d.Share == "" {
		return "", ErrNoDest
	}
	cfg, err := shares.Load(sharesPath)
	if err != nil {
		return "", err
	}
	i := cfg.Find(d.Share)
	if i < 0 {
		return "", fmt.Errorf("%w: share %q does not exist", ErrBadDest, d.Share)
	}
	s := cfg.Shares[i]
	if !s.BackupTarget || s.Volume != BackupVolume || !shares.ValidName(s.Folder) {
		return "", fmt.Errorf("%w: share %q", ErrBadDest, s.Name)
	}
	if d.Folder != "" && !shares.ValidName(d.Folder) {
		return "", fmt.Errorf("%w: folder", ErrBadDest)
	}
	elems := []string{s.Folder}
	if d.Folder != "" {
		elems = append(elems, d.Folder)
	}
	elems = append(elems, t.ID)
	p := root
	for n, e := range elems {
		p = filepath.Join(p, e)
		st, err := os.Lstat(p)
		switch {
		case err == nil && !st.IsDir():
			return "", fmt.Errorf("%w: %s is not a real directory", ErrBadDest, strings.TrimPrefix(p, root))
		case err != nil && n == 0:
			return "", fmt.Errorf("%w: share folder missing", ErrBadDest)
		case err != nil && !os.IsNotExist(err):
			return "", err
		}
	}
	return p, nil
}

// BackupTargetUser returns the hook of the share manager (ShareManager.
// InUseAsBackupTarget): it names the backup task of tasks.json that stores
// into the share. An unreadable list counts as "in use" so a share is never
// pulled from under a backup by mistake.
func BackupTargetUser(tasksPath string) func(share string) (string, bool) {
	return func(share string) (string, bool) {
		list, err := Load(tasksPath)
		if err != nil {
			return "úloha záloh (seznam úloh nelze přečíst)", true
		}
		for _, t := range list {
			if t.Type == TypeBackup && t.Backup != nil && strings.EqualFold(t.Backup.Dest.Share, share) {
				return "úloha „" + t.Name + "“", true
			}
		}
		return "", false
	}
}
