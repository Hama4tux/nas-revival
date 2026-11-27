package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/firewall"
)

// cfgFw is a config history with the time drop-in and firewall.json managed.
type cfgFw struct {
	fw    *fwFixture
	d     *Dispatcher
	ntp   string
	store *config.Store
}

func newCfgFw(t *testing.T) *cfgFw {
	t.Helper()
	f := newFixture(t)
	fw := newFwFixture(t)
	tm := &TimeManager{Dropin: filepath.Join(t.TempDir(), "50-nas-revival.conf"), Run: f.m.Run}
	store := &config.Store{Dir: filepath.Join(t.TempDir(), "config"),
		Files: []config.File{{ID: FileTime, Path: tm.Dropin}, {ID: FileFirewall, Path: fw.m.Paths.ConfigFile}}}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	cm := &ConfigManager{Store: store, Net: f.m, Time: tm, Firewall: fw.m}
	return &cfgFw{fw: fw, ntp: tm.Dropin, store: store, d: &Dispatcher{Net: f.m, Time: tm, Firewall: fw.m, Config: cm}}
}

func (c *cfgFw) call(t *testing.T, op string, args any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return c.d.Handle(context.Background(), request{Op: op, Args: raw, User: "admin"})
}

func (c *cfgFw) entries(t *testing.T) []config.Entry {
	t.Helper()
	h, err := c.store.List()
	if err != nil {
		t.Fatal(err)
	}
	return h.Entries
}

func (c *cfgFw) enable(t *testing.T) {
	t.Helper()
	if _, err := c.call(t, "firewall.apply", map[string]any{"settings": fwSettings(), "clientAddr": fwClient}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call(t, "firewall.confirm", nil); err != nil {
		t.Fatal(err)
	}
}

func TestFirewallVersionOnlyAfterConfirm(t *testing.T) {
	c := newCfgFw(t)
	if _, err := c.call(t, "firewall.apply", map[string]any{"settings": fwSettings(), "clientAddr": fwClient}); err != nil {
		t.Fatal(err)
	}
	if n := len(c.entries(t)); n != 1 {
		t.Fatalf("pending firewall change recorded: %d entries", n)
	}
	if _, err := c.call(t, "firewall.confirm", nil); err != nil {
		t.Fatal(err)
	}
	e := c.entries(t)
	if len(e) != 2 || e[0].Module != FileFirewall || e[0].User != "admin" || strings.Join(e[0].Changed, ",") != FileFirewall {
		t.Fatalf("entries = %+v", e)
	}
}

func TestRollbackFirewallWaitsForConfirm(t *testing.T) {
	c := newCfgFw(t)
	c.enable(t) // version 2: firewall on
	before, _ := os.ReadFile(c.fw.m.Paths.ConfigFile)

	res, err := c.call(t, "config.rollback", map[string]any{"id": 1, "clientAddr": fwClient})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(ConfigRollbackResult)
	if strings.Join(r.Restored, ",") != FileFirewall || r.FirewallPending == nil || r.FirewallPending.Next.Enabled {
		t.Fatalf("result = %+v", r)
	}
	if now, _ := os.ReadFile(c.fw.m.Paths.ConfigFile); string(now) != string(before) {
		t.Fatal("firewall.json written before confirmation")
	}
	if _, err := c.call(t, "config.rollback", map[string]any{"id": 2, "clientAddr": fwClient}); err == nil {
		t.Fatal("rollback during a pending firewall change accepted")
	}
	// Not confirmed: the boot file is loaded again, nothing saved.
	c.fw.fire()
	if s, _ := firewall.Load(c.fw.m.Paths.ConfigFile); !s.Enabled || c.fw.m.HasPending() {
		t.Fatalf("after timeout: enabled %v pending %v", s.Enabled, c.fw.m.HasPending())
	}

	if _, err := c.call(t, "config.rollback", map[string]any{"id": 1, "clientAddr": fwClient}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call(t, "firewall.confirm", nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := firewall.Load(c.fw.m.Paths.ConfigFile); s.Enabled || exists(c.fw.m.Paths.BootFile) {
		t.Fatalf("confirmed rollback: enabled %v, boot file %v", s.Enabled, exists(c.fw.m.Paths.BootFile))
	}
	if e := c.entries(t); e[0].Module != FileFirewall || e[0].Kind != config.KindChange {
		t.Fatalf("newest = %+v", e[0])
	}
}

func TestRollbackFirewallLockoutWritesNothing(t *testing.T) {
	c := newCfgFw(t)
	c.enable(t) // version 2
	if _, err := c.call(t, "time.apply", map[string]any{"ntpServers": []string{"tik.cesnet.cz"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call(t, "firewall.apply", map[string]any{"settings": firewall.Default(), "clientAddr": fwClient}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call(t, "firewall.confirm", nil); err != nil {
		t.Fatal(err)
	}
	// Version 2 allows only 192.0.2.0/24; a browser from elsewhere would be locked out.
	_, err := c.call(t, "config.rollback", map[string]any{"id": 2, "clientAddr": "198.51.100.7"})
	if he, ok := AsError(err); !ok || he.Code != CodeInvalid {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(c.ntp); !strings.Contains(string(b), "tik.cesnet.cz") || c.fw.m.HasPending() {
		t.Fatal("refused rollback changed something")
	}
	// Without a client address (old web process) the firewall is not restored either.
	if _, err := c.call(t, "config.rollback", map[string]any{"id": 2}); err == nil {
		t.Fatal("firewall restore without client address accepted")
	}
}

func TestArchiveFirewallPartIsStrict(t *testing.T) {
	good, _ := firewall.Encode(fwSettings())
	arc, _ := json.Marshal(ConfigArchive{Format: ArchiveFormat, Schema: ArchiveSchema,
		Files: []ArchiveFile{{ID: FileFirewall, Present: true, Content: string(good)}}})
	if _, in, err := parseArchive(arc); err != nil || len(in) != 1 || in[0].ID != FileFirewall {
		t.Fatalf("archive with a firewall part: %+v %v", in, err)
	}
	out, present, err := canonical(FileFirewall, good)
	again, _, err2 := canonical(FileFirewall, out)
	if err != nil || err2 != nil || !present || !strings.Contains(string(out), `"192.0.2.0/24"`) || string(again) != string(out) {
		t.Fatalf("valid part: %q %v %v %v", out, present, err, err2)
	}
	for _, bad := range []string{`{"enabled":true,"allow":{},"rules":[],"extra":1}`, `{"enabled":true,"allow":{"telnet":["0.0.0.0/0"]}}`, `[]`} {
		if _, _, err := canonical(FileFirewall, []byte(bad)); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}
