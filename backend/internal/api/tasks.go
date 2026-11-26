package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tasks"
)

// TaskAdmin is implemented by the helper client (ops task.*).
type TaskAdmin interface {
	TaskGet(ctx context.Context) (helper.TaskList, error)
	TaskSet(ctx context.Context, l []tasks.Task) (helper.TaskList, error)
	TaskRun(ctx context.Context, id string) error
	TaskStop(ctx context.Context, id string) error
}

// TaskStatus reads timers and services from systemd and the run history.
type TaskStatus struct {
	// Run executes a fixed systemctl command; nil uses helper.ExecRunner.
	Run         helper.Runner
	HistoryPath string
	// BackupHistoryPath is the history of the backup-pull tasks (written by
	// the nas-backup account, group-readable); empty disables it.
	BackupHistoryPath string
}

const tasksBodyLimit = 32 << 10

type taskBody struct {
	Tasks []tasks.Task `json:"tasks"`
}

type taskView struct {
	tasks.Task
	Running bool       `json:"running"`
	NextRun *time.Time `json:"nextRun"`
	LastRun *tasks.Run `json:"lastRun"`
}

func (s *Server) registerTasks(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/tasks", s.requireSession(s.tasksGet))
	api.HandleFunc("PUT /api/v1/tasks", s.requireSession(s.tasksPut))
	api.HandleFunc("POST /api/v1/tasks/{id}/run", s.requireSession(s.tasksControl))
	api.HandleFunc("POST /api/v1/tasks/{id}/stop", s.requireSession(s.tasksControl))
	api.HandleFunc("GET /api/v1/tasks/{id}/history", s.requireSession(s.tasksHistory))
}

func (s *Server) tasksReady(w http.ResponseWriter) bool {
	if s.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro plánované úlohy není dostupná.")
		return false
	}
	return true
}

func (s *Server) tasksGet(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	l, err := s.Tasks.TaskGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.taskViews(r.Context(), l.Tasks))
}

func (s *Server) tasksPut(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	var body taskBody
	if !decodeBody(w, r, tasksBodyLimit, &body) {
		return
	}
	if body.Tasks == nil {
		writeInvalid(w, "tasks", "Chybí seznam úloh.")
		return
	}
	l, err := helper.ValidateTasks(body.Tasks)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	res, err := s.Tasks.TaskSet(r.Context(), l)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("tasks changed", "count", len(res.Tasks), "by", sessionUser(r))
	writeJSON(w, http.StatusOK, s.taskViews(r.Context(), res.Tasks))
}

func (s *Server) tasksControl(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	id := r.PathValue("id")
	if !tasks.ValidID(id) {
		writeError(w, http.StatusNotFound, helper.CodeNotFound, "Úloha neexistuje.")
		return
	}
	var err error
	verb := "run"
	if strings.HasSuffix(r.URL.Path, "/stop") {
		verb = "stop"
		err = s.Tasks.TaskStop(r.Context(), id)
	} else {
		err = s.Tasks.TaskRun(r.Context(), id)
	}
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("task "+verb, "id", id, "by", sessionUser(r))
	writeJSON(w, http.StatusAccepted, struct{}{})
}

func (s *Server) tasksHistory(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	id := r.PathValue("id")
	l, err := s.Tasks.TaskGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	t, ok := tasks.Find(l.Tasks, id)
	if !ok {
		writeError(w, http.StatusNotFound, helper.CodeNotFound, "Úloha neexistuje.")
		return
	}
	histPath := s.TaskStatus.HistoryPath
	if t.Type == tasks.TypeBackup {
		histPath = s.TaskStatus.BackupHistoryPath
	}
	runs := []tasks.Run{}
	if histPath == "" {
	} else if h, err := tasks.LoadHistory(histPath); err != nil {
		slog.Warn("task history unreadable", "err", err)
	} else if rs := h.Runs[id]; rs != nil {
		runs = rs
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) taskViews(ctx context.Context, l []tasks.Task) map[string]any {
	st := s.TaskStatus
	run := st.Run
	if run == nil {
		run = helper.ExecRunner
	}
	next, running := map[string]time.Time{}, map[string]bool{}
	if len(l) > 0 {
		timers := []string{"show", "--timestamp=unix", "--property=Id,NextElapseUSecRealtime", "--"}
		services := []string{"show", "--property=Id,ActiveState", "--"}
		for _, t := range l {
			timers = append(timers, tasks.TimerName(t.ID))
			services = append(services, tasks.ServiceNameFor(t))
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := run(cctx, "systemctl", timers...); err == nil {
			for _, b := range parseShowBlocks(string(out)) {
				id := unitTaskID(b["Id"], "nas-revival-task-", ".timer")
				if sec, err := strconv.ParseInt(strings.TrimPrefix(b["NextElapseUSecRealtime"], "@"), 10, 64); err == nil && sec > 0 && id != "" {
					next[id] = time.Unix(sec, 0).UTC()
				}
			}
		}
		if out, err := run(cctx, "systemctl", services...); err == nil {
			for _, b := range parseShowBlocks(string(out)) {
				id := unitTaskID(b["Id"], "nas-revival-task@", ".service")
				if id == "" {
					id = unitTaskID(b["Id"], "nas-revival-backup@", ".service")
				}
				if id != "" {
					running[id] = b["ActiveState"] == "activating" || b["ActiveState"] == "active"
				}
			}
		}
	}
	hist := loadHistory(st.HistoryPath)
	bhist := loadHistory(st.BackupHistoryPath)
	views := make([]taskView, 0, len(l))
	for _, t := range l {
		v := taskView{Task: t, Running: running[t.ID]}
		if n, ok := next[t.ID]; ok && t.Enabled {
			n := n
			v.NextRun = &n
		}
		h := hist
		if t.Type == tasks.TypeBackup {
			h = bhist
		}
		if rs := h.Runs[t.ID]; len(rs) > 0 {
			last := rs[0]
			v.LastRun = &last
		}
		views = append(views, v)
	}
	return map[string]any{"tasks": views}
}

func loadHistory(path string) tasks.History {
	if path == "" {
		return tasks.History{}
	}
	h, err := tasks.LoadHistory(path)
	if err != nil {
		slog.Warn("task history unreadable", "err", err)
	}
	return h
}

func unitTaskID(unit, prefix, suffix string) string {
	if !strings.HasPrefix(unit, prefix) || !strings.HasSuffix(unit, suffix) {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(unit, prefix), suffix)
	if !tasks.ValidID(id) {
		return ""
	}
	return id
}

// parseShowBlocks splits `systemctl show` output of several units (blocks
// separated by an empty line) into key/value maps.
func parseShowBlocks(out string) []map[string]string {
	var res []map[string]string
	cur := map[string]string{}
	flush := func() {
		if len(cur) > 0 {
			res = append(res, cur)
			cur = map[string]string{}
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur[k] = v
		}
	}
	flush()
	return res
}
