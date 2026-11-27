package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/security"
)

// Security Center (W5.1): web sessions with forced logout (SEC-10) and the
// Security Advisor (SEC-13).
func (s *Server) registerSecurity(api *http.ServeMux) {
	// Sessions show other people's addresses and browsers: administrators only.
	api.HandleFunc("GET /api/v1/security/sessions", s.requireRole(accounts.RoleAdmin, s.securitySessions))
	api.HandleFunc("DELETE /api/v1/security/sessions/{id}", s.requireRole(accounts.RoleAdmin, s.securityRevoke))
	api.HandleFunc("POST /api/v1/security/sessions/revoke-others", s.requireRole(accounts.RoleAdmin, s.securityRevokeOthers))
	api.HandleFunc("GET /api/v1/security/advisor", s.requireSession(s.securityAdvisor))
	s.registerFirewall(api)
}

type sessionView struct {
	auth.SessionInfo
	Current bool `json:"current"`
}

func (s *Server) securitySessions(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	own := s.Sessions.ID(sc.token)
	list := s.Sessions.List()
	out := make([]sessionView, len(list))
	for i, v := range list {
		out[i] = sessionView{SessionInfo: v, Current: v.ID == own}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out,
		"idleTimeoutSeconds": int(auth.IdleTimeout.Seconds()), "absoluteTimeoutSeconds": int(auth.AbsoluteTimeout.Seconds())})
}

func (s *Server) securityRevoke(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	id := r.PathValue("id")
	own := s.Sessions.ID(sc.token) == id
	user, ok := s.Sessions.Revoke(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Relace už neexistuje.")
		return
	}
	auditOK(r, "session.revoke", user, "Ukončena relace uživatele "+user)
	if own {
		clearCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) securityRevokeOthers(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	n := s.Sessions.RevokeOthers(sc.token)
	auditOK(r, "session.revoke_others", sc.username, "Ukončeny ostatní relace ("+strconv.Itoa(n)+")")
	writeJSON(w, http.StatusOK, map[string]int{"revoked": n})
}

// securityAdvisor collects the inputs one by one; a backend that fails or is
// missing turns only its checks into "unknown".
func (s *Server) securityAdvisor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in := security.Input{Now: time.Now(), BlockedClients: s.Limiter.BlockedCount()}
	if s.Ssh != nil {
		if v, err := s.Ssh.SshGet(ctx); err == nil {
			in.SSH = &security.SSH{Running: v.Running, RootLogin: v.Effective.RootLogin,
				PasswordAuth: v.Effective.PasswordAuth, SftpGroups: len(v.Settings.SftpGroups)}
		} else {
			slog.Warn("advisor: ssh", "err", err)
		}
	}
	if s.Smb != nil {
		if v, err := s.Smb.SmbGet(ctx); err == nil {
			in.SMB = &security.SMB{Enabled: v.Installed && v.Settings.Enabled, MinProtocol: v.Settings.MinProtocol}
		} else {
			slog.Warn("advisor: smb", "err", err)
		}
	}
	if s.Nfs != nil {
		if v, err := s.Nfs.NfsGet(ctx); err == nil {
			in.NFS = &security.NFS{Enabled: v.Installed && v.Settings.Enabled, Exports: v.Settings.Exports}
		} else {
			slog.Warn("advisor: nfs", "err", err)
		}
	}
	if s.TLS != nil {
		info := s.TLS.Info()
		in.TLS = &info
	}
	if s.Users != nil {
		if v, err := s.Users.UserList(ctx); err == nil {
			a := &security.Accounts{RecoveryAdmin: s.RecoveryAdmin != nil && s.RecoveryAdmin()}
			for _, u := range v.Users {
				if u.Role == accounts.RoleAdmin && !u.Disabled {
					a.NASAdmins++
				}
			}
			in.Accounts = a
		} else {
			slog.Warn("advisor: users", "err", err)
		}
	}
	if s.Firewall != nil {
		if v, err := s.Firewall.FirewallGet(ctx); err == nil {
			in.Firewall = &security.Firewall{Supported: v.Supported, Enabled: v.Settings.Enabled}
		} else {
			slog.Warn("advisor: firewall", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkedAt": in.Now.UTC().Format(time.RFC3339),
		"findings": security.Evaluate(in)})
}
