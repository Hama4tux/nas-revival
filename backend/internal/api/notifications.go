package api

import (
	"net/http"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/notify"
)

// NotificationCenter serves /notifications (NOTIF-01); nil answers 503.
type NotificationCenter interface {
	List(user string) notify.List
	MarkRead(user string, ids []string)
}

// maxReadIDs bounds one mark-read request; the centre holds at most 200.
const maxReadIDs = notify.MaxEntries

func (s *Server) registerNotifications(api *http.ServeMux) {
	// Read state is per user, so the read-only administrator may set their own;
	// standard users do not see health at all (like the overview).
	api.HandleFunc("GET /api/v1/notifications", s.requireRole(accounts.RoleViewer, s.notifications))
	api.HandleFunc("POST /api/v1/notifications/read", s.requireRole(accounts.RoleViewer, s.notificationsRead))
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	if s.Notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Upozornění nejsou k dispozici.")
		return
	}
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	writeJSON(w, http.StatusOK, s.Notifications.List(sc.username))
}

func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request) {
	if s.Notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Upozornění nejsou k dispozici.")
		return
	}
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if !decodeBody(w, r, 16<<10, &body) {
		return
	}
	if body.All == (len(body.IDs) > 0) || len(body.IDs) > maxReadIDs {
		writeInvalid(w, "ids", "Zadejte buď seznam upozornění, nebo všechna.")
		return
	}
	for _, id := range body.IDs {
		if len(id) == 0 || len(id) > 32 {
			writeInvalid(w, "ids", "Neplatné upozornění.")
			return
		}
	}
	ids := body.IDs
	if body.All {
		ids = nil
	}
	sc := r.Context().Value(ctxKey{}).(sessionCtx)
	s.Notifications.MarkRead(sc.username, ids)
	w.WriteHeader(http.StatusNoContent)
}
