package api

import (
	"context"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/services"
)

// LogAdmin is implemented by the helper client (journal.query, journal.retention.*).
type LogAdmin interface {
	LogQuery(ctx context.Context, q services.Query) (helper.JournalResult, error)
	LogRetentionGet(ctx context.Context) (helper.JournalRetention, error)
	LogRetentionSet(ctx context.Context, s helper.JournalRetention) (helper.JournalRetention, error)
}

// Log Center (W5.3, ADR 0015). Kernel and auth logs name users and clients,
// so the whole app needs at least the read-only administrator role.
func (s *Server) registerLogs(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/logs", s.requireRole(accounts.RoleViewer, s.logsQuery))
	api.HandleFunc("GET /api/v1/logs/retention", s.requireRole(accounts.RoleViewer, s.logsRetentionGet))
	api.HandleFunc("PUT /api/v1/logs/retention", s.requireRole(accounts.RoleAdmin, s.logsRetentionPut))
}

func (s *Server) logsQuery(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		unavailable(w)
		return
	}
	p := r.URL.Query()
	q := services.Query{Category: p.Get("category"), Unit: p.Get("unit"), Since: p.Get("since"),
		Until: p.Get("until"), Text: p.Get("text")}
	if v := p.Get("priority"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeInvalid(w, "priority", "Závažnost musí být 0 až 7.")
			return
		}
		q.Priority = &n
	}
	if v := p.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n == 0 {
			writeInvalid(w, "limit", "Počet záznamů musí být 1 až 1000.")
			return
		}
		q.Limit = n
	}
	res, err := s.Logs.LogQuery(r.Context(), q)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	if res.Entries == nil {
		res.Entries = []services.QueryEntry{}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) logsRetentionGet(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		unavailable(w)
		return
	}
	res, err := s.Logs.LogRetentionGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) logsRetentionPut(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		unavailable(w)
		return
	}
	var body struct {
		MaxUse       string `json:"maxUse"`
		MaxRetention string `json:"maxRetention"`
	}
	if !decodeBody(w, r, settingsBodyLimit, &body) {
		return
	}
	res, err := s.Logs.LogRetentionSet(r.Context(), helper.JournalRetention{MaxUse: body.MaxUse, MaxRetention: body.MaxRetention})
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "logs.retention", "", "Změněno uchování záznamů: velikost "+res.MaxUse+", doba "+res.MaxRetention)
	writeJSON(w, http.StatusOK, res)
}
