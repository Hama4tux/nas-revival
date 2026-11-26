package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
)

// FileStation is the helper side of /files (W4.1, ADR 0009). Every call
// carries the session user in ctx; the helper checks the share access.
type FileStation interface {
	FileShares(ctx context.Context) (helper.FileShareList, error)
	FileList(ctx context.Context, share, path string, offset int) (helper.FileList, error)
	FileOpen(ctx context.Context, share, path string) (*os.File, helper.BackupFileMeta, error)
	FileMkdir(ctx context.Context, share, dir, name string) error
	FileRename(ctx context.Context, share, path, name string) error
	FileMove(ctx context.Context, share string, paths []string, dest string) (helper.FileBatch, error)
	FileDelete(ctx context.Context, share string, paths []string) (helper.FileBatch, error)
	FileUploadOpen(ctx context.Context, share, dir, name string, size int64, overwrite bool) (*os.File, string, error)
	FileUploadCommit(ctx context.Context, share, dir, token, name string, overwrite bool) error
	FileUploadAbort(ctx context.Context, share, dir, token string) error
}

const (
	// fileBodyLimit stays below the helper's request limit (16 KiB).
	fileBodyLimit = 12 << 10
	// MaxUploads is how many uploads run at once (FS-04: bounded).
	MaxUploads = 2
	// uploadIdle is how long an upload may stall before it is dropped.
	uploadIdle = 60 * time.Second
)

func (s *Server) registerFiles(api *http.ServeMux) {
	user := func(h http.HandlerFunc) http.HandlerFunc { return s.requireRole(accounts.RoleUser, h) }
	api.HandleFunc("GET /api/v1/files", user(s.fileShares))
	api.HandleFunc("GET /api/v1/files/{share}/list", user(s.fileList))
	api.HandleFunc("GET /api/v1/files/{share}/download", user(s.fileDownload))
	api.HandleFunc("POST /api/v1/files/{share}/mkdir", user(s.fileMkdir))
	api.HandleFunc("POST /api/v1/files/{share}/rename", user(s.fileRename))
	api.HandleFunc("POST /api/v1/files/{share}/move", user(s.fileMove))
	api.HandleFunc("POST /api/v1/files/{share}/delete", user(s.fileDelete))
	api.HandleFunc("PUT /api/v1/files/{share}/upload", user(s.fileUpload))
}

func (s *Server) filesReady(w http.ResponseWriter) bool {
	if s.Files == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro soubory není dostupná.")
		return false
	}
	return true
}

func (s *Server) fileShares(w http.ResponseWriter, r *http.Request) {
	if !s.filesReady(w) {
		return
	}
	res, err := s.Files.FileShares(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) fileList(w http.ResponseWriter, r *http.Request) {
	if !s.filesReady(w) {
		return
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > helper.MaxBrowseRead {
			writeInvalid(w, "offset", "Neplatný posun.")
			return
		}
		offset = n
	}
	res, err := s.Files.FileList(r.Context(), r.PathValue("share"), r.URL.Query().Get("path"), offset)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// fileDownload streams a file through the descriptor the helper opened.
func (s *Server) fileDownload(w http.ResponseWriter, r *http.Request) {
	if !s.filesReady(w) {
		return
	}
	f, meta, err := s.Files.FileOpen(r.Context(), r.PathValue("share"), r.URL.Query().Get("path"))
	if err != nil {
		writeHelperError(w, err)
		return
	}
	defer f.Close()
	http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Disposition", attachment(meta.Name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", meta.Modified, f)
}

type fileNameBody struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type fileItemsBody struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
}

func (s *Server) fileMkdir(w http.ResponseWriter, r *http.Request) {
	var b fileNameBody
	if !s.filesReady(w) || !decodeBody(w, r, fileBodyLimit, &b) {
		return
	}
	if err := s.Files.FileMkdir(r.Context(), r.PathValue("share"), b.Path, b.Name); err != nil {
		writeHelperError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) fileRename(w http.ResponseWriter, r *http.Request) {
	var b fileNameBody
	if !s.filesReady(w) || !decodeBody(w, r, fileBodyLimit, &b) {
		return
	}
	if err := s.Files.FileRename(r.Context(), r.PathValue("share"), b.Path, b.Name); err != nil {
		writeHelperError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) fileMove(w http.ResponseWriter, r *http.Request) {
	var b fileItemsBody
	if !s.filesReady(w) || !decodeBody(w, r, fileBodyLimit, &b) {
		return
	}
	res, err := s.Files.FileMove(r.Context(), r.PathValue("share"), b.Paths, b.Dest)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) fileDelete(w http.ResponseWriter, r *http.Request) {
	var b fileItemsBody
	if !s.filesReady(w) || !decodeBody(w, r, fileBodyLimit, &b) {
		return
	}
	if b.Dest != "" {
		writeInvalid(w, "dest", "Neplatné tělo požadavku.")
		return
	}
	// A big permanent delete outlasts the server's write timeout.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(12 * time.Minute))
	res, err := s.Files.FileDelete(r.Context(), r.PathValue("share"), b.Paths)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// idleReader extends the read deadline on every read, so a long upload may
// take hours but a stalled one is dropped after uploadIdle.
type idleReader struct {
	r  io.Reader
	rc *http.ResponseController
}

func (ir idleReader) Read(p []byte) (int, error) {
	ir.rc.SetReadDeadline(time.Now().Add(uploadIdle))
	return ir.r.Read(p)
}

// fileUpload streams the request body into a temporary file the helper
// created, then lets the helper give it its name (FS-03). Memory stays flat:
// io.CopyN uses one 32 KiB buffer.
func (s *Server) fileUpload(w http.ResponseWriter, r *http.Request) {
	if !s.filesReady(w) {
		return
	}
	q := r.URL.Query()
	share, dir, name, overwrite := r.PathValue("share"), q.Get("path"), q.Get("name"), q.Get("overwrite") == "1"
	size := r.ContentLength
	if size < 0 {
		writeError(w, http.StatusLengthRequired, "length_required", "Chybí velikost souboru (Content-Length).")
		return
	}
	if !helper.ValidFileName(name) {
		writeInvalid(w, "name", `Neplatný název (bez znaků / \ : * ? " < > |, bez mezery na začátku či konci a tečky na konci).`)
		return
	}
	if s.uploads.Add(1) > MaxUploads {
		s.uploads.Add(-1)
		writeError(w, http.StatusTooManyRequests, "busy", "Současně lze nahrávat nejvýš "+strconv.Itoa(MaxUploads)+" soubory; zkuste to za chvíli.")
		return
	}
	defer s.uploads.Add(-1)
	ctx := r.Context()
	f, token, err := s.Files.FileUploadOpen(ctx, share, dir, name, size, overwrite)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	n, err := io.CopyN(f, idleReader{r.Body, rc}, size)
	if err == nil {
		// The data is on the disk before it gets its name.
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	// The request context may already be gone; cleaning up must not be.
	bg := context.WithoutCancel(ctx)
	if err != nil {
		s.Files.FileUploadAbort(bg, share, dir, token)
		slog.Warn("upload failed", "share", share, "bytes", n, "size", size, "err", err, "by", sessionUser(r))
		writeError(w, http.StatusBadRequest, "upload_failed", "Nahrávání se přerušilo; soubor nebyl uložen.")
		return
	}
	if err := s.Files.FileUploadCommit(bg, share, dir, token, name, overwrite); err != nil {
		writeHelperError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
