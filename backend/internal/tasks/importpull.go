package tasks

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// DefaultImportTime is the daily start of imported jobs (NAS local time).
const DefaultImportTime = "06:45"

// ImportPull converts the interim nas-pull jobs.conf (`name user@host
// paths...`, # comments) into backup-pull tasks and merges them into
// existing: a task with the same id is replaced, the others stay. The result
// is not validated; the caller runs Validate (the helper's task.set does).
func ImportPull(jobs io.Reader, existing []Task, at string) ([]Task, error) {
	if at == "" {
		at = DefaultImportTime
	}
	out := append([]Task{}, existing...)
	sc := bufio.NewScanner(jobs)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return nil, fmt.Errorf("jobs.conf: řádek %q nemá tvar „jméno uživatel@server složky…“", f[0])
		}
		name, target, paths := f[0], f[1], f[2:]
		if !ValidID(name) {
			return nil, fmt.Errorf("jobs.conf: jméno úlohy %q není platné ID (a-z, 0-9, nejvýše 16 znaků)", name)
		}
		u, h, ok := strings.Cut(target, "@")
		if !ok {
			return nil, fmt.Errorf("jobs.conf: úloha %s: chybí uživatel@server", name)
		}
		h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
		b := &BackupParams{Host: h, Port: DefaultSSHPort, User: u, KeepDays: 35}
		for _, p := range paths {
			p = strings.TrimSuffix(p, "/")
			b.Paths = append(b.Paths, p)
			if p == "nas-export" {
				b.Fresh = &FreshParams{File: "nas-export/last-ok", MaxAgeHours: 26}
			}
		}
		t := Task{ID: name, Name: "Záloha " + name, Type: TypeBackup, Enabled: true,
			Schedule: Schedule{Kind: KindDaily, Time: at}, Backup: b}
		replaced := false
		for i := range out {
			if out[i].ID == name {
				out[i], replaced = t, true
			}
		}
		if !replaced {
			out = append(out, t)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("jobs.conf: žádná úloha")
	}
	return out, nil
}
