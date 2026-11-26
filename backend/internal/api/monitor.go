package api

import (
	"net/http"
	"strconv"

	"nas-revival/backend/internal/monitor"
)

// MonitorSource is the Resource Monitor history (monitor.Monitor).
type MonitorSource interface {
	History(sinceMs int64) monitor.History
}

func (s *Server) registerMonitor(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/monitor", s.requireSession(s.monitorHistory))
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
