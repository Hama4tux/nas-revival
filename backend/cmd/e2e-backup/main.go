// Command e2e-backup checks the backup helper ops on the deployed NAS (W4.3).
// Run as nas-revival (scripts/webui/e2e-backup.sh). Server addresses come from
// the NAS's own tasks.json, so nothing site-specific lives in the repo; it
// prints only results, never keys or addresses.
package main

import (
	"context"
	"fmt"
	"os"
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
	}
	check("backup-pull tasks present", n > 0, fmt.Sprintf("count=%d", n))
	if fail > 0 {
		os.Exit(1)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return "err=" + err.Error()
}
