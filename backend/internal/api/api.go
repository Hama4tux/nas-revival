// Package api exposes the versioned administration HTTP API (docs/api/v1.md).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/system"
)

const (
	cookieName = "nr_session"
	csrfHeader = "X-Requested-With"
	csrfValue  = "nas-revival"
)

// OverviewSource is the shared collector (system.Snapshots).
type OverviewSource interface {
	Snapshot(ctx context.Context) (system.Overview, error)
}

type SmartSource interface {
	Report(ctx context.Context) smart.Report
}

// RoleSource resolves the current role of a logged-in user (auth.Directory).
type RoleSource interface {
	Role(username string) (accounts.Role, bool)
	// IsNASUser: the account's password is changed through the helper;
	// otherwise it is the recovery administrator (admin.json).
	IsNASUser(username string) bool
}

type Server struct {
	Verifier *auth.Verifier
	// Roles is consulted on every request; nil means single-administrator
	// mode (tests, mock), where every session is an administrator.
	Roles    RoleSource
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Overview OverviewSource
	Smart    SmartSource
	// Settings backends; a nil one answers 503.
	Network NetworkSettings
	Time    TimeSettings
	TLS     TLSSettings
	// System serves /system/info and /system/capabilities; nil answers 503.
	System SystemSource
	// Storage serves /storage; nil answers 503.
	Storage StorageSource
	// SmartDetail and SmartTests serve /storage/disks/{id}/smart; nil answers 503.
	SmartDetail SmartDetailSource
	SmartTests  SmartTester
	// DiskPower and DiskPowerSet serve /storage/disks/{id}/power; nil answers 503.
	DiskPower DiskPowerSource
	// DiskPowerHistory serves /storage/disks/{id}/power/history; nil answers 503.
	DiskPowerHistory DiskPowerHistorySource
	DiskPowerSet     DiskPowerSetter
	// DiskActivity serves /storage/activity; nil answers 503.
	DiskActivity DiskActivityMeasurer
	// SystemPower serves /system/{reboot,shutdown}; nil answers 503.
	SystemPower SystemPowerer
	// Config serves /config/history and /config/rollback; nil answers 503.
	Config ConfigHistory
	// Users serves /users and /groups; nil answers 503.
	Users UserAdmin
	// Shares serves /shares; nil answers 503.
	Shares ShareAdmin
	// Services serves /services; nil answers 503.
	Services ServiceAdmin
	// Smb serves /services/smb; nil answers 503.
	Smb SmbAdmin
	// Nfs serves /services/nfs; nil answers 503.
	Nfs NfsAdmin
	// Ssh serves /services/ssh; nil answers 503.
	Ssh SshAdmin
	// Tasks serves /tasks; nil answers 503. TaskStatus adds timers, state and history.
	Tasks      TaskAdmin
	TaskStatus TaskStatus
	// Backup serves /backup; nil answers 503.
	Backup BackupAdmin
	// NetInfo serves /network, NetAdmin the changes under /network; nil answers 503.
	NetInfo  NetworkInfo
	NetAdmin NetworkAdmin
	// Monitor serves /monitor; nil answers 503.
	Monitor MonitorSource
	// Processes serves /monitor/processes, ProcessKill its kill; nil answers 503.
	Processes   ProcessSource
	ProcessKill ProcessKiller
	// Hardware is the W3.4 hardware page; HardwareAdmin changes it through the helper.
	Hardware      HardwareSource
	HardwareAdmin HardwareAdmin
	powerTokens   confirmTokens
	// UI serves the static frontend for every non-API path.
	UI http.Handler
}

func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/session", s.login)
	api.HandleFunc("GET /api/v1/session", s.requireRole(accounts.RoleUser, s.sessionInfo))
	api.HandleFunc("DELETE /api/v1/session", s.requireRole(accounts.RoleUser, s.logout))
	s.registerAccount(api)
	api.HandleFunc("GET /api/v1/overview", s.requireSession(s.overview))
	api.HandleFunc("GET /api/v1/smart", s.requireSession(s.smart))
	s.registerSettings(api)
	s.registerSystem(api)
	s.registerSystemPower(api)
	s.registerStorage(api)
	s.registerConfig(api)
	s.registerUsers(api)
	s.registerShares(api)
	s.registerServices(api)
	s.registerSmb(api)
	s.registerNfs(api)
	s.registerSsh(api)
	s.registerTasks(api)
	s.registerBackup(api)
	s.registerNetwork(api)
	s.registerMonitor(api)
	s.registerHardware(api)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Neznámý požadavek.")
	})

	root := http.NewServeMux()
	root.Handle("/api/", noStore(csrf(api)))
	if s.UI != nil {
		root.Handle("/", s.UI)
	}
	return securityHeaders(root)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// style-src needs 'unsafe-inline' for style attributes set by React; scripts stay 'self' only.
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csrf complements SameSite=Strict cookies: state-changing requests must carry
// a custom header (impossible cross-origin without CORS) and a matching Origin.
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get(csrfHeader) != csrfValue {
				writeError(w, http.StatusForbidden, "csrf", "Požadavek byl odmítnut.")
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "csrf", "Požadavek byl odmítnut.")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

type sessionCtx struct {
	username string
	role     accounts.Role
	expires  time.Time
	token    string
}

// requireSession is the default policy (USR-04, default deny): reading needs
// at least the read-only administrator, every change the administrator.
// Endpoints a standard user may call use requireRole(RoleUser, …) explicitly.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	read, write := s.requireRole(accounts.RoleViewer, next), s.requireRole(accounts.RoleAdmin, next)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			read(w, r)
			return
		}
		write(w, r)
	}
}

func (s *Server) roleOf(user string) (accounts.Role, bool) {
	if s.Roles == nil {
		return accounts.RoleAdmin, true
	}
	return s.Roles.Role(user)
}

func (s *Server) requireRole(min accounts.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Přihlaste se.")
			return
		}
		user, exp, ok := s.Sessions.Lookup(c.Value)
		if !ok {
			clearCookie(w)
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Přihlaste se.")
			return
		}
		// The role is looked up on every request, so a role change, disable or
		// delete (also from the console) applies at once.
		role, ok := s.roleOf(user)
		if !ok {
			s.Sessions.DeleteUser(user)
			clearCookie(w)
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Přihlaste se.")
			return
		}
		if !role.Allows(min) {
			writeError(w, http.StatusForbidden, "forbidden", forbiddenMessage(role))
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, sessionCtx{username: user, role: role, expires: exp, token: c.Value})
		ctx = helper.WithUser(ctx, user)
		next(w, r.WithContext(ctx))
	}
}

func forbiddenMessage(role accounts.Role) string {
	if role == accounts.RoleViewer {
		return "Účet jen pro čtení nemůže nic měnit."
	}
	return "K této části nemáte přístup."
}

func clientIP(r *http.Request) string {
	// No proxy is in front of the service, so X-Forwarded-For is never trusted.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if wait, blocked := s.Limiter.Blocked(ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Příliš mnoho pokusů, zkuste to později.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Username == "" || body.Password == "" || len(body.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "bad_request", "Vyplňte jméno a heslo.")
		return
	}
	if err := s.Verifier.Verify(body.Username, body.Password); err != nil {
		s.Limiter.Fail(ip)
		slog.Warn("login failed", "client", ip)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Nesprávné jméno nebo heslo.")
		return
	}
	s.Limiter.Reset(ip)
	token, err := s.Sessions.Create(body.Username)
	if err != nil {
		slog.Error("session create failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Interní chyba.")
		return
	}
	role, _ := s.roleOf(body.Username)
	slog.Info("login", "user", body.Username, "role", role, "client", ip)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(auth.AbsoluteTimeout.Seconds()),
	})
	w.WriteHeader(http.StatusNoContent)
}

func clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	writeJSON(w, http.StatusOK, map[string]any{"username": sc.username, "role": sc.role,
		"expiresAt": sc.expires.UTC().Format(time.RFC3339)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	s.Sessions.Delete(sc.token)
	clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	o, err := s.Overview.Snapshot(r.Context())
	if err != nil {
		slog.Error("overview failed", "err", err)
		writeError(w, http.StatusInternalServerError, "overview_unavailable", "Stav systému nelze načíst.")
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) smart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Smart.Report(r.Context()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		slog.Warn("write response failed", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
