package api

import (
	"context"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/shares"
)

// ShareAdmin is implemented by the helper client (ops share.*).
type ShareAdmin interface {
	ShareList(ctx context.Context) (helper.ShareList, error)
	ShareCreate(ctx context.Context, s shares.Share) (helper.ShareList, error)
	ShareUpdate(ctx context.Context, s shares.Share) (helper.ShareList, error)
	ShareDelete(ctx context.Context, d helper.ShareDelete) (helper.ShareDeleteResult, error)
	ShareRecycleCleanup(ctx context.Context) (shares.RecycleResult, error)
}

const sharesBodyLimit = 8 << 10

func (s *Server) registerShares(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/shares", s.requireSession(s.sharesList))
	api.HandleFunc("POST /api/v1/shares", s.requireSession(s.shareCreate))
	api.HandleFunc("POST /api/v1/shares/recycle-cleanup", s.requireSession(s.sharesRecycle))
	api.HandleFunc("PUT /api/v1/shares/{name}", s.requireSession(s.shareUpdate))
	api.HandleFunc("DELETE /api/v1/shares/{name}", s.requireSession(s.shareDelete))
}

func (s *Server) sharesReady(w http.ResponseWriter) bool {
	if s.Shares == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu sdílených složek není dostupná.")
		return false
	}
	return true
}

func sharePathName(w http.ResponseWriter, r *http.Request) (string, bool) {
	n := r.PathValue("name")
	if !shares.ValidName(n) {
		writeError(w, http.StatusNotFound, helper.CodeNotFound, "Sdílená složka neexistuje.")
		return "", false
	}
	return n, true
}

func (s *Server) sharesList(w http.ResponseWriter, r *http.Request) {
	if !s.sharesReady(w) {
		return
	}
	l, err := s.Shares.ShareList(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) shareCreate(w http.ResponseWriter, r *http.Request) {
	if !s.sharesReady(w) {
		return
	}
	var body shares.Share
	if !decodeBody(w, r, sharesBodyLimit, &body) {
		return
	}
	body, err := helper.ValidateShare(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	l, err := s.Shares.ShareCreate(r.Context(), body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("share created", "share", body.Name, "by", sessionUser(r))
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) shareUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.sharesReady(w) {
		return
	}
	name, ok := sharePathName(w, r)
	if !ok {
		return
	}
	var body shares.Share
	if !decodeBody(w, r, sharesBodyLimit, &body) {
		return
	}
	body.Name = name // the path wins
	body, err := helper.ValidateShare(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	l, err := s.Shares.ShareUpdate(r.Context(), body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("share updated", "share", name, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) shareDelete(w http.ResponseWriter, r *http.Request) {
	if !s.sharesReady(w) {
		return
	}
	name, ok := sharePathName(w, r)
	if !ok {
		return
	}
	var body struct {
		DeleteData  bool   `json:"deleteData"`
		ConfirmName string `json:"confirmName"`
	}
	if r.ContentLength != 0 && !decodeBody(w, r, sharesBodyLimit, &body) {
		return
	}
	if body.DeleteData && body.ConfirmName != name {
		writeInvalid(w, "confirmName", "Pro smazání dat napište přesný název složky.")
		return
	}
	res, err := s.Shares.ShareDelete(r.Context(), helper.ShareDelete{Name: name, DeleteData: body.DeleteData, ConfirmName: body.ConfirmName})
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("share deleted", "share", name, "deleteData", body.DeleteData, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) sharesRecycle(w http.ResponseWriter, r *http.Request) {
	if !s.sharesReady(w) {
		return
	}
	res, err := s.Shares.ShareRecycleCleanup(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("share recycle bins emptied", "files", res.RemovedFiles, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, res)
}
