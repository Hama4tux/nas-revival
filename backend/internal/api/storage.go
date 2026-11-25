package api

import (
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/storage"
)

// StorageSource reports disks, partitions and mounts without waking disks.
type StorageSource interface {
	Report() (storage.Report, error)
}

func (s *Server) registerStorage(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/storage", s.requireSession(s.storage))
}

func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Informace o úložišti nejsou dostupné.")
		return
	}
	rep, err := s.Storage.Report()
	if err != nil {
		slog.Error("storage report failed", "err", err)
		writeError(w, http.StatusInternalServerError, "storage_unavailable", "Informace o úložišti nelze načíst.")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
