package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"nas-revival/backend/internal/helper"
)

// confirmTTL: a preview's token is valid this long and only once (API_DESIGN).
const confirmTTL = 60 * time.Second

// SystemPowerer restarts and shuts down the NAS through the helper
// (ops power.preflight, power.reboot, power.shutdown).
type SystemPowerer interface {
	PowerPreflight(ctx context.Context) (helper.PowerPreflight, error)
	PowerAction(ctx context.Context, action string, force bool, user string) (helper.PowerActionResult, error)
}

// Effects the UI explains before a restart or shutdown (codes, texts in cs.ts).
var powerEffects = map[string][]string{
	"reboot":   {"web_offline", "connections_drop", "sessions_end", "back_in_minutes"},
	"shutdown": {"web_offline", "connections_drop", "sessions_end", "power_button"},
}

type powerPreview struct {
	Action       string                `json:"action"`
	Hostname     string                `json:"hostname"`
	ConfirmToken string                `json:"confirmToken"`
	ExpiresAt    time.Time             `json:"expiresAt"`
	Effects      []string              `json:"effects"`
	Blockers     []helper.PowerBlocker `json:"blockers"`
}

type confirmEntry struct {
	user, op string
	expires  time.Time
}

// confirmTokens binds a preview to the following call: single use, per user
// and operation, short-lived. The zero value is ready to use.
type confirmTokens struct {
	mu  sync.Mutex
	m   map[string]confirmEntry
	now func() time.Time
}

func (c *confirmTokens) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *confirmTokens) issue(user, op string) (string, time.Time, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	tok := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.m == nil {
		c.m = map[string]confirmEntry{}
	}
	for k, e := range c.m {
		if !now.Before(e.expires) {
			delete(c.m, k)
		}
	}
	exp := now.Add(confirmTTL)
	c.m[tok] = confirmEntry{user: user, op: op, expires: exp}
	return tok, exp, nil
}

// consume removes the token and reports whether it was valid for user and op.
func (c *confirmTokens) consume(tok, user, op string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[tok]
	if !ok {
		return false
	}
	delete(c.m, tok)
	return e.user == user && e.op == op && c.clock().Before(e.expires)
}

func (s *Server) registerSystemPower(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/system/{action}/preview", s.requireSession(s.powerPreview))
	api.HandleFunc("POST /api/v1/system/{action}", s.requireSession(s.powerAction))
}

// powerActionOf returns the action from the path; other names are 404 so
// /system/info and friends keep their own routes.
func powerActionOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := r.PathValue("action")
	if _, ok := powerEffects[a]; !ok {
		writeError(w, http.StatusNotFound, "not_found", "Neznámý požadavek.")
		return "", false
	}
	return a, true
}

func (s *Server) powerPreview(w http.ResponseWriter, r *http.Request) {
	action, ok := powerActionOf(w, r)
	if !ok {
		return
	}
	if s.SystemPower == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	pf, err := s.SystemPower.PowerPreflight(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	tok, exp, err := s.powerTokens.issue(sc.username, "power."+action)
	if err != nil {
		slog.Error("confirm token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Interní chyba.")
		return
	}
	host, _ := os.Hostname()
	if pf.Blockers == nil {
		pf.Blockers = []helper.PowerBlocker{}
	}
	writeJSON(w, http.StatusOK, powerPreview{Action: action, Hostname: host, ConfirmToken: tok, ExpiresAt: exp.UTC(),
		Effects: powerEffects[action], Blockers: pf.Blockers})
}

func (s *Server) powerAction(w http.ResponseWriter, r *http.Request) {
	action, ok := powerActionOf(w, r)
	if !ok {
		return
	}
	if s.SystemPower == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	var body struct {
		ConfirmToken string `json:"confirmToken"`
		Force        bool   `json:"force"`
	}
	if !decodeBody(w, r, 1024, &body) {
		return
	}
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	if !s.powerTokens.consume(body.ConfirmToken, sc.username, "power."+action) {
		writeError(w, http.StatusConflict, "confirm_required", "Potvrzení vypršelo. Otevřete dialog znovu.")
		return
	}
	res, err := s.SystemPower.PowerAction(r.Context(), action, body.Force, sc.username)
	if err != nil {
		if he, ok := helper.AsError(err); ok && (he.Code == helper.CodeBlocked || he.Code == helper.CodePowerPending) {
			writeError(w, http.StatusConflict, he.Code, he.Message)
			return
		}
		writeHelperError(w, err)
		return
	}
	auditOK(r, "power."+action, "", "Požadováno: "+action+forcedNote(res.Forced))
	writeJSON(w, http.StatusAccepted, res)
}
