package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixture struct {
	t     *testing.T
	s     *Store
	net   string
	ntp   string
	clock time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	d := t.TempDir()
	f := &fixture{t: t, net: filepath.Join(d, "20-wired.network"), ntp: filepath.Join(d, "ntp.conf"),
		clock: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	f.write(f.net, "[Match]\nName=enp1s0\n\n[Network]\nDHCP=yes\n")
	f.s = &Store{Dir: filepath.Join(d, "state"), Files: []File{{"network", f.net}, {"time", f.ntp}},
		Now: func() time.Time { f.clock = f.clock.Add(time.Minute); return f.clock }}
	if err := f.s.Init(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) write(path, s string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) list() History {
	f.t.Helper()
	h, err := f.s.List()
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

func driftOf(h History, id string) string {
	for _, d := range h.Drift {
		if d.ID == id {
			return d.State
		}
	}
	return ""
}

func TestInitRecordsBaselineOnce(t *testing.T) {
	f := newFixture(t)
	if err := f.s.Init(); err != nil {
		t.Fatal(err)
	}
	h := f.list()
	if len(h.Entries) != 1 || h.Entries[0].Kind != KindBaseline || h.Entries[0].ID != 1 {
		t.Fatalf("entries = %+v", h.Entries)
	}
	e := h.Entries[0]
	if len(e.Changed) != 1 || e.Changed[0] != "network" {
		t.Fatalf("baseline changed = %v (absent time file is not a change)", e.Changed)
	}
	if driftOf(h, "network") != StateOK || driftOf(h, "time") != StateOK {
		t.Fatalf("drift = %+v", h.Drift)
	}
}

func TestRecordSkipsUnchangedState(t *testing.T) {
	f := newFixture(t)
	if _, wrote, err := f.s.Record("admin", KindChange, "time", 0); err != nil || wrote {
		t.Fatalf("Record without change wrote=%v err=%v", wrote, err)
	}
	f.write(f.ntp, "[Time]\nNTP=tik.cesnet.cz\n")
	e, wrote, err := f.s.Record("admin", KindChange, "time", 0)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if e.ID != 2 || e.User != "admin" || e.Module != "time" || len(e.Changed) != 1 || e.Changed[0] != "time" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestManualEditIsDriftAndPrepareKeepsIt(t *testing.T) {
	f := newFixture(t)
	f.write(f.net, "[Network]\nAddress=10.0.0.5/24\n")
	if driftOf(f.list(), "network") != StateModified {
		t.Fatal("manual edit not shown as drift")
	}
	if err := f.s.Prepare(); err != nil {
		t.Fatal(err)
	}
	h := f.list()
	if h.Entries[0].Kind != KindManual || driftOf(h, "network") != StateOK {
		t.Fatalf("after Prepare: %+v drift %+v", h.Entries[0], h.Drift)
	}
	// Nothing new: Prepare is a no-op.
	if err := f.s.Prepare(); err != nil || len(f.list().Entries) != 2 {
		t.Fatalf("second Prepare added an entry (err %v)", err)
	}
}

func TestPendingFileIsNotDriftNorRecorded(t *testing.T) {
	f := newFixture(t)
	pending := true
	f.s.Pending = func(id string) bool { return pending && id == "network" }
	f.write(f.net, "[Network]\nAddress=10.0.0.5/24\n")
	if driftOf(f.list(), "network") != StatePending {
		t.Fatal("pending network change not marked as pending")
	}
	f.write(f.ntp, "[Time]\nNTP=a.example\n")
	e, _, err := f.s.Record("admin", KindChange, "time", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Changed) != 1 || e.Changed[0] != "time" {
		t.Fatalf("pending network leaked into the time change: %v", e.Changed)
	}
	pending = false
	e, wrote, err := f.s.Record("admin", KindChange, "network", 0)
	if err != nil || !wrote || e.Changed[0] != "network" {
		t.Fatalf("confirm record = %+v %v %v", e, wrote, err)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	f := newFixture(t)
	f.s.MaxEntries = 5
	for i := 0; i < 8; i++ {
		f.write(f.ntp, "[Time]\nNTP=s"+string(rune('a'+i))+".example\n")
		if _, _, err := f.s.Record("admin", KindChange, "time", 0); err != nil {
			t.Fatal(err)
		}
	}
	h := f.list()
	if len(h.Entries) != 5 || h.Entries[0].ID != 9 || h.Entries[4].ID != 5 {
		t.Fatalf("ids = %v", ids(h))
	}
	f.s.MaxEntries, f.s.MaxBytes = 20, 1
	f.write(f.ntp, "[Time]\nNTP=z.example\n")
	f.s.Record("admin", KindChange, "time", 0)
	if h := f.list(); len(h.Entries) != 1 || h.Entries[0].ID != 10 {
		t.Fatalf("byte limit: ids = %v (newest must stay)", ids(h))
	}
}

func ids(h History) []int {
	var out []int
	for _, e := range h.Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestPlanRestoresByteIdenticalAndHonoursDrift(t *testing.T) {
	f := newFixture(t)
	orig, _ := os.ReadFile(f.net)
	f.write(f.net, "[Network]\nAddress=10.0.0.5/24\n")
	f.write(f.ntp, "[Time]\nNTP=a.example\n")
	if _, _, err := f.s.Record("admin", KindChange, "network", 0); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.Plan(1, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, r := range plan {
		switch r.ID {
		case "network":
			if !r.Present || string(r.Data) != string(orig) {
				t.Fatalf("network restore = %q", r.Data)
			}
		case "time":
			if r.Present {
				t.Fatal("time file did not exist in the baseline; restore must remove it")
			}
		}
	}
	f.write(f.ntp, "[Time]\nNTP=manual.example\n")
	if _, err := f.s.Plan(1, "admin", false); !errors.Is(err, ErrDrift) {
		t.Fatalf("drift without takeover: err = %v", err)
	}
	if _, err := f.s.Plan(1, "admin", true); err != nil {
		t.Fatal(err)
	}
	if h := f.list(); h.Entries[0].Kind != KindManual {
		t.Fatal("takeover did not keep the manual version in the history")
	}
	if _, err := f.s.Plan(99, "admin", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown version: err = %v", err)
	}
}

func TestPlanRejectsDamagedSnapshot(t *testing.T) {
	f := newFixture(t)
	f.write(f.net, "changed\n")
	f.s.Record("admin", KindChange, "network", 0)
	os.WriteFile(filepath.Join(f.s.entryDir(1), "network"), []byte("tampered"), 0o600)
	if _, err := f.s.Plan(1, "admin", false); err == nil {
		t.Fatal("damaged snapshot accepted")
	}
}

func TestInitRemovesHalfWrittenEntry(t *testing.T) {
	f := newFixture(t)
	tmp := filepath.Join(f.s.historyDir(), ".tmp-123")
	os.MkdirAll(tmp, 0o700)
	os.WriteFile(filepath.Join(tmp, "network"), []byte("half"), 0o600)
	if err := f.s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("half-written entry left behind")
	}
	if len(f.list().Entries) != 1 {
		t.Fatal("half-written entry listed")
	}
}

func TestGetShowsDiffAgainstCurrent(t *testing.T) {
	f := newFixture(t)
	f.write(f.net, "[Match]\nName=enp1s0\n\n[Network]\nAddress=10.0.0.5/24\n")
	f.s.Record("admin", KindChange, "network", 0)
	d, err := f.s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	var net FileDiff
	for _, fd := range d.Files {
		if fd.ID == "network" {
			net = fd
		} else if !fd.Same {
			t.Fatalf("%s should be unchanged", fd.ID)
		}
	}
	want := []DiffLine{{" ", "[Match]"}, {" ", "Name=enp1s0"}, {" ", ""}, {" ", "[Network]"},
		{"-", "Address=10.0.0.5/24"}, {"+", "DHCP=yes"}}
	if net.Same || len(net.Lines) != len(want) {
		t.Fatalf("lines = %+v", net.Lines)
	}
	for i := range want {
		if net.Lines[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, net.Lines[i], want[i])
		}
	}
}

func TestLinesTooLarge(t *testing.T) {
	big := ""
	for i := 0; i <= maxDiffLines; i++ {
		big += "x\n"
	}
	if _, tooLarge := Lines(big, "y\n"); !tooLarge {
		t.Fatal("expected tooLarge")
	}
	if out, _ := Lines("", "a\n"); len(out) != 1 || out[0] != (DiffLine{"+", "a"}) {
		t.Fatalf("new file diff = %+v", out)
	}
}

func TestExportReturnsEveryManagedFile(t *testing.T) {
	f := newFixture(t)
	out, err := f.s.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].ID != "network" || !out[0].Present || string(out[0].Data) != "[Match]\nName=enp1s0\n\n[Network]\nDHCP=yes\n" ||
		out[1].ID != "time" || out[1].Present || out[1].Data != nil {
		t.Fatalf("export = %+v", out)
	}
}

func TestImportPlansOnlyIncludedChangedFiles(t *testing.T) {
	f := newFixture(t)
	same := []byte("[Match]\nName=enp1s0\n\n[Network]\nDHCP=yes\n")
	// The network file is unchanged and the time file is left out: nothing to do.
	plan, err := f.s.PlanImport([]Incoming{{ID: "network", Present: true, Data: same}}, "admin", false)
	if err != nil || len(plan) != 0 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	in := []Incoming{{ID: "network", Present: false}, {ID: "time", Present: true, Data: []byte("[Time]\nNTP=a\n")}}
	d, err := f.s.Compare(in)
	if err != nil || len(d.Files) != 2 || d.Files[0].Same || d.Files[0].Present || d.Files[1].Same || !d.Files[1].Present {
		t.Fatalf("compare = %+v, %v", d, err)
	}
	plan, err = f.s.PlanImport(in, "admin", false)
	if err != nil || len(plan) != 2 || plan[0].ID != "network" || plan[0].Present || string(plan[1].Data) != "[Time]\nNTP=a\n" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if _, err := f.s.Compare([]Incoming{{ID: "passwd", Present: true}}); err == nil {
		t.Fatal("unknown file accepted")
	}
}

func TestImportHonoursDrift(t *testing.T) {
	f := newFixture(t)
	f.write(f.ntp, "[Time]\nNTP=manual\n")
	in := []Incoming{{ID: "time", Present: true, Data: []byte("[Time]\nNTP=b\n")}}
	if d, err := f.s.Compare(in); err != nil || len(d.Drift) != 1 || d.Drift[0] != "time" {
		t.Fatalf("compare drift = %+v, %v", d, err)
	}
	if _, err := f.s.PlanImport(in, "admin", false); !errors.Is(err, ErrDrift) {
		t.Fatalf("err = %v", err)
	}
	if plan, err := f.s.PlanImport(in, "admin", true); err != nil || len(plan) != 1 {
		t.Fatalf("takeover plan = %+v, %v", plan, err)
	}
	if h := f.list(); h.Entries[0].Kind != KindManual {
		t.Fatalf("manual state not kept: %+v", h.Entries[0])
	}
}

// A file added to the registry by a later build: Init records it at once (no
// drift), and a rollback to a version that predates it leaves it alone.
func TestRegistryGrowthKeepsNewFileUntouched(t *testing.T) {
	f := newFixture(t)
	fw := filepath.Join(filepath.Dir(f.net), "firewall.json")
	f.write(fw, "{\"enabled\":true}\n")
	f.s.Files = append(f.s.Files, File{"firewall", fw})
	if err := f.s.Init(); err != nil {
		t.Fatal(err)
	}
	h := f.list()
	if len(h.Entries) != 2 || h.Entries[0].Kind != KindBaseline || len(h.Entries[0].Changed) != 1 || h.Entries[0].Changed[0] != "firewall" {
		t.Fatalf("entries = %+v", h.Entries)
	}
	if driftOf(h, "firewall") != StateOK {
		t.Fatalf("drift = %+v", h.Drift)
	}
	if err := f.s.Init(); err != nil || len(f.list().Entries) != 2 {
		t.Fatal("second Init recorded again")
	}
	plan, err := f.s.Plan(1, "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan {
		if r.ID == "firewall" {
			t.Fatalf("rollback to a version without the file plans %+v", r)
		}
	}
	d, err := f.s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range d.Files {
		if fd.ID == "firewall" {
			t.Fatalf("detail of an old version lists the untracked file: %+v", fd)
		}
	}
}
