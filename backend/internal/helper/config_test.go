package helper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nas-revival/backend/internal/config"
)

type configFixture struct {
	*fixture
	d     *Dispatcher
	drive *fakeDrive
	ntp   string
}

func newConfigFixture(t *testing.T) *configFixture {
	t.Helper()
	f := newFixture(t)
	drive := &fakeDrive{apm: apm254}
	power := newPower(t, drive)
	tm := &TimeManager{Dropin: filepath.Join(t.TempDir(), "50-nas-revival.conf"), Run: f.m.Run}
	store := &config.Store{Dir: filepath.Join(t.TempDir(), "config"), Pending: func(id string) bool { return id == FileNetwork && f.m.HasPending() },
		Files: []config.File{{ID: FileNetwork, Path: f.m.Paths.NetworkFile}, {ID: FileTime, Path: tm.Dropin}, {ID: FileDiskPower, Path: power.ConfigPath}}}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	cm := &ConfigManager{Store: store, Net: f.m, Time: tm, Power: power}
	return &configFixture{fixture: f, drive: drive, ntp: tm.Dropin,
		d: &Dispatcher{Net: f.m, Time: tm, Power: power, Config: cm}}
}

func (c *configFixture) call(t *testing.T, op string, args any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return c.d.Handle(context.Background(), request{Op: op, Args: raw, User: "admin"})
}

func (c *configFixture) history(t *testing.T) config.History {
	t.Helper()
	res, err := c.call(t, "config.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.(config.History)
}

func TestSettingsChangeRecordsVersionWithUser(t *testing.T) {
	c := newConfigFixture(t)
	if _, err := c.call(t, "time.apply", map[string]any{"ntpServers": []string{"tik.cesnet.cz"}}); err != nil {
		t.Fatal(err)
	}
	h := c.history(t)
	if len(h.Entries) != 2 {
		t.Fatalf("entries = %+v", h.Entries)
	}
	e := h.Entries[0]
	if e.Kind != config.KindChange || e.User != "admin" || e.Module != FileTime || e.Changed[0] != FileTime {
		t.Fatalf("entry = %+v", e)
	}
}

func TestNetworkVersionOnlyAfterConfirm(t *testing.T) {
	c := newConfigFixture(t)
	if _, err := c.call(t, "network.apply", newCfg); err != nil {
		t.Fatal(err)
	}
	if h := c.history(t); len(h.Entries) != 1 || h.Drift[0].State != config.StatePending {
		t.Fatalf("pending change: entries %d drift %+v", len(h.Entries), h.Drift)
	}
	if _, err := c.call(t, "network.confirm", nil); err != nil {
		t.Fatal(err)
	}
	if h := c.history(t); len(h.Entries) != 2 || h.Entries[0].Module != FileNetwork || h.Drift[0].State != config.StateOK {
		t.Fatalf("after confirm: %+v drift %+v", h.Entries, h.Drift)
	}
}

func TestRollbackRestoresAllFilesNetworkPending(t *testing.T) {
	c := newConfigFixture(t)
	c.call(t, "time.apply", map[string]any{"ntpServers": []string{"tik.cesnet.cz"}})
	c.call(t, "network.apply", newCfg)
	c.call(t, "network.confirm", nil)

	c.calls = nil
	res, err := c.call(t, "config.rollback", map[string]any{"id": 1})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(ConfigRollbackResult)
	if len(r.Restored) != 2 || r.Restored[1] != FileNetwork || r.NetworkPending == nil {
		t.Fatalf("result = %+v", r)
	}
	if _, err := os.Stat(c.ntp); !os.IsNotExist(err) {
		t.Fatal("NTP drop-in absent in version 1 must be removed")
	}
	if got := c.file(t); got != origNet {
		t.Fatalf("network file = %q, want byte-identical original", got)
	}
	h := c.history(t)
	if h.Entries[0].Kind != config.KindRollback || h.Entries[0].RollbackOf != 1 {
		t.Fatalf("rollback entry = %+v", h.Entries[0])
	}
	// A second rollback while the network change waits is refused.
	if _, err := c.call(t, "config.rollback", map[string]any{"id": 2}); err == nil {
		t.Fatal("rollback during pending network change accepted")
	}
	// Not confirmed: the timer restores the newer network file.
	c.fire()
	if c.file(t) == origNet {
		t.Fatal("unconfirmed network rollback was not reverted")
	}
}

func TestRollbackRefusesDriftWithoutTakeover(t *testing.T) {
	c := newConfigFixture(t)
	c.call(t, "time.apply", map[string]any{"ntpServers": []string{"tik.cesnet.cz"}})
	os.WriteFile(c.ntp, []byte("[Time]\nNTP=manual.example\n"), 0o644)
	_, err := c.call(t, "config.rollback", map[string]any{"id": 1})
	if he, ok := AsError(err); !ok || he.Code != CodeConfigDrift {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.call(t, "config.rollback", map[string]any{"id": 1, "takeover": true}); err != nil {
		t.Fatal(err)
	}
	h := c.history(t)
	if h.Entries[1].Kind != config.KindManual || h.Entries[0].Kind != config.KindRollback {
		t.Fatalf("history = %+v", h.Entries[:2])
	}
}

func TestRollbackDiskPowerReappliesDisks(t *testing.T) {
	c := newConfigFixture(t)
	apm := 128
	if _, err := c.call(t, "disk.setPower", map[string]any{"diskId": "wwn-0x1",
		"settings": map[string]any{"profile": "custom", "standbyMinutes": 20, "apm": apm}}); err != nil {
		t.Fatal(err)
	}
	c.drive.calls = nil
	if _, err := c.call(t, "config.rollback", map[string]any{"id": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.d.Power.ConfigPath); !os.IsNotExist(err) {
		t.Fatal("disks.json absent in version 1 must be removed")
	}
	if len(c.drive.calls) == 0 {
		t.Fatal("disks were not re-applied after rollback")
	}
}

func TestConfigOpsValidateArgs(t *testing.T) {
	c := newConfigFixture(t)
	for _, args := range []any{map[string]any{"id": 0}, map[string]any{"id": 1, "path": "/etc/shadow"}} {
		if _, err := c.call(t, "config.get", args); err == nil {
			t.Fatalf("args %v accepted", args)
		}
	}
	_, err := c.call(t, "config.get", map[string]any{"id": 42})
	if he, ok := AsError(err); !ok || he.Code != CodeNotFound {
		t.Fatalf("unknown version: %v", err)
	}
	if cleanUser("../root") != "unknown" || cleanUser("admin") != "admin" {
		t.Fatal("cleanUser")
	}
}

func TestClientSendsSessionUser(t *testing.T) {
	c := newConfigFixture(t)
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, ln, uint32(os.Getuid()), c.d); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	cl := &Client{Socket: sock}
	if _, err := cl.TimeApply(WithUser(context.Background(), "alice"), []string{"tik.cesnet.cz"}); err != nil {
		t.Fatal(err)
	}
	h, err := cl.ConfigHistory(context.Background())
	if err != nil || h.Entries[0].User != "alice" {
		t.Fatalf("history = %+v %v", h.Entries, err)
	}
}
