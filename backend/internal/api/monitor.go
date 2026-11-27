package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/monitor"
	"nas-revival/backend/internal/procs"
)

// MonitorSource is the Resource Monitor history (monitor.Monitor).
type MonitorSource interface {
	History(sinceMs int64) monitor.History
}

// ProcessSource reads the process list (procs.Reader).
type ProcessSource interface {
	List(ctx context.Context) (procs.List, error)
}

// ProcessKiller is the helper client (process.kill).
type ProcessKiller interface {
	ProcessKill(ctx context.Context, pid int, start uint64, force bool) (helper.ProcessKillResult, error)
}

func (s *Server) registerMonitor(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/monitor", s.requireSession(s.monitorHistory))
	api.HandleFunc("GET /api/v1/monitor/processes", s.requireSession(s.processList))
	// POST: administrators only (requireSession).
	api.HandleFunc("POST /api/v1/monitor/processes/{pid}/kill", s.requireSession(s.processKill))
}

// processList walks /proc only when asked (the page polls while open); the
// reader reuses one reading for a few seconds.
func (s *Server) processList(w http.ResponseWriter, r *http.Request) {
	if s.Processes == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Seznam procesů není dostupný.")
		return
	}
	l, err := s.Processes.List(r.Context())
	if err != nil {
		slog.Error("process list failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Seznam procesů se nepodařilo načíst.")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) processKill(w http.ResponseWriter, r *http.Request) {
	if s.ProcessKill == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro ukončení procesu není dostupná.")
		return
	}
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || pid < 1 {
		writeInvalid(w, "pid", "Neplatné číslo procesu.")
		return
	}
	var body struct {
		Start *uint64 `json:"start"` // required; 0 is a real value (PID 1)
		Force bool    `json:"force"`
	}
	if !decodeBody(w, r, 1<<10, &body) {
		return
	}
	if body.Start == nil {
		writeInvalid(w, "start", "Chybí čas spuštění procesu.")
		return
	}
	res, err := s.ProcessKill.ProcessKill(r.Context(), pid, *body.Start, body.Force)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "process.signal", res.Name, "Procesu "+res.Name+" (PID "+strconv.Itoa(pid)+") poslán signál "+res.Signal)
	writeJSON(w, http.StatusOK, res)
}

// monitorHistory returns the last hour, or with ?since=<Unix ms> only the
// newer samples, so a polling page downloads one sample per poll.
func (s *Server) monitorHistory(w http.ResponseWriter, r *http.Request) {
	if s.Monitor == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Monitor prostředků není dostupný.")
		return
	}
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_since", "Neplatný parametr since.")
			return
		}
		since = n
	}
	writeJSON(w, http.StatusOK, s.Monitor.History(since))
}
