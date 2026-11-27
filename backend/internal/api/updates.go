package api

import (
	"context"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
)

// UpdatesAdmin is implemented by the helper client (updates.*).
type UpdatesAdmin interface {
	UpdatesGet(ctx context.Context) (helper.UpdatesStatus, error)
	UpdatesCheck(ctx context.Context) (helper.UpdatesStatus, error)
	UpdatesUpgrade(ctx context.Context, listedAt string) (helper.UpdatesStatus, error)
}

// Update Manager (W5.4, ADR 0016): Debian updates; the versions of NAS
// Revival and the kernel are shown read-only.
func (s *Server) registerUpdates(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/updates", s.requireRole(accounts.RoleViewer, s.updatesGet))
	api.HandleFunc("POST /api/v1/updates/check", s.requireRole(accounts.RoleAdmin, s.updatesCheck))
	api.HandleFunc("POST /api/v1/updates/upgrade", s.requireRole(accounts.RoleAdmin, s.updatesUpgrade))
}

type versionsView struct {
	App    string `json:"app"`
	Kernel string `json:"kernel"`
}

type updatesView struct {
	helper.UpdatesStatus
	Versions versionsView `json:"versions"`
}

func (s *Server) updatesView(st helper.UpdatesStatus) updatesView {
	v := updatesView{UpdatesStatus: st}
	if s.System != nil {
		if in, err := s.System.Info(); err == nil {
			v.Versions.App, v.Versions.Kernel = in.Version, in.Kernel
		}
	}
	return v
}

func (s *Server) updatesGet(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		unavailable(w)
		return
	}
	st, err := s.Updates.UpdatesGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.updatesView(st))
}

func (s *Server) updatesCheck(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		unavailable(w)
		return
	}
	st, err := s.Updates.UpdatesCheck(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "updates.check", "", "Spuštěna kontrola aktualizací")
	writeJSON(w, http.StatusAccepted, s.updatesView(st))
}

func (s *Server) updatesUpgrade(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		unavailable(w)
		return
	}
	var body struct {
		ListedAt string `json:"listedAt"`
	}
	if !decodeBody(w, r, 1<<10, &body) {
		return
	}
	st, err := s.Updates.UpdatesUpgrade(r.Context(), body.ListedAt)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "updates.upgrade", "", "Spuštěna instalace aktualizací ("+strconv.Itoa(len(st.Packages))+" balíčků, "+
		strconv.Itoa(st.Security())+" bezpečnostních)")
	writeJSON(w, http.StatusAccepted, s.updatesView(st))
}
