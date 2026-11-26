// Command e2e-backup checks the backup helper ops on the deployed NAS (W4.3).
// Run as nas-revival (scripts/webui/e2e-backup.sh). Server addresses come from
// the NAS's own tasks.json, so nothing site-specific lives in the repo; it
// prints only results, never keys or addresses.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tasks"
)

func main() {
	c := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock", Timeout: 90 * time.Second}
	ctx := context.Background()
	fail := 0
	check := func(name string, ok bool, detail string) {
		status := "ok  "
		if !ok {
			status = "FAIL"
			fail++
		}
		fmt.Printf("%s %s %s\n", status, name, detail)
	}

	k, err := c.BackupKey(ctx)
	check("backup.key.get", err == nil && strings.HasPrefix(k.PublicKey, "ssh-ed25519 ") &&
		strings.Contains(k.AuthorizedKeysHint, "restrict,command=\"rrsync -ro "), errText(err))

	sh, err := c.BackupShareGet(ctx)
	check("backup.share.get", err == nil && sh.Name == "zalohy", fmt.Sprintf("enabled=%v %s", sh.Enabled, errText(err)))

	l, err := tasks.Load(tasks.DefaultConfigPath)
	if err != nil {
		check("tasks.json", false, err.Error())
		os.Exit(1)
	}
	n := 0
	for _, t := range l {
		if t.Type != tasks.TypeBackup || t.Backup == nil {
			continue
		}
		n++
		b := t.Backup
		s, err := c.BackupHostScan(ctx, b.Host, b.Port)
		check("backup.host.scan "+t.ID, err == nil && len(s.Keys) > 0 && s.Known && !s.Changed,
			fmt.Sprintf("keys=%d known=%v changed=%v %s", len(s.Keys), s.Known, s.Changed, errText(err)))
		r, err := c.BackupTest(ctx, b.Host, b.Port, b.User, b.Paths[0])
		check("backup.test "+t.ID, err == nil && r.OK && !r.FullAccess,
			fmt.Sprintf("entries=%d fullAccess=%v %s", len(r.Entries), r.FullAccess, errText(err)))
		checkVersions(ctx, c, t.ID, check)
	}
	check("backup-pull tasks present", n > 0, fmt.Sprintf("count=%d", n))
	if fail > 0 {
		os.Exit(1)
	}
}

// checkVersions lists the versions, walks the newest one down to its first
// file, reads it through the passed descriptor and checks that the web
// account cannot open the same file directly. Prints counts only.
func checkVersions(ctx context.Context, c *helper.Client, id string, check func(string, bool, string)) {
	v, err := c.BackupVersions(ctx, id)
	check("backup.versions "+id, err == nil && len(v.Versions) > 0 && v.Complete,
		fmt.Sprintf("versions=%d complete=%v %s", len(v.Versions), v.Complete, errText(err)))
	if err != nil || len(v.Versions) == 0 {
		return
	}
	date, dir, file := v.Versions[0].Date, "", ""
	var size int64
	for depth := 0; depth < 8 && file == ""; depth++ {
		b, err := c.BackupBrowse(ctx, id, date, dir, 0)
		if err != nil {
			check("backup.browse "+id, false, errText(err))
			return
		}
		next := ""
		for _, e := range b.Entries {
			if e.BadName {
				continue
			}
			if e.Type == "file" && file == "" {
				file, size = path.Join(dir, e.Name), e.Size
			}
			if e.Type == "dir" && next == "" {
				next = path.Join(dir, e.Name)
			}
		}
		if file == "" {
			if next == "" {
				break
			}
			dir = next
		}
	}
	check("backup.browse "+id, file != "", fmt.Sprintf("depth=%d", strings.Count(file, "/")))
	if file == "" {
		return
	}
	f, meta, err := c.BackupOpen(ctx, id, date, file)
	if err != nil {
		check("backup.open "+id, false, errText(err))
		return
	}
	n, rerr := io.Copy(io.Discard, io.LimitReader(f, 64<<20))
	f.Close()
	check("backup.open "+id, rerr == nil && meta.Size == size && (n == size || n == 64<<20),
		fmt.Sprintf("read=%d size=%d", n, size))
	_, derr := os.Open(filepath.Join(tasks.BackupVolume, "versions", date, id, file))
	check("web account has no direct read access "+id, derr != nil && errors.Is(derr, fs.ErrPermission), "")
	if _, _, err := c.BackupOpen(ctx, id, date, "../"+id); err == nil {
		check("backup.open refuses ..", false, "")
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return "err=" + err.Error()
}
