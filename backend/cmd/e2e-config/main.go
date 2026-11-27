// Command e2e-config is the W4.4 end-to-end check on the NAS (scripts/webui/e2e-config.sh runs it as
// nas-revival). It exports the configuration through the helper, previews and imports the export unchanged
// (no write, no new version), checks that foreign files are refused, then imports one real change – an
// extra NTP server – and imports the original export again, so the NAS ends where it started. Two import
// versions stay in the config history as the record of the test.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"nas-revival/backend/internal/helper"
)

var fails int

func check(ok bool, format string, a ...any) {
	status := "ok  "
	if !ok {
		status, fails = "FAIL", fails+1
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, a...))
}

func code(err error) string {
	if he, ok := helper.AsError(err); ok {
		return he.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func raw(a helper.ConfigArchive) json.RawMessage {
	b, _ := json.Marshal(a)
	return b
}

func changed(p helper.ConfigImportPreview) []string {
	out := []string{}
	for _, f := range p.Files {
		if !f.Same {
			out = append(out, f.ID)
		}
	}
	return out
}

func main() {
	ctx := helper.WithUser(context.Background(), "w44-e2e")
	hc := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock"}
	a, err := hc.ConfigExport(ctx)
	check(err == nil && a.Format == helper.ArchiveFormat && a.Schema == helper.ArchiveSchema && len(a.Files) == 7,
		"export: %d files, host %q, err %v", len(a.Files), a.Hostname, err)
	if err != nil {
		os.Exit(1)
	}
	for _, f := range a.Files {
		// Password hashes (argon2id, yescrypt, sha512-crypt) and private keys; "passwordAuth" in ssh.json is a switch.
		secret := false
		for _, s := range []string{"$argon2", "$y$", "$6$", "PRIVATE KEY"} {
			secret = secret || strings.Contains(f.Content, s)
		}
		check(!secret, "export part %s holds no secrets (%d bytes)", f.ID, len(f.Content))
	}
	orig := raw(a)

	p, err := hc.ConfigImportPreview(ctx, orig)
	check(err == nil && len(changed(p)) == 0 && !p.OtherHost, "preview of the own export: changed %v, drift %v, err %v", changed(p), p.Drift, err)
	if err != nil || len(changed(p)) != 0 || len(p.Drift) != 0 {
		fmt.Println("stop: the NAS differs from its own export or has manual changes; nothing imported")
		os.Exit(1)
	}
	before, _ := hc.ConfigHistory(ctx)
	r, err := hc.ConfigImport(ctx, orig, false, "")
	after, _ := hc.ConfigHistory(ctx)
	check(err == nil && len(r.Restored) == 0 && len(after.Entries) == len(before.Entries),
		"import of the own export writes nothing: %v, versions %d → %d, err %v", r.Restored, len(before.Entries), len(after.Entries), err)

	for name, bad := range map[string]string{
		"foreign format": `{"format":"zyxel","schema":1,"files":[]}`,
		"newer schema":   `{"format":"nas-revival-config","schema":99,"files":[]}`,
		"unknown part":   `{"format":"nas-revival-config","schema":1,"files":[{"id":"passwd","present":true,"content":"x"}]}`,
		"invalid smb":    `{"format":"nas-revival-config","schema":1,"files":[{"id":"smb","present":true,"content":"{\"version\":1,\"minProtocol\":\"NT1\"}"}]}`,
	} {
		_, err := hc.ConfigImportPreview(ctx, json.RawMessage(bad))
		check(code(err) == helper.CodeInvalid, "%s refused: %v", name, err)
	}

	// One real change: the time part gets an NTP server, then the original export goes back.
	mod := a
	mod.Files = append([]helper.ArchiveFile{}, a.Files...)
	idx := -1
	for i, f := range mod.Files {
		if f.ID == helper.FileTime {
			idx = i
		}
	}
	if idx < 0 {
		check(false, "export has a time part")
		os.Exit(1)
	}
	mod.Files[idx] = helper.ArchiveFile{ID: helper.FileTime, Present: true, Content: helper.RenderTimesyncd([]string{"tik.cesnet.cz", "tak.cesnet.cz"})}
	p, err = hc.ConfigImportPreview(ctx, raw(mod))
	check(err == nil && strings.Join(changed(p), ",") == helper.FileTime, "preview of a changed NTP list: %v %v", changed(p), err)
	r, err = hc.ConfigImport(ctx, raw(mod), false, "")
	check(err == nil && strings.Join(r.Restored, ",") == helper.FileTime, "import of the changed NTP list: %v %v", r.Restored, err)
	h, _ := hc.ConfigHistory(ctx)
	check(len(h.Entries) > 0 && h.Entries[0].Kind == "import" && h.Entries[0].User == "w44-e2e", "history: newest version is the import by w44-e2e: %+v", h.Entries[0])
	ts, err := hc.TimeGet(ctx)
	check(err == nil && strings.Join(ts.NTPServers, " ") == "tik.cesnet.cz tak.cesnet.cz", "timesyncd uses the imported servers: %v %v", ts.NTPServers, err)

	r, err = hc.ConfigImport(ctx, orig, false, "")
	check(err == nil && strings.Join(r.Restored, ",") == helper.FileTime, "import of the original export restores the time part: %v %v", r.Restored, err)
	p, err = hc.ConfigImportPreview(ctx, orig)
	check(err == nil && len(changed(p)) == 0, "NAS matches the original export again: %v %v", changed(p), err)

	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}
