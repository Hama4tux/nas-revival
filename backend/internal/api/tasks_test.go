package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tasks"
)

type fakeTasks struct {
	list  []tasks.Task
	calls []string
}

func (f *fakeTasks) TaskGet(context.Context) (helper.TaskList, error) {
	f.calls = append(f.calls, "get")
	return helper.TaskList{Tasks: f.list}, nil
}
func (f *fakeTasks) TaskSet(_ context.Context, l []tasks.Task) (helper.TaskList, error) {
	f.calls = append(f.calls, "set")
	f.list = l
	return helper.TaskList{Tasks: l}, nil
}
func (f *fakeTasks) known(id string) error {
	for _, t := range f.list {
		if t.ID == id {
			return nil
		}
	}
	return &helper.Error{Code: helper.CodeNotFound, Message: "Úloha neexistuje."}
}
func (f *fakeTasks) TaskRun(_ context.Context, id string) error {
	f.calls = append(f.calls, "run "+id)
	return f.known(id)
}
func (f *fakeTasks) TaskStop(_ context.Context, id string) error {
	f.calls = append(f.calls, "stop "+id)
	return f.known(id)
}

func newTaskServer(t *testing.T) (http.Handler, *fakeTasks, map[string]*http.Cookie, *[]string, string) {
	t.Helper()
	s, h, _, _ := newRoleServer(t)
	f := &fakeTasks{list: tasks.Default()}
	hist := filepath.Join(t.TempDir(), "task-history.json")
	var argv []string
	s.Tasks = f
	s.TaskStatus = TaskStatus{HistoryPath: hist, Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		argv = append(argv, name+" "+strings.Join(args, " "))
		if args[1] == "--timestamp=unix" {
			return []byte("Id=nas-revival-task-recycle.timer\nNextElapseUSecRealtime=@1780000000\n"), nil
		}
		return []byte("Id=nas-revival-task@recycle.service\nActiveState=active\n"), nil
	}}
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	return h, f, cookies, &argv, hist
}

func TestTasksRoles(t *testing.T) {
	h, f, cookies, _, _ := newTaskServer(t)
	ok := `{"tasks":[{"id":"recycle","name":"Koše","type":"recycle-cleanup","enabled":true,"schedule":{"kind":"daily","time":"03:30"}}]}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/tasks", "", 200, 200, 403, "get"},
		{"PUT", "/api/v1/tasks", ok, 200, 403, 403, "set"},
		{"POST", "/api/v1/tasks/recycle/run", "", 202, 403, 403, "run recycle"},
		{"POST", "/api/v1/tasks/recycle/stop", "", 202, 403, 403, "stop recycle"},
		{"GET", "/api/v1/tasks/recycle/history", "", 200, 200, 403, "get"},
		{"POST", "/api/v1/tasks/nope/run", "", 404, 403, 403, "run nope"},
		{"POST", "/api/v1/tasks/nope/stop", "", 404, 403, 403, "stop nope"},
		{"GET", "/api/v1/tasks/nope/history", "", 404, 404, 403, "get"},
		{"POST", "/api/v1/tasks/..%2Fx/run", "", 404, 403, 403, ""},
		// Rejected before the helper is asked.
		{"PUT", "/api/v1/tasks", `{"tasks":[{"name":"x","type":"shell","schedule":{"kind":"daily","time":"03:30"}}]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/tasks", `{"tasks":[{"name":"x","type":"recycle-cleanup","command":"id","schedule":{"kind":"daily","time":"03:30"}}]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/tasks", `{}`, 400, 403, 403, ""},
	}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			f.calls = nil
			rec := do(h, c.method, c.path, c.body, xrw, cookies[name])
			if rec.Code != want {
				t.Errorf("%s %s as %s = %d, want %d (%s)", c.method, c.path, name, rec.Code, want, rec.Body)
			}
			// The helper is reached when the role passes and the request is valid.
			reach := want == 200 || want == 202 || (want == 404 && name != "eva" && c.call != "")
			if name == "petr" && want == 403 || name == "eva" {
				reach = false
			}
			if reach && (len(f.calls) == 0 || f.calls[len(f.calls)-1] != c.call) {
				t.Errorf("%s %s as %s: calls %v, want %q", c.method, c.path, name, f.calls, c.call)
			}
			if !reach && len(f.calls) != 0 {
				t.Errorf("%s %s as %s: unexpected calls %v", c.method, c.path, name, f.calls)
			}
		}
	}
}

func TestTasksGetShape(t *testing.T) {
	h, _, cookies, argv, hist := newTaskServer(t)
	start := time.Date(2026, 1, 1, 3, 30, 0, 0, time.UTC)
	tasks.Record(hist, []string{"recycle"}, "recycle", tasks.Run{Start: start, End: start.Add(time.Minute), Result: "ok", Message: "Odstraněno 0 souborů (0,0 MB)."})
	rec := do(h, "GET", "/api/v1/tasks", "", xrw, cookies["petr"])
	var got struct {
		Tasks []struct {
			ID      string          `json:"id"`
			Running bool            `json:"running"`
			NextRun *string         `json:"nextRun"`
			LastRun *tasks.Run      `json:"lastRun"`
			Smart   json.RawMessage `json:"smart"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Tasks) != 1 {
		t.Fatalf("%v %s", err, rec.Body)
	}
	x := got.Tasks[0]
	if !x.Running || x.NextRun == nil || *x.NextRun != "2026-05-28T20:26:40Z" || x.LastRun == nil || x.LastRun.Result != "ok" || x.Smart != nil {
		t.Fatalf("%+v %s", x, rec.Body)
	}
	want := []string{
		"systemctl show --timestamp=unix --property=Id,NextElapseUSecRealtime -- nas-revival-task-recycle.timer",
		"systemctl show --property=Id,ActiveState -- nas-revival-task@recycle.service",
	}
	if strings.Join(*argv, "|") != strings.Join(want, "|") {
		t.Fatalf("argv %v", *argv)
	}
	rec = do(h, "GET", "/api/v1/tasks/recycle/history", "", xrw, cookies["petr"])
	if !strings.Contains(rec.Body.String(), `"runs":[{"start":"2026-01-01T03:30:00Z"`) {
		t.Fatalf("%s", rec.Body)
	}
}

func TestTasksDisabledHasNoNextRunAndEmptyHistory(t *testing.T) {
	h, f, cookies, _, _ := newTaskServer(t)
	f.list[0].Enabled = false
	rec := do(h, "GET", "/api/v1/tasks", "", xrw, cookies["jana"])
	if !strings.Contains(rec.Body.String(), `"nextRun":null`) || !strings.Contains(rec.Body.String(), `"lastRun":null`) {
		t.Fatalf("%s", rec.Body)
	}
	rec = do(h, "GET", "/api/v1/tasks/recycle/history", "", xrw, cookies["jana"])
	if strings.TrimSpace(rec.Body.String()) != `{"runs":[]}` {
		t.Fatalf("%s", rec.Body)
	}
}

func TestTasksUnavailable(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	c := loginAs(t, h, "jana")
	if rec := do(h, "GET", "/api/v1/tasks", "", xrw, c); rec.Code != 503 {
		t.Fatalf("%d", rec.Code)
	}
}

type fakeBackup struct {
	calls []string
	dir   string
}

func (f *fakeBackup) BackupKey(context.Context) (helper.BackupKey, error) {
	f.calls = append(f.calls, "key")
	return helper.BackupKey{PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:x"}, nil
}
func (f *fakeBackup) BackupHostScan(_ context.Context, host string, port int) (helper.HostScan, error) {
	f.calls = append(f.calls, "scan "+host)
	return helper.HostScan{Keys: []helper.HostKey{}}, nil
}
func (f *fakeBackup) BackupHostTrust(_ context.Context, host string, port int, typ, key string) error {
	f.calls = append(f.calls, "trust "+host)
	return nil
}
func (f *fakeBackup) BackupTest(_ context.Context, host string, port int, user, path string) (helper.BackupTestResult, error) {
	f.calls = append(f.calls, "test "+host)
	return helper.BackupTestResult{OK: true}, nil
}
func (f *fakeBackup) BackupShareGet(context.Context) (helper.BackupShareState, error) {
	return helper.BackupShareState{Name: "zalohy", Path: "/srv/data/versions"}, nil
}
func (f *fakeBackup) BackupShareSet(_ context.Context, en bool) (helper.BackupShareState, error) {
	f.calls = append(f.calls, "share")
	return helper.BackupShareState{Enabled: en, Name: "zalohy", Path: "/srv/data/versions"}, nil
}

func (f *fakeBackup) BackupVersions(_ context.Context, id string) (helper.BackupVersions, error) {
	f.calls = append(f.calls, "versions "+id)
	return helper.BackupVersions{Versions: []helper.BackupVersion{{Date: "2026-11-25"}}, Complete: true}, nil
}
func (f *fakeBackup) BackupBrowse(_ context.Context, id, date, path string, offset int) (helper.BrowseResult, error) {
	f.calls = append(f.calls, fmt.Sprintf("browse %s %s %q %d", id, date, path, offset))
	return helper.BrowseResult{Path: path, Entries: []helper.BrowseEntry{}}, nil
}
func (f *fakeBackup) BackupOpen(_ context.Context, id, date, path string) (*os.File, helper.BackupFileMeta, error) {
	f.calls = append(f.calls, "open "+path)
	p := filepath.Join(f.dir, "file")
	if err := os.WriteFile(p, []byte("dump-body"), 0o600); err != nil {
		return nil, helper.BackupFileMeta{}, err
	}
	fh, err := os.Open(p)
	return fh, helper.BackupFileMeta{Name: "Fakturace č.1.sql", Size: 9, Modified: time.Unix(1700000000, 0)}, err
}

func (f *fakeBackup) BackupRestoreStart(_ context.Context, req helper.RestoreRequest) (helper.RestoreStatus, error) {
	f.calls = append(f.calls, fmt.Sprintf("restore %s %s %q %s %q %q", req.ID, req.Date, req.Path, req.Share, req.Dir, req.Name))
	return helper.RestoreStatus{State: helper.RestoreScanning, Job: req.ID}, nil
}
func (f *fakeBackup) BackupRestoreStatus(context.Context) (helper.RestoreStatus, error) {
	return helper.RestoreStatus{State: helper.RestoreIdle}, nil
}
func (f *fakeBackup) BackupRestoreCancel(context.Context) (helper.RestoreStatus, error) {
	f.calls = append(f.calls, "restore cancel")
	return helper.RestoreStatus{State: helper.RestoreIdle}, nil
}

func TestBackupRestoreStartPassesTargetAndUser(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	fb := &fakeBackup{dir: t.TempDir()}
	s.Backup = fb
	ck := loginAs(t, h, "jana")
	rec := do(h, "POST", "/api/v1/backup/pull/versions/2026-11-25/restore",
		`{"path":"app/db","share":"data","dir":"obnova","name":"db (2026-11-25)"}`, xrw, ck)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"state":"scanning"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := fb.calls[len(fb.calls)-1]; got != `restore pull 2026-11-25 "app/db" data "obnova" "db (2026-11-25)"` {
		t.Errorf("call = %s", got)
	}
	if rec := do(h, "POST", "/api/v1/backup/pull/versions/2026-11-25/restore", `{"path":"x","extra":1}`, xrw, ck); rec.Code != 400 {
		t.Errorf("unknown field: %d", rec.Code)
	}
}

func TestBackupFileDownload(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	fb := &fakeBackup{dir: t.TempDir()}
	s.Backup = fb
	ck := loginAs(t, h, "jana")
	rec := do(h, "GET", "/api/v1/backup/pull/versions/2026-11-25/file?path=app%2FFakturace%20%C4%8D.1.sql", "", xrw, ck)
	if rec.Code != 200 || rec.Body.String() != "dump-body" {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
	cd := rec.Header().Get("Content-Disposition")
	if cd != `attachment; filename="Fakturace _.1.sql"; filename*=UTF-8''Fakturace%20%C4%8D.1.sql` {
		t.Errorf("Content-Disposition = %s", cd)
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", rec.Header())
	}
	if fb.calls[len(fb.calls)-1] != "open app/Fakturace č.1.sql" {
		t.Errorf("calls = %v", fb.calls)
	}
}

func TestBackupRoutesRoles(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	fb := &fakeBackup{dir: t.TempDir()}
	s.Backup = fb
	ck := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	body := `{"host":"192.0.2.10","port":22}`
	test := `{"host":"192.0.2.10","port":22,"user":"root","path":"nas-export"}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
	}{
		{"GET", "/api/v1/backup/key", "", 200, 403, 403},
		{"POST", "/api/v1/backup/hostkey/scan", body, 200, 403, 403},
		{"POST", "/api/v1/backup/hostkey/trust", `{"host":"192.0.2.10","port":22,"type":"ssh-ed25519","key":"AAAA"}`, 200, 403, 403},
		{"POST", "/api/v1/backup/test", test, 200, 403, 403},
		{"GET", "/api/v1/backup/share", "", 200, 200, 403},
		{"PUT", "/api/v1/backup/share", `{"enabled":true}`, 200, 403, 403},
		// option injection never reaches the helper
		{"POST", "/api/v1/backup/hostkey/scan", `{"host":"-oProxyCommand=x","port":22}`, 400, 403, 403},
		{"POST", "/api/v1/backup/test", `{"host":"192.0.2.10","port":22,"user":"root","path":"../x"}`, 400, 403, 403},
		{"POST", "/api/v1/backup/test", `{"host":"192.0.2.10","port":22,"user":"-x","path":"a"}`, 400, 403, 403},
		{"POST", "/api/v1/backup/hostkey/scan", `{"host":"a","port":99999}`, 400, 403, 403},
		{"PUT", "/api/v1/backup/share", `{}`, 400, 403, 403},
		{"GET", "/api/v1/backup/pull/versions", "", 200, 403, 403},
		{"GET", "/api/v1/backup/pull/versions/2026-11-25/browse?path=app&offset=200", "", 200, 403, 403},
		{"GET", "/api/v1/backup/pull/versions/2026-11-25/file?path=app/x", "", 200, 403, 403},
		{"GET", "/api/v1/backup/-x/versions", "", 400, 403, 403},
		{"GET", "/api/v1/backup/pull/versions/2026-11-25/browse?offset=-1", "", 400, 403, 403},
		{"POST", "/api/v1/backup/pull/versions/2026-11-25/restore", `{"path":"app","share":"data","dir":"","name":"app"}`, 202, 403, 403},
		{"POST", "/api/v1/backup/-x/versions/2026-11-25/restore", `{"path":"app","share":"data","dir":"","name":"app"}`, 400, 403, 403},
		{"GET", "/api/v1/backup/restore", "", 200, 403, 403},
		{"POST", "/api/v1/backup/restore/cancel", "", 200, 403, 403},
	}
	for i, c := range cases {
		for who, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			rec := do(h, c.method, c.path, c.body, xrw, ck[who])
			if rec.Code != want {
				t.Errorf("case %d %s %s as %s: %d want %d (%s)", i, c.method, c.path, who, rec.Code, want, rec.Body)
			}
		}
	}
	for _, c := range fb.calls {
		if strings.Contains(c, "-o") {
			t.Fatalf("option host reached the helper: %s", c)
		}
	}
}

func TestBackupTaskViewUsesBackupHistoryAndService(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	dir := t.TempDir()
	bt := tasks.Task{ID: "pull", Name: "Záloha", Type: tasks.TypeBackup, Enabled: true,
		Schedule: tasks.Schedule{Kind: tasks.KindDaily, Time: "06:45"},
		Backup:   &tasks.BackupParams{Host: "192.0.2.10", Port: 22, User: "root", Paths: []string{"a"}, KeepDays: 35}}
	s.Tasks = &fakeTasks{list: []tasks.Task{bt}}
	bh := filepath.Join(dir, "backup-history.json")
	now := time.Now().UTC().Truncate(time.Second)
	tasks.RecordMode(bh, 0o640, []string{"pull"}, "pull", tasks.Run{Start: now, End: now, Result: tasks.ResultOK, Message: "ok", Bytes: 1234, UsedBytes: 5678})
	var argv []string
	s.TaskStatus = TaskStatus{HistoryPath: filepath.Join(dir, "missing.json"), BackupHistoryPath: bh,
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			argv = append(argv, strings.Join(args, " "))
			if args[1] == "--timestamp=unix" {
				return nil, nil
			}
			return []byte("Id=nas-revival-backup@pull.service\nActiveState=active\n"), nil
		}}
	c := loginAs(t, h, "jana")
	rec := do(h, "GET", "/api/v1/tasks", "", xrw, c)
	var out struct {
		Tasks []struct {
			ID      string `json:"id"`
			Running bool   `json:"running"`
			LastRun *struct {
				Bytes     int64 `json:"bytes"`
				UsedBytes int64 `json:"usedBytes"`
			} `json:"lastRun"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Tasks) != 1 {
		t.Fatalf("%v %s", err, rec.Body)
	}
	tk := out.Tasks[0]
	if !tk.Running || tk.LastRun == nil || tk.LastRun.Bytes != 1234 || tk.LastRun.UsedBytes != 5678 {
		t.Fatalf("%+v", tk)
	}
	if !strings.Contains(strings.Join(argv, "|"), "nas-revival-backup@pull.service") {
		t.Fatal(argv)
	}
	rec = do(h, "GET", "/api/v1/tasks/pull/history", "", xrw, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"bytes":1234`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// missing history file -> null lastRun
	s.TaskStatus.BackupHistoryPath = filepath.Join(dir, "none.json")
	rec = do(h, "GET", "/api/v1/tasks", "", xrw, c)
	if !strings.Contains(rec.Body.String(), `"lastRun":null`) {
		t.Fatal(rec.Body)
	}
}
