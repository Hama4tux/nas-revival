package api

import (
	"context"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/sshd"
)

// SshAdmin is implemented by the helper client (ops ssh.*).
type SshAdmin interface {
	SshGet(ctx context.Context) (helper.SshService, error)
	SshSet(ctx context.Context, s sshd.Settings) (helper.SshService, error)
}

const sshBodyLimit = 2 << 10

func (s *Server) registerSsh(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/services/ssh", s.requireSession(s.sshGet))
	api.HandleFunc("PUT /api/v1/services/ssh", s.requireSession(s.sshSet))
}

func (s *Server) sshReady(w http.ResponseWriter) bool {
	if s.Ssh == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu SSH není dostupná.")
		return false
	}
	return true
}

func (s *Server) sshGet(w http.ResponseWriter, r *http.Request) {
	if !s.sshReady(w) {
		return
	}
	st, err := s.Ssh.SshGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) sshSet(w http.ResponseWriter, r *http.Request) {
	if !s.sshReady(w) {
		return
	}
	var body sshd.Settings
	if !decodeBody(w, r, sshBodyLimit, &body) {
		return
	}
	body, err := helper.ValidateSsh(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	st, err := s.Ssh.SshSet(r.Context(), body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("ssh settings changed", "port", body.Port, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, st)
}
