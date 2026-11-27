package helper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nas-revival/backend/internal/services"
)

type exitErr int

func (e exitErr) Error() string { return "exit status" }
func (e exitErr) ExitCode() int { return int(e) }

type logFx struct {
	m     *LogManager
	calls []string
	out   map[string]string // command prefix -> output
	err   map[string]error
}

func newLogFx(t *testing.T) *logFx {
	t.Helper()
	f := &logFx{out: map[string]string{}, err: map[string]error{}}
	f.m = &LogManager{Dropin: filepath.Join(t.TempDir(), "journald.conf.d", "50-nas-revival.conf"),
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, line)
			for p, e := range f.err {
				if strings.HasPrefix(line, p) {
					return []byte(f.out[p]), e
				}
			}
			for p, o := range f.out {
				if strings.HasPrefix(line, p) {
					return []byte(o), nil
				}
			}
			return nil, nil
		}}
	if err := os.MkdirAll(filepath.Dir(f.m.Dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLogQuery(t *testing.T) {
	f := newLogFx(t)
	f.out["journalctl --output=json"] = `{"__REALTIME_TIMESTAMP":"1795780800000000","MESSAGE":"b","_SYSTEMD_UNIT":"ssh.service"}
{"__REALTIME_TIMESTAMP":"1795780700000000","MESSAGE":"a"}
`
	r, err := f.m.Query(context.Background(), services.Query{Category: "auth", Limit: 1})
	if err != nil || len(r.Entries) != 1 || !r.More || r.Entries[0].Message != "b" {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.HasPrefix(f.calls[0], "journalctl --output=json") || !strings.Contains(f.calls[0], "--lines=2") ||
		!strings.HasSuffix(f.calls[0], "SYSLOG_FACILITY=4 SYSLOG_FACILITY=10") {
		t.Errorf("call = %q", f.calls[0])
	}
}

func TestLogQueryValidationAndErrors(t *testing.T) {
	f := newLogFx(t)
	_, err := f.m.Query(context.Background(), services.Query{Unit: "evil.service"})
	var he *Error
	if !errors.As(err, &he) || he.Code != CodeInvalid || he.Field != "unit" || len(f.calls) != 0 {
		t.Fatalf("err = %v calls = %v", err, f.calls)
	}
	// grep without a match: exit 1, no output = empty result
	f.err["journalctl"] = exitErr(1)
	r, err := f.m.Query(context.Background(), services.Query{Text: "nothing"})
	if err != nil || len(r.Entries) != 0 || r.More {
		t.Errorf("no match: %+v %v", r, err)
	}
	if string(mustJSON(t, r)) != `{"entries":[],"more":false}` {
		t.Errorf("json = %s", mustJSON(t, r))
	}
	// exit 1 without --grep is a failure
	if _, err := f.m.Query(context.Background(), services.Query{}); codeOf(err) != CodeInternal {
		t.Errorf("exit 1 without text: %v", err)
	}
	f.err["journalctl"] = errors.New("boom")
	if _, err := f.m.Query(context.Background(), services.Query{Text: "x"}); codeOf(err) != CodeInternal {
		t.Errorf("failure: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseDiskUsage(t *testing.T) {
	for in, want := range map[string]uint64{
		"Archived and active journals take up 59.1M in the file system.": 61970842,
		"Archived and active journals take up 8.0K in the file system.":  8192,
		"Archived and active journals take up 1G in the file system.":    1 << 30,
	} {
		if got, ok := ParseDiskUsage(in); !ok || got != want {
			t.Errorf("%q = %d %v, want %d", in, got, ok, want)
		}
	}
	if _, ok := ParseDiskUsage("garbage"); ok {
		t.Error("garbage parsed")
	}
}

func TestRetentionGetDefaultsAndParse(t *testing.T) {
	f := newLogFx(t)
	f.out["journalctl --disk-usage"] = "Archived and active journals take up 8.0K in the file system.\n"
	r, err := f.m.RetentionGet(context.Background())
	if err != nil || r.MaxUse != "64M" || r.MaxRetention != "none" || r.UsageBytes == nil || *r.UsageBytes != 8192 {
		t.Fatalf("%+v %v", r, err)
	}
	if err := os.WriteFile(f.m.Dropin, []byte("[Journal]\nSystemMaxUse=128M\nMaxRetentionSec=1month\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ = f.m.RetentionGet(context.Background())
	if r.MaxUse != "128M" || r.MaxRetention != "1month" {
		t.Errorf("%+v", r)
	}
	// unknown values fall back; disk usage failure omits usageBytes
	os.WriteFile(f.m.Dropin, []byte("SystemMaxUse=1T\nMaxRetentionSec=0\n"), 0o644)
	f.err["journalctl --disk-usage"] = errors.New("x")
	r, err = f.m.RetentionGet(context.Background())
	if err != nil || r.MaxUse != "64M" || r.MaxRetention != "none" || r.UsageBytes != nil {
		t.Errorf("%+v %v", r, err)
	}
	if strings.Contains(string(mustJSON(t, r)), "usageBytes") {
		t.Error("usageBytes not omitted")
	}
}

func TestRetentionSet(t *testing.T) {
	f := newLogFx(t)
	r, err := f.m.RetentionSet(context.Background(), JournalRetention{MaxUse: "256M", MaxRetention: "3month"})
	if err != nil || r.MaxUse != "256M" || r.MaxRetention != "3month" {
		t.Fatalf("%+v %v", r, err)
	}
	b, _ := os.ReadFile(f.m.Dropin)
	for _, want := range []string{"# /etc/systemd/journald.conf.d/50-nas-revival.conf", "[Journal]\n", "SyncIntervalSec=15min\n",
		"SystemMaxUse=256M\n", "MaxRetentionSec=3month\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("drop-in lacks %q:\n%s", want, b)
		}
	}
	if st, _ := os.Stat(f.m.Dropin); st.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", st.Mode())
	}
	if f.calls[0] != "systemctl restart -- systemd-journald.service" {
		t.Errorf("calls = %q", f.calls)
	}
	// none: no MaxRetentionSec line
	if _, err := f.m.RetentionSet(context.Background(), JournalRetention{MaxUse: "32M", MaxRetention: "none"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(f.m.Dropin)
	if strings.Contains(string(b), "MaxRetentionSec") || !strings.Contains(string(b), "SystemMaxUse=32M") {
		t.Errorf("drop-in:\n%s", b)
	}
}

func TestRetentionSetRejectsAndFailures(t *testing.T) {
	f := newLogFx(t)
	for _, s := range []JournalRetention{{MaxUse: "1G", MaxRetention: "none"}, {MaxUse: "64M", MaxRetention: "2year"}, {MaxUse: "64M\nStorage=none", MaxRetention: "none"}, {}} {
		if _, err := f.m.RetentionSet(context.Background(), s); codeOf(err) != CodeInvalid {
			t.Errorf("%+v: %v", s, err)
		}
	}
	if _, err := os.Stat(f.m.Dropin); err == nil || len(f.calls) != 0 {
		t.Errorf("invalid input wrote a file or ran commands: %v", f.calls)
	}
	f.err["systemctl"] = errors.New("x")
	if _, err := f.m.RetentionSet(context.Background(), JournalRetention{MaxUse: "64M", MaxRetention: "none"}); codeOf(err) != CodeInternal {
		t.Errorf("restart failure: %v", err)
	}
}

func TestRenderMatchesDeployFile(t *testing.T) {
	want, err := os.ReadFile("../../../deploy/debian/50-nas-revival-journald.conf")
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderJournalDropin(JournalRetention{MaxUse: "64M", MaxRetention: "none"}); string(got) != string(want) {
		t.Errorf("deploy file differs from the default render:\n%s", got)
	}
}

func TestDispatcherLogOps(t *testing.T) {
	f := newLogFx(t)
	d := &Dispatcher{Logs: f.m}
	ctx := context.Background()
	if _, err := d.Handle(ctx, request{Op: "journal.query", Args: json.RawMessage(`{"category":"kernel","priority":4}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "journal.query", Args: json.RawMessage(`{"bogus":1}`)}); codeOf(err) != CodeInvalid {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := d.Handle(ctx, request{Op: "journal.retention.get"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "journal.retention.set", Args: json.RawMessage(`{"maxUse":"32M","maxRetention":"1year"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Handle(ctx, request{Op: "journal.nope"}); codeOf(err) != CodeInvalid {
		t.Errorf("unknown op: %v", err)
	}
	if _, err := (&Dispatcher{}).Handle(ctx, request{Op: "journal.query"}); codeOf(err) != CodeInvalid {
		t.Errorf("disabled: %v", err)
	}
}
