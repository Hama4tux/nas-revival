package api

import (
	"context"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/nfs"
)

// NfsAdmin is implemented by the helper client (ops nfs.*).
type NfsAdmin interface {
	NfsGet(ctx context.Context) (helper.NfsService, error)
	NfsSet(ctx context.Context, s nfs.Settings) (helper.NfsService, error)
	NfsStatus(ctx context.Context) (helper.NfsStatus, error)
}

const nfsBodyLimit = 32 << 10

func (s *Server) registerNfs(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/services/nfs", s.requireSession(s.nfsGet))
	api.HandleFunc("PUT /api/v1/services/nfs", s.requireSession(s.nfsSet))
	// Which client addresses hold a lease: administrators only, like SMB.
	api.HandleFunc("GET /api/v1/services/nfs/status", s.requireRole(accounts.RoleAdmin, s.nfsStatus))
}

func (s *Server) nfsReady(w http.ResponseWriter) bool {
	if s.Nfs == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu NFS není dostupná.")
		return false
	}
	return true
}

func (s *Server) nfsGet(w http.ResponseWriter, r *http.Request) {
	if !s.nfsReady(w) {
		return
	}
	st, err := s.Nfs.NfsGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) nfsSet(w http.ResponseWriter, r *http.Request) {
	if !s.nfsReady(w) {
		return
	}
	var body nfs.Settings
	if !decodeBody(w, r, nfsBodyLimit, &body) {
		return
	}
	body, err := helper.ValidateNfs(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	st, err := s.Nfs.NfsSet(r.Context(), body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("nfs settings changed", "enabled", body.Enabled, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) nfsStatus(w http.ResponseWriter, r *http.Request) {
	if !s.nfsReady(w) {
		return
	}
	st, err := s.Nfs.NfsStatus(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
