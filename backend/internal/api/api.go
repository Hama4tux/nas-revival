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

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/system"
)

const (
	cookieName = "nr_session"
	csrfHeader = "X-Requested-With"
	csrfValue  = "nas-revival"
)

type OverviewSource interface {
	Collect(ctx context.Context) (system.Overview, error)
}

type SmartSource interface {
	Report(ctx context.Context) smart.Report
}

type Server struct {
	Verifier *auth.Verifier
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Overview OverviewSource
	Smart    SmartSource
	// UI serves the static frontend for every non-API path.
	UI http.Handler
}

func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/session", s.login)
	api.HandleFunc("GET /api/v1/session", s.requireSession(s.sessionInfo))
	api.HandleFunc("DELETE /api/v1/session", s.requireSession(s.logout))
	api.HandleFunc("GET /api/v1/overview", s.requireSession(s.overview))
	api.HandleFunc("GET /api/v1/smart", s.requireSession(s.smart))
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
	expires  time.Time
	token    string
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
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
		ctx := context.WithValue(r.Context(), ctxKey{}, sessionCtx{username: user, expires: exp, token: c.Value})
		next(w, r.WithContext(ctx))
	}
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
	slog.Info("login", "user", body.Username, "client", ip)
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
	writeJSON(w, http.StatusOK, map[string]any{"username": sc.username, "expiresAt": sc.expires.UTC().Format(time.RFC3339)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	s.Sessions.Delete(sc.token)
	clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	o, err := s.Overview.Collect(r.Context())
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
