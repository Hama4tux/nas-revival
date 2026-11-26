package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tasks"
)

// BackupAdmin is implemented by the helper client (ops backup.*).
type BackupAdmin interface {
	BackupKey(ctx context.Context) (helper.BackupKey, error)
	BackupHostScan(ctx context.Context, host string, port int) (helper.HostScan, error)
	BackupHostTrust(ctx context.Context, host string, port int, typ, key string) error
	BackupTest(ctx context.Context, host string, port int, user, path string) (helper.BackupTestResult, error)
	BackupShareGet(ctx context.Context) (helper.BackupShareState, error)
	BackupShareSet(ctx context.Context, enabled bool) (helper.BackupShareState, error)
	BackupVersions(ctx context.Context, id string) (helper.BackupVersions, error)
	BackupBrowse(ctx context.Context, id, date, path string, offset int) (helper.BrowseResult, error)
	BackupOpen(ctx context.Context, id, date, path string) (*os.File, helper.BackupFileMeta, error)
}

const backupBodyLimit = 4 << 10

type hostBody struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type trustBody struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Type string `json:"type"`
	Key  string `json:"key"`
}

type testBody struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Path string `json:"path"`
}

func (s *Server) registerBackup(api *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.HandlerFunc { return s.requireRole(accounts.RoleAdmin, h) }
	api.HandleFunc("GET /api/v1/backup/key", admin(s.backupKey))
	api.HandleFunc("POST /api/v1/backup/hostkey/scan", admin(s.backupScan))
	api.HandleFunc("POST /api/v1/backup/hostkey/trust", admin(s.backupTrust))
	api.HandleFunc("POST /api/v1/backup/test", admin(s.backupTest))
	api.HandleFunc("GET /api/v1/backup/share", s.requireSession(s.backupShareGet))
	api.HandleFunc("PUT /api/v1/backup/share", s.requireSession(s.backupShareSet))
	api.HandleFunc("GET /api/v1/backup/{id}/versions", admin(s.backupVersions))
	api.HandleFunc("GET /api/v1/backup/{id}/versions/{date}/browse", admin(s.backupBrowse))
	api.HandleFunc("GET /api/v1/backup/{id}/versions/{date}/file", admin(s.backupFile))
}

func (s *Server) backupReady(w http.ResponseWriter) bool {
	if s.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro zálohy není dostupná.")
		return false
	}
	return true
}

func (s *Server) backupKey(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	k, err := s.Backup.BackupKey(helper.WithUser(r.Context(), sessionUser(r)))
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// checkTarget validates host and port like the helper (and the task list).
func checkTarget(w http.ResponseWriter, host string, port int) (int, bool) {
	if !tasks.ValidHost(host) {
		writeInvalid(w, "host", "Neplatný název nebo adresa serveru.")
		return 0, false
	}
	if port == 0 {
		port = tasks.DefaultSSHPort
	}
	if !tasks.ValidPort(port) {
		writeInvalid(w, "port", "Port musí být 1 až 65535.")
		return 0, false
	}
	return port, true
}

func (s *Server) backupScan(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var b hostBody
	if !decodeBody(w, r, backupBodyLimit, &b) {
		return
	}
	port, ok := checkTarget(w, b.Host, b.Port)
	if !ok {
		return
	}
	res, err := s.Backup.BackupHostScan(r.Context(), b.Host, port)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) backupTrust(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var b trustBody
	if !decodeBody(w, r, backupBodyLimit, &b) {
		return
	}
	port, ok := checkTarget(w, b.Host, b.Port)
	if !ok {
		return
	}
	if err := s.Backup.BackupHostTrust(helper.WithUser(r.Context(), sessionUser(r)), b.Host, port, b.Type, b.Key); err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("backup host trusted", "host", b.Host, "port", port, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) backupTest(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var b testBody
	if !decodeBody(w, r, backupBodyLimit, &b) {
		return
	}
	port, ok := checkTarget(w, b.Host, b.Port)
	if !ok {
		return
	}
	if !tasks.ValidRemoteUser(b.User) {
		writeInvalid(w, "user", "Neplatné jméno uživatele na serveru.")
		return
	}
	if !tasks.ValidRelPath(b.Path) {
		writeInvalid(w, "path", "Neplatná cesta ke složce.")
		return
	}
	res, err := s.Backup.BackupTest(r.Context(), b.Host, port, b.User, b.Path)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) backupShareGet(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	st, err := s.Backup.BackupShareGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) backupShareSet(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeBody(w, r, backupBodyLimit, &b) {
		return
	}
	if b.Enabled == nil {
		writeInvalid(w, "enabled", "Chybí hodnota enabled.")
		return
	}
	st, err := s.Backup.BackupShareSet(helper.WithUser(r.Context(), sessionUser(r)), *b.Enabled)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("backup share changed", "enabled", st.Enabled, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, st)
}

// backupJob validates the {id} of the versions routes.
func backupJob(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !tasks.ValidID(id) {
		writeInvalid(w, "id", "Neplatné ID úlohy.")
		return "", false
	}
	return id, true
}

func (s *Server) backupVersions(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	id, ok := backupJob(w, r)
	if !ok {
		return
	}
	res, err := s.Backup.BackupVersions(r.Context(), id)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) backupBrowse(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	id, ok := backupJob(w, r)
	if !ok {
		return
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1000000 {
			writeInvalid(w, "offset", "Neplatný posun.")
			return
		}
		offset = n
	}
	res, err := s.Backup.BackupBrowse(r.Context(), id, r.PathValue("date"), r.URL.Query().Get("path"), offset)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// attachment builds a Content-Disposition with an ASCII fallback and the
// UTF-8 name (RFC 6266 / 5987).
func attachment(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '/' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
}

// backupFile streams one file of a version. The helper opens it as root and
// passes the descriptor; the web process never gets read access to the tree.
func (s *Server) backupFile(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	id, ok := backupJob(w, r)
	if !ok {
		return
	}
	f, meta, err := s.Backup.BackupOpen(r.Context(), id, r.PathValue("date"), r.URL.Query().Get("path"))
	if err != nil {
		writeHelperError(w, err)
		return
	}
	defer f.Close()
	// Big dumps take longer than the server's write timeout.
	http.NewResponseController(w).SetWriteDeadline(time.Time{})
	slog.Info("backup file downloaded", "job", id, "date", r.PathValue("date"), "bytes", meta.Size, "by", sessionUser(r))
	w.Header().Set("Content-Disposition", attachment(meta.Name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", meta.Modified, f)
}
