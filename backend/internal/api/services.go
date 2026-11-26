package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/services"
)

// ServiceAdmin is implemented by the helper client (ops service.*, journal.unit).
type ServiceAdmin interface {
	ServiceList(ctx context.Context) (helper.ServiceList, error)
	ServiceAction(ctx context.Context, name, action string) (helper.Service, error)
	ServiceLog(ctx context.Context, name string, lines int) (helper.ServiceLog, error)
}

const defaultLogLines = 100

func (s *Server) registerServices(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/services", s.requireSession(s.servicesList))
	api.HandleFunc("POST /api/v1/services/{name}/{action}", s.requireSession(s.serviceAction))
	// Logs can contain secrets of the service (SVC-12): administrators only.
	api.HandleFunc("GET /api/v1/services/{name}/log", s.requireRole(accounts.RoleAdmin, s.serviceLog))
}

func (s *Server) servicesReady(w http.ResponseWriter) bool {
	if s.Services == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu služeb není dostupná.")
		return false
	}
	return true
}

// serviceName accepts only catalog units; anything else is 404 before the
// helper is asked.
func serviceName(w http.ResponseWriter, r *http.Request) (services.Def, bool) {
	d, ok := services.Lookup(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, helper.CodeNotFound, "Tato služba se nespravuje.")
	}
	return d, ok
}

func (s *Server) servicesList(w http.ResponseWriter, r *http.Request) {
	if !s.servicesReady(w) {
		return
	}
	l, err := s.Services.ServiceList(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	if !s.servicesReady(w) {
		return
	}
	d, ok := serviceName(w, r)
	if !ok {
		return
	}
	action := r.PathValue("action")
	if !d.Allows(action) {
		writeInvalid(w, "action", "Tuto akci služba nepodporuje.")
		return
	}
	svc, err := s.Services.ServiceAction(r.Context(), d.Name, action)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("service "+action, "unit", d.Name, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) serviceLog(w http.ResponseWriter, r *http.Request) {
	if !s.servicesReady(w) {
		return
	}
	d, ok := serviceName(w, r)
	if !ok {
		return
	}
	lines := defaultLogLines
	if v := r.URL.Query().Get("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > services.MaxLogLines {
			writeInvalid(w, "lines", "Počet řádků musí být 1 až 200.")
			return
		}
		lines = n
	}
	l, err := s.Services.ServiceLog(r.Context(), d.Name, lines)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
