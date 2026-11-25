package api

import (
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/system"
)

// SystemSource provides rarely changing identification of the device.
type SystemSource interface {
	Info() (system.Info, error)
	Capabilities() system.Capabilities
}

func (s *Server) registerSystem(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/system/info", s.requireSession(s.systemInfo))
	api.HandleFunc("GET /api/v1/system/capabilities", s.requireSession(s.systemCapabilities))
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	if s.System == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Informace o systému nejsou dostupné.")
		return
	}
	in, err := s.System.Info()
	if err != nil {
		slog.Error("system info failed", "err", err)
		writeError(w, http.StatusInternalServerError, "system_info_unavailable", "Informace o systému nelze načíst.")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) systemCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.System == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Informace o systému nejsou dostupné.")
		return
	}
	writeJSON(w, http.StatusOK, s.System.Capabilities())
}
