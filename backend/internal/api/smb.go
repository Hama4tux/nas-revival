package api

import (
	"context"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/smb"
)

// SmbAdmin is implemented by the helper client (ops smb.*).
type SmbAdmin interface {
	SmbGet(ctx context.Context) (helper.SmbService, error)
	SmbSet(ctx context.Context, s smb.Settings) (helper.SmbService, error)
	SmbStatus(ctx context.Context) (smb.Status, error)
}

const smbBodyLimit = 2 << 10

func (s *Server) registerSmb(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/services/smb", s.requireSession(s.smbGet))
	api.HandleFunc("PUT /api/v1/services/smb", s.requireSession(s.smbSet))
	// Who is connected from where and which files are open (SMB-02):
	// administrators only.
	api.HandleFunc("GET /api/v1/services/smb/status", s.requireRole(accounts.RoleAdmin, s.smbStatus))
}

func (s *Server) smbReady(w http.ResponseWriter) bool {
	if s.Smb == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu SMB není dostupná.")
		return false
	}
	return true
}

func (s *Server) smbGet(w http.ResponseWriter, r *http.Request) {
	if !s.smbReady(w) {
		return
	}
	st, err := s.Smb.SmbGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) smbSet(w http.ResponseWriter, r *http.Request) {
	if !s.smbReady(w) {
		return
	}
	var body smb.Settings
	if !decodeBody(w, r, smbBodyLimit, &body) {
		return
	}
	body, err := helper.ValidateSmb(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	st, err := s.Smb.SmbSet(r.Context(), body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("smb settings changed", "enabled", body.Enabled, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) smbStatus(w http.ResponseWriter, r *http.Request) {
	if !s.smbReady(w) {
		return
	}
	st, err := s.Smb.SmbStatus(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
