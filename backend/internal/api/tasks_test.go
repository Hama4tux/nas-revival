package api

import (
	"context"
	"encoding/json"
	"net/http"
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
