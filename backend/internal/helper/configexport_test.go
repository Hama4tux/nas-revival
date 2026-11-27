package helper

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"nas-revival/backend/internal/config"
)

func (c *configFixture) export(t *testing.T) ConfigArchive {
	t.Helper()
	c.d.Config.Hostname = func() string { return "nas" }
	res, err := c.call(t, "config.export", nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.(ConfigArchive)
}

func archiveJSON(t *testing.T, a ConfigArchive) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestExportImportRoundTrip(t *testing.T) {
	c := newConfigFixture(t)
	c.call(t, "time.apply", map[string]any{"ntpServers": []string{"tik.cesnet.cz"}})
	a := c.export(t)
	if a.Format != ArchiveFormat || a.Schema != ArchiveSchema || a.Hostname != "nas" || len(a.Files) != 3 {
		t.Fatalf("export = %+v", a)
	}
	raw := archiveJSON(t, a)

	// The NAS changes after the export; the preview shows the way back.
	c.call(t, "time.apply", map[string]any{"ntpServers": []string{"pool.ntp.org"}})
	res, err := c.call(t, "config.importPreview", map[string]any{"archive": raw})
	if err != nil {
		t.Fatal(err)
	}
	p := res.(ConfigImportPreview)
	if p.OtherHost || len(p.Drift) != 0 || len(p.Files) != 3 || p.Files[1].ID != FileTime || p.Files[1].Same || !p.Files[0].Same {
		t.Fatalf("preview = %+v", p)
	}

	res, err = c.call(t, "config.import", map[string]any{"archive": raw})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(ConfigImportResult); len(r.Restored) != 1 || r.Restored[0] != FileTime || r.NetworkPending != nil {
		t.Fatalf("import = %+v", r)
	}
	if b, _ := os.ReadFile(c.ntp); !strings.Contains(string(b), "NTP=tik.cesnet.cz") {
		t.Fatalf("NTP drop-in = %q", b)
	}
	if e := c.history(t).Entries[0]; e.Kind != config.KindImport || e.User != "admin" || e.Changed[0] != FileTime {
		t.Fatalf("history = %+v", e)
	}
	// Importing the same file again changes nothing and adds no version.
	n := len(c.history(t).Entries)
	if res, err := c.call(t, "config.import", map[string]any{"archive": raw}); err != nil || len(res.(ConfigImportResult).Restored) != 0 {
		t.Fatalf("second import = %+v, %v", res, err)
	}
	if len(c.history(t).Entries) != n {
		t.Fatal("no-op import recorded a version")
	}
}

func TestImportCanonicalisesFiles(t *testing.T) {
	// Directives the settings pages cannot write are dropped.
	b, present, err := canonical(FileNetwork, []byte("[Match]\nName=e*\n\n[Network]\nDHCP=yes\n\n[Route]\nGateway=10.9.9.9\n"))
	if err != nil || !present || strings.Contains(string(b), "Route") {
		t.Fatalf("network = %q, %v", b, err)
	}
	// The network parser is tolerant: an address it cannot read is left out,
	// the preview then shows DHCP and the confirm window guards the change.
	if b, _, err := canonical(FileNetwork, []byte("[Network]\nAddress=10.0.0.5/40\nGateway=10.0.0.1\n")); err != nil || strings.Contains(string(b), "10.0.0.5") {
		t.Fatalf("bad address = %q, %v", b, err)
	}
	b, _, err = canonical(FileTime, []byte("[Time]\nNTP=tik.cesnet.cz\nFallbackNTP=evil.example\n"))
	if err != nil || strings.Contains(string(b), "evil") {
		t.Fatalf("time = %q, %v", b, err)
	}
	if _, present, err := canonical(FileTime, []byte("[Time]\n")); err != nil || present {
		t.Fatalf("empty NTP list must mean defaults: %v %v", present, err)
	}
	b, _, err = canonical(FileSmb, []byte(`{"version":1,"enabled":true,"workgroup":"WORKGROUP","description":"NAS","minProtocol":"SMB3_11","netbios":false,"extra":"x"}`))
	if err != nil || strings.Contains(string(b), "extra") || !strings.HasSuffix(string(b), "}\n") {
		t.Fatalf("smb = %q, %v", b, err)
	}
	for id, bad := range map[string]string{
		FileTime:      "[Time]\nNTP=bad;host\n",
		FileDiskPower: `{"version":1,"disks":{"../sda":{"profile":"eco","standbyMinutes":10}}}`,
		FileShares:    `{"version":2,"shares":[]}`,
		FileSmb:       `{"version":1,"workgroup":"a b c d e f g h i j k l m n o"}`,
		FileNfs:       `not json`,
		FileSsh:       `{"version":1,"port":0}`,
	} {
		if _, _, err := canonical(id, []byte(bad)); err == nil {
			t.Errorf("%s accepted %q", id, bad)
		}
	}
}

func TestImportRejectsForeignFiles(t *testing.T) {
	c := newConfigFixture(t)
	good := c.export(t)
	mutate := func(f func(*ConfigArchive)) json.RawMessage {
		a := good
		a.Files = append([]ArchiveFile{}, good.Files...)
		f(&a)
		return archiveJSON(t, a)
	}
	cases := map[string]json.RawMessage{
		"format":    mutate(func(a *ConfigArchive) { a.Format = "zyxel" }),
		"newer":     mutate(func(a *ConfigArchive) { a.Schema = ArchiveSchema + 1 }),
		"unknown":   mutate(func(a *ConfigArchive) { a.Files = append(a.Files, ArchiveFile{ID: "passwd", Present: true, Content: "root::0:0"}) }),
		"duplicate": mutate(func(a *ConfigArchive) { a.Files = append(a.Files, a.Files[0]) }),
		"empty":     mutate(func(a *ConfigArchive) { a.Files = nil }),
		"absent":    mutate(func(a *ConfigArchive) { a.Files[1] = ArchiveFile{ID: FileTime, Content: "x"} }),
		"big":       mutate(func(a *ConfigArchive) { a.Files[1] = ArchiveFile{ID: FileTime, Present: true, Content: strings.Repeat("#", MaxArchiveFile+1)} }),
		"field":     json.RawMessage(`{"format":"nas-revival-config","schema":1,"files":[],"script":"x"}`),
	}
	for name, raw := range cases {
		if _, err := c.call(t, "config.importPreview", map[string]any{"archive": raw}); codeOf(err) != CodeInvalid {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A file from another NAS is allowed but flagged.
	other := mutate(func(a *ConfigArchive) { a.Hostname = "jiny-nas" })
	res, err := c.call(t, "config.importPreview", map[string]any{"archive": other})
	if err != nil || !res.(ConfigImportPreview).OtherHost {
		t.Fatalf("other host = %+v, %v", res, err)
	}
}

func TestImportRefusedWhileNetworkPending(t *testing.T) {
	c := newConfigFixture(t)
	raw := archiveJSON(t, c.export(t))
	c.call(t, "network.apply", newCfg)
	if _, err := c.call(t, "config.import", map[string]any{"archive": raw}); codeOf(err) != CodeChangePending {
		t.Fatalf("err = %v", err)
	}
}
