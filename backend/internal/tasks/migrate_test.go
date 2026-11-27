package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/shares"
)

func migrationFixture(t *testing.T) (root, sharesPath string, list []Task) {
	t.Helper()
	root = t.TempDir()
	os.Mkdir(filepath.Join(root, "data"), 0o750)
	sharesPath = filepath.Join(t.TempDir(), "shares.json")
	if err := shares.Save(sharesPath, shares.Config{Shares: []shares.Share{{Name: "data", Volume: BackupVolume,
		Folder: "data", ReadOnly: true, BackupTarget: true, Permissions: []shares.Permission{}}}}); err != nil {
		t.Fatal(err)
	}
	mk := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join(root, "apps", "pull", "app", "same.txt"), "12345")
	os.MkdirAll(filepath.Join(root, "apps", "other"), 0o700)
	mk(filepath.Join(root, "versions", "2026-11-25", "pull", "app", "old.txt"), "old")
	os.MkdirAll(filepath.Join(root, "versions", "2026-11-26", "pull", "app"), 0o700)
	if err := os.Link(filepath.Join(root, "apps", "pull", "app", "same.txt"),
		filepath.Join(root, "versions", "2026-11-26", "pull", "app", "same.txt")); err != nil {
		t.Fatal(err)
	}
	b := goodBackup()
	b.Dest = BackupDest{}
	other := goodBackup()
	other.Dest = BackupDest{Share: "data", Folder: "keep"}
	t1, t2 := backupTask(b), backupTask(other)
	t2.ID = "other"
	return root, sharesPath, []Task{t1, t2, Default()[0]}
}

func TestMigrationMovesKeepHardlinks(t *testing.T) {
	root, sp, list := migrationFixture(t)
	plan, err := PlanMigration(root, sp, list, BackupDest{Share: "data", Folder: "daily-backup"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Migrated) != 1 || plan.Migrated[0] != "pull" || plan.Tasks[0].Backup.Dest.Folder != "daily-backup" ||
		plan.Tasks[1].Backup.Dest.Folder != "keep" || list[0].Backup.Dest.Share != "" {
		t.Fatalf("%+v", plan)
	}
	job := filepath.Join(root, "data", "daily-backup", "pull")
	if len(plan.Moves) != 3 || plan.Moves[0].To != filepath.Join(job, "current") ||
		plan.Moves[2].To != filepath.Join(job, "versions", "2026-11-26") ||
		len(plan.Mkdirs) != 3 {
		t.Fatalf("%+v", plan)
	}
	// planning changes nothing
	if _, err := os.Stat(filepath.Join(root, "apps", "pull")); err != nil {
		t.Fatal("plan moved data")
	}
	var chowned []string
	if err := plan.Apply(func(p string) error { chowned = append(chowned, p); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(chowned) != 3 {
		t.Fatalf("chown %v", chowned)
	}
	cur := filepath.Join(job, "current", "app", "same.txt")
	ver := filepath.Join(job, "versions", "2026-11-26", "app", "same.txt")
	if ino(t, cur) != ino(t, ver) {
		t.Error("hardlink lost")
	}
	if st, _ := os.Stat(filepath.Join(job, "versions")); st.Mode().Perm() != 0o750 {
		t.Errorf("mode %v", st.Mode())
	}
	if _, err := os.Stat(filepath.Join(root, "apps", "pull")); err == nil {
		t.Error("source left")
	}
	gone := RemoveLegacyDirs(root)
	if _, err := os.Stat(filepath.Join(root, "versions")); err == nil {
		t.Errorf("versions left, removed %v", gone)
	}
	// apps/other is not empty: apps stays, and so does its content
	if _, err := os.Stat(filepath.Join(root, "apps", "other")); err != nil {
		t.Error("apps/other removed")
	}
	// the migrated list validates and runs in the new layout
	if _, err := Validate(plan.Tasks); err != nil {
		t.Fatal(err)
	}
	// nothing left to do on a second run
	again, err := PlanMigration(root, sp, plan.Tasks, BackupDest{Share: "data", Folder: "daily-backup"})
	if err != nil || len(again.Migrated) != 0 || len(again.Moves) != 0 {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestMigrationRefusals(t *testing.T) {
	root, sp, list := migrationFixture(t)
	dest := BackupDest{Share: "data", Folder: "daily-backup"}
	// existing target
	os.MkdirAll(filepath.Join(root, "data", "daily-backup", "pull", "current"), 0o750)
	if _, err := PlanMigration(root, sp, list, dest); err == nil || !strings.Contains(err.Error(), "target exists") {
		t.Fatalf("existing target: %v", err)
	}
	os.RemoveAll(filepath.Join(root, "data", "daily-backup"))
	// a symlinked source is never followed
	os.RemoveAll(filepath.Join(root, "apps", "pull"))
	os.Symlink(t.TempDir(), filepath.Join(root, "apps", "pull"))
	if _, err := PlanMigration(root, sp, list, dest); err == nil {
		t.Fatal("symlink source accepted")
	}
	os.Remove(filepath.Join(root, "apps", "pull"))
	// a share that is no backup target
	cfg, _ := shares.Load(sp)
	cfg.Shares[0].BackupTarget = false
	shares.Save(sp, cfg)
	if _, err := PlanMigration(root, sp, list, dest); err == nil {
		t.Fatal("share without backupTarget accepted")
	}
}

func TestBackupTargetUser(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	task := backupTask(goodBackup())
	if err := Save(p, []Task{Default()[0], task}); err != nil {
		t.Fatal(err)
	}
	f := BackupTargetUser(p)
	if u, ok := f("DATA"); !ok || u != "úloha „Záloha“" {
		t.Errorf("%q %v", u, ok)
	}
	if _, ok := f("docs"); ok {
		t.Error("unrelated share in use")
	}
	if _, ok := BackupTargetUser(p + ".missing")("data"); ok {
		t.Error("default list uses a share")
	}
}
