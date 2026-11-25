package api

import (
	"context"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/helper"
)

// ConfigHistory is implemented by the helper client (ops config.list,
// config.get, config.rollback).
type ConfigHistory interface {
	ConfigHistory(ctx context.Context) (config.History, error)
	ConfigVersion(ctx context.Context, id int) (config.Detail, error)
	ConfigRollback(ctx context.Context, id int, takeover bool) (helper.ConfigRollbackResult, error)
}

func (s *Server) registerConfig(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/config/history", s.requireSession(s.configHistory))
	api.HandleFunc("GET /api/v1/config/history/{n}", s.requireSession(s.configVersion))
	api.HandleFunc("POST /api/v1/config/rollback/{n}", s.requireSession(s.configRollback))
}

func (s *Server) configReady(w http.ResponseWriter) bool {
	if s.Config == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return false
	}
	return true
}

// versionID parses {n}: a positive decimal without sign or leading zeros.
func versionID(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.PathValue("n")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || strconv.Itoa(n) != raw {
		writeInvalid(w, "n", "Neplatné číslo verze.")
		return 0, false
	}
	return n, true
}

func (s *Server) configHistory(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	h, err := s.Config.ConfigHistory(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) configVersion(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	n, ok := versionID(w, r)
	if !ok {
		return
	}
	d, err := s.Config.ConfigVersion(r.Context(), n)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) configRollback(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	n, ok := versionID(w, r)
	if !ok {
		return
	}
	var body struct {
		Takeover bool `json:"takeover"`
	}
	if !decodeBody(w, r, 1024, &body) {
		return
	}
	res, err := s.Config.ConfigRollback(r.Context(), n, body.Takeover)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
