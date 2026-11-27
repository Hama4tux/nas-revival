package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/helper"
)

// ConfigHistory is implemented by the helper client (ops config.list,
// config.get, config.rollback).
type ConfigHistory interface {
	ConfigHistory(ctx context.Context) (config.History, error)
	ConfigVersion(ctx context.Context, id int) (config.Detail, error)
	ConfigRollback(ctx context.Context, id int, takeover bool) (helper.ConfigRollbackResult, error)
	ConfigExport(ctx context.Context) (helper.ConfigArchive, error)
	ConfigImportPreview(ctx context.Context, archive json.RawMessage) (helper.ConfigImportPreview, error)
	ConfigImport(ctx context.Context, archive json.RawMessage, takeover bool) (helper.ConfigImportResult, error)
}

func (s *Server) registerConfig(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/config/history", s.requireSession(s.configHistory))
	api.HandleFunc("GET /api/v1/config/history/{n}", s.requireSession(s.configVersion))
	api.HandleFunc("POST /api/v1/config/rollback/{n}", s.requireSession(s.configRollback))
	// The export lists accounts, shares and NFS clients: administrators only,
	// although it is a GET.
	api.HandleFunc("GET /api/v1/config/export", s.requireRole(accounts.RoleAdmin, s.configExport))
	api.HandleFunc("POST /api/v1/config/import/preview", s.requireSession(s.configImportPreview))
	api.HandleFunc("POST /api/v1/config/import", s.requireSession(s.configImport))
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

var exportNameRe = regexp.MustCompile(`[^A-Za-z0-9-]`)

// configExport downloads the export file (CFG-05).
func (s *Server) configExport(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	a, err := s.Config.ConfigExport(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	host := exportNameRe.ReplaceAllString(a.Hostname, "")
	if host == "" {
		host = "nas"
	}
	name := "nas-revival-config-" + host + "-" + a.Created.Local().Format("20060102-1504") + ".json"
	w.Header().Set("Content-Disposition", attachment(name))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, a)
}

type importBody struct {
	Archive  json.RawMessage `json:"archive"`
	Takeover bool            `json:"takeover"`
}

func decodeImport(w http.ResponseWriter, r *http.Request) (importBody, bool) {
	var b importBody
	if !decodeBody(w, r, helper.MaxArchive+1024, &b) {
		return b, false
	}
	if len(b.Archive) == 0 || b.Archive[0] != '{' {
		writeInvalid(w, "archive", "Soubor není export konfigurace NAS Revival.")
		return b, false
	}
	return b, true
}

func (s *Server) configImportPreview(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	b, ok := decodeImport(w, r)
	if !ok {
		return
	}
	p, err := s.Config.ConfigImportPreview(r.Context(), b.Archive)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) configImport(w http.ResponseWriter, r *http.Request) {
	if !s.configReady(w) {
		return
	}
	b, ok := decodeImport(w, r)
	if !ok {
		return
	}
	res, err := s.Config.ConfigImport(r.Context(), b.Archive, b.Takeover)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
