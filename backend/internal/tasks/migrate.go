package tasks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// One-time migration of the backup data from the fixed layout
// (<root>/apps/<id>, <root>/versions/<date>/<id>) to the job directory of a
// backupTarget share (<share folder>/<folder>/<id>/{current,versions/<date>}).
// Everything is a rename on the same filesystem, so the hardlinks between the
// versions stay intact.

// Move is one rename of the migration.
type Move struct{ Task, From, To string }

// MigrationPlan is what PlanMigration found: directories to create, renames
// to do, and the task list with the destination set.
type MigrationPlan struct {
	Mkdirs   []string
	Moves    []Move
	Tasks    []Task
	Migrated []string // ids of the tasks that got a destination
}

// realDirOrNone: nil when p is a real directory, fs.ErrNotExist when absent,
// another error for a symlink or a file.
func realDirOrNone(p string) error {
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a real directory", p)
	}
	return nil
}

// PlanMigration prepares the migration of every backup task that has no
// destination yet. root is the data volume (BackupVolume). Nothing is changed;
// an existing target makes the plan fail.
func PlanMigration(root, sharesPath string, list []Task, dest BackupDest) (MigrationPlan, error) {
	plan := MigrationPlan{Tasks: make([]Task, len(list))}
	copy(plan.Tasks, list)
	planned := map[string]bool{}
	mkdir := func(p string) error {
		err := realDirOrNone(p)
		switch {
		case err == nil, planned[p]:
			return nil
		case errors.Is(err, fs.ErrNotExist):
			planned[p] = true
			plan.Mkdirs = append(plan.Mkdirs, p)
			return nil
		}
		return err
	}
	for i, t := range plan.Tasks {
		if t.Type != TypeBackup || t.Backup == nil || t.Backup.Dest.Share != "" {
			continue
		}
		b := *t.Backup
		b.Dest = dest
		t.Backup = &b
		plan.Tasks[i] = t
		plan.Migrated = append(plan.Migrated, t.ID)
		job, err := ResolveJobDir(root, sharesPath, t)
		if err != nil {
			return plan, fmt.Errorf("task %s: %w", t.ID, err)
		}
		type src struct{ from, to string }
		var srcs []src
		app := filepath.Join(root, "apps", t.ID)
		switch err := realDirOrNone(app); {
		case err == nil:
			srcs = append(srcs, src{app, filepath.Join(job, "current")})
		case !errors.Is(err, fs.ErrNotExist):
			return plan, err
		}
		vroot := filepath.Join(root, "versions")
		entries, err := os.ReadDir(vroot)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return plan, err
		}
		var dates []string
		for _, e := range entries {
			if dateRe.MatchString(e.Name()) {
				dates = append(dates, e.Name())
			}
		}
		sort.Strings(dates)
		for _, d := range dates {
			v := filepath.Join(vroot, d, t.ID)
			switch err := realDirOrNone(v); {
			case err == nil:
				srcs = append(srcs, src{v, filepath.Join(job, "versions", d)})
			case !errors.Is(err, fs.ErrNotExist):
				return plan, err
			}
		}
		if len(srcs) == 0 {
			continue
		}
		for _, p := range []string{filepath.Dir(job), job, filepath.Join(job, "versions")} {
			if p == filepath.Dir(job) && dest.Folder == "" {
				continue // the share folder itself exists (ResolveJobDir checked)
			}
			if err := mkdir(p); err != nil {
				return plan, err
			}
		}
		for _, s := range srcs {
			if _, err := os.Lstat(s.to); err == nil {
				return plan, fmt.Errorf("target exists: %s", s.to)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return plan, err
			}
			plan.Moves = append(plan.Moves, Move{Task: t.ID, From: s.from, To: s.to})
		}
	}
	return plan, nil
}

// Apply creates the directories (mode 0750, chown gives them to the backup
// account) and renames the data. It stops at the first error; running the
// migration again continues with what is left.
func (p MigrationPlan) Apply(chown func(path string) error) error {
	for _, d := range p.Mkdirs {
		if err := os.Mkdir(d, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := os.Chmod(d, 0o750); err != nil {
			return err
		}
		if chown != nil {
			if err := chown(d); err != nil {
				return err
			}
		}
	}
	for _, m := range p.Moves {
		if _, err := os.Lstat(m.To); err == nil {
			return fmt.Errorf("target exists: %s", m.To)
		}
		if err := os.Rename(m.From, m.To); err != nil {
			return err
		}
	}
	return nil
}

// RemoveLegacyDirs removes the emptied <root>/versions/<date> directories,
// <root>/versions and <root>/apps. Only empty real directories go; the
// returned paths are the ones removed.
func RemoveLegacyDirs(root string) []string {
	var gone []string
	rm := func(p string) {
		if realDirOrNone(p) != nil {
			return
		}
		if os.Remove(p) == nil { // fails when not empty
			gone = append(gone, p)
		}
	}
	vroot := filepath.Join(root, "versions")
	if realDirOrNone(vroot) == nil {
		entries, _ := os.ReadDir(vroot)
		for _, e := range entries {
			if dateRe.MatchString(e.Name()) {
				rm(filepath.Join(vroot, e.Name()))
			}
		}
	}
	rm(vroot)
	rm(filepath.Join(root, "apps"))
	return gone
}
