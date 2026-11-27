package api

import (
	"net/http"
	"strconv"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/audit"
	"nas-revival/backend/internal/helper"
)

// registerAccount: what every logged-in user may do with their own account.
func (s *Server) registerAccount(api *http.ServeMux) {
	api.HandleFunc("PUT /api/v1/session/password", s.requireRole(accounts.RoleUser, s.ownPassword))
}

// ownPassword changes the caller's password after checking the current one
// (counted by the login limiter, so it cannot be used to guess passwords).
// All sessions of the user end, including this one.
func (s *Server) ownPassword(w http.ResponseWriter, r *http.Request) {
	user := sessionUser(r)
	ip := clientIP(r)
	if wait, blocked := s.Limiter.Blocked(ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Příliš mnoho pokusů, zkuste to později.")
		return
	}
	var body struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	if s.Roles == nil || !s.Roles.IsNASUser(user) {
		writeError(w, http.StatusConflict, "recovery_admin",
			"Heslo záchranného administrátora se mění přes SSH příkazem nas-revival set-password.")
		return
	}
	if !s.usersReady(w) {
		return
	}
	if err := helper.ValidatePassword(body.Password); err != nil {
		writeHelperError(w, err)
		return
	}
	if len(body.Current) > 1024 || s.Verifier.Verify(user, body.Current) != nil {
		s.Limiter.Fail(ip)
		audit.Log(audit.Entry{Action: "password.own.fail", User: user, Client: ip, Result: audit.Fail, Priority: 4,
			Message: "Změna vlastního hesla: špatné současné heslo (" + user + ")"})
		writeInvalid(w, "current", "Současné heslo nesouhlasí.")
		return
	}
	s.Limiter.Reset(ip)
	res, err := s.Users.UserSetPassword(r.Context(), user, body.Password)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	s.Sessions.DeleteUser(user)
	clearCookie(w)
	audit.Log(audit.Entry{Action: "password.own", User: user, Client: ip, Result: audit.OK,
		Message: "Změněno vlastní heslo uživatele " + user})
	writeJSON(w, http.StatusOK, res)
}
