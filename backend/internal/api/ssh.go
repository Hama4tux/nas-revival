package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/sshd"
)

// SshAdmin is implemented by the helper client (ops ssh.*).
type SshAdmin interface {
	SshGet(ctx context.Context) (helper.SshService, error)
	SshSet(ctx context.Context, s sshd.Settings) (helper.SshService, error)
	SshKeys(ctx context.Context) (helper.SshKeys, error)
	SshKeyAdd(ctx context.Context, name, key string) (helper.SshKeys, error)
	SshKeyRemove(ctx context.Context, name, fingerprint string) (helper.SshKeys, error)
}

const (
	sshBodyLimit    = 2 << 10
	sshKeyBodyLimit = sshd.MaxKeyLen + 1<<10
)

func (s *Server) registerSsh(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/services/ssh", s.requireSession(s.sshGet))
	api.HandleFunc("PUT /api/v1/services/ssh", s.requireSession(s.sshSet))
	api.HandleFunc("GET /api/v1/services/ssh/keys", s.requireSession(s.sshKeys))
	api.HandleFunc("POST /api/v1/services/ssh/keys/{name}", s.requireSession(s.sshKeyAdd))
	api.HandleFunc("DELETE /api/v1/services/ssh/keys/{name}", s.requireSession(s.sshKeyRemove))
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
	auditOK(r, "ssh.settings", "", "Změněno nastavení SSH (port "+strconv.Itoa(body.Port)+")")
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) sshKeys(w http.ResponseWriter, r *http.Request) {
	if !s.sshReady(w) {
		return
	}
	k, err := s.Ssh.SshKeys(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// sshKeyAdd checks the key here too, so a malformed paste never reaches the helper.
func (s *Server) sshKeyAdd(w http.ResponseWriter, r *http.Request) {
	if !s.sshReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if !decodeBody(w, r, sshKeyBodyLimit, &body) {
		return
	}
	k, _, err := sshd.ParseKey(body.Key)
	if err != nil {
		writeHelperError(w, helper.SshFieldErr(err))
		return
	}
	l, err := s.Ssh.SshKeyAdd(r.Context(), name, body.Key)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "ssh.key.add", name, "Přidán SSH klíč účtu "+name+" ("+k.Fingerprint+")")
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) sshKeyRemove(w http.ResponseWriter, r *http.Request) {
	if !s.sshReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decodeBody(w, r, sshBodyLimit, &body) {
		return
	}
	if !strings.HasPrefix(body.Fingerprint, "SHA256:") || len(body.Fingerprint) > 100 {
		writeInvalid(w, "fingerprint", "Neplatný otisk klíče.")
		return
	}
	l, err := s.Ssh.SshKeyRemove(r.Context(), name, body.Fingerprint)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "ssh.key.remove", name, "Odebrán SSH klíč účtu "+name+" ("+body.Fingerprint+")")
	writeJSON(w, http.StatusOK, l)
}
