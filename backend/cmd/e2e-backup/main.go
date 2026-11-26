// Command e2e-backup checks the backup helper ops on the deployed NAS (W4.3).
// Run as nas-revival (scripts/webui/e2e-backup.sh). Server addresses come from
// the NAS's own tasks.json, so nothing site-specific lives in the repo; it
// prints only results, never keys or addresses.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	if size <= 16<<20 {
		checkRestore(ctx, c, id, date, file, size, check)
	} else {
		fmt.Println("skip backup.restore (first file is over 16 MiB)")
	}
}

// checkRestore restores the small file into a throw-away share of a
// throw-away account and removes both again (W4.3 phase 2b).
func checkRestore(ctx context.Context, c *helper.Client, id, date, file string, size int64, check func(string, bool, string)) {
	const user, share = "nrt-br", "nrt-restore"
	admin := helper.WithUser(ctx, "w43-e2e")
	call := func(ctx context.Context, op string, args any) error { return c.Call(ctx, op, args, nil) }
	defer func() {
		call(admin, "share.delete", map[string]any{"name": share, "deleteData": true, "confirmName": share})
		call(admin, "user.delete", map[string]string{"name": user})
	}()
	b := make([]byte, 12)
	rand.Read(b)
	if err := call(admin, "user.create", map[string]any{"name": user, "fullName": "", "password": base64.RawURLEncoding.EncodeToString(b) + "aA1!",
		"groups": []string{}, "role": "user"}); err != nil {
		check("restore setup user", false, errText(err))
		return
	}
	if err := call(admin, "share.create", map[string]any{"share": map[string]any{"name": share, "description": "e2e", "volume": tasks.BackupVolume,
		"folder": share, "readOnly": false, "hidden": true, "recycle": map[string]any{"enabled": false, "days": 0},
		"permissions": []map[string]string{{"kind": "user", "name": user, "access": "rw"}}}}); err != nil {
		check("restore setup share", false, errText(err))
		return
	}
	as := helper.WithUser(ctx, user)
	req := helper.RestoreRequest{ID: id, Date: date, Path: file, Share: share, Name: "e2e-restore.bin"}
	_, err := c.BackupRestoreStart(as, req)
	check("backup.restore.start", err == nil, errText(err))
	var st helper.RestoreStatus
	for i := 0; i < 120; i++ {
		if st, err = c.BackupRestoreStatus(as); err != nil || !st.Running() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	check("backup.restore done", err == nil && st.State == helper.RestoreDone && st.Files == 1 && st.Bytes == size,
		fmt.Sprintf("state=%s files=%d bytes=%d %s", st.State, st.Files, st.Bytes, st.Message))
	var l helper.FileList
	err = c.Call(as, "fs.list", map[string]any{"share": share, "path": ""}, &l)
	found := false
	for _, e := range l.Entries {
		found = found || (e.Name == "e2e-restore.bin" && e.Size == size)
	}
	check("restored file visible to the account", err == nil && found && l.Total == 1, fmt.Sprintf("entries=%d %s", l.Total, errText(err)))
	_, err = c.BackupRestoreStart(as, req)
	var he *helper.Error
	check("backup.restore refuses an existing name", errors.As(err, &he) && he.Code == helper.CodeFileExists, errText(err))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return "err=" + err.Error()
}
