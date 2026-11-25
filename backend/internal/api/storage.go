package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/storage"
)

// StorageSource reports disks, partitions and mounts without waking disks.
type StorageSource interface {
	Report() (storage.Report, error)
}

// SmartDetailSource reads per-disk SMART detail (implemented by *smart.DetailService).
type SmartDetailSource interface {
	Detail(ctx context.Context, device string) smart.Detail
	Invalidate(device string)
}

// SmartTester starts self-tests through the helper (op smart.test).
type SmartTester interface {
	SmartTest(ctx context.Context, diskID, typ string) (helper.SmartTestResult, error)
}

func (s *Server) registerStorage(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/storage", s.requireSession(s.storage))
	api.HandleFunc("GET /api/v1/storage/disks/{id}/smart", s.requireSession(s.diskSmart))
	api.HandleFunc("POST /api/v1/storage/disks/{id}/smart/test", s.requireSession(s.diskSmartTest))
	s.registerDiskPower(api)
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

// smartDisk resolves {id} to a SMART-capable disk or writes the error.
func (s *Server) smartDisk(w http.ResponseWriter, r *http.Request) (storage.Disk, bool) {
	return s.internalDisk(w, r, "smart_unsupported", "Disk nepodporuje SMART.")
}

// internalDisk resolves {id} to an internal SATA disk (the ones smartctl and
// hdparm address directly) or writes the error.
func (s *Server) internalDisk(w http.ResponseWriter, r *http.Request, code, msg string) (storage.Disk, bool) {
	if s.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Informace o úložišti nejsou dostupné.")
		return storage.Disk{}, false
	}
	rep, err := s.Storage.Report()
	if err != nil {
		slog.Error("storage report failed", "err", err)
		writeError(w, http.StatusInternalServerError, "storage_unavailable", "Informace o úložišti nelze načíst.")
		return storage.Disk{}, false
	}
	d, ok := rep.FindByID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "disk_not_found", "Disk nebyl nalezen.")
		return storage.Disk{}, false
	}
	if !d.SMART {
		writeError(w, http.StatusNotFound, code, msg)
		return storage.Disk{}, false
	}
	return d, true
}

func (s *Server) diskSmart(w http.ResponseWriter, r *http.Request) {
	if s.SmartDetail == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SMART není dostupný.")
		return
	}
	d, ok := s.smartDisk(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.SmartDetail.Detail(r.Context(), d.Name))
}

func (s *Server) diskSmartTest(w http.ResponseWriter, r *http.Request) {
	if s.SmartTests == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	var body struct {
		Type string `json:"type"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.Type != "short" && body.Type != "long") {
		writeInvalid(w, "type", "Typ testu musí být short nebo long.")
		return
	}
	d, ok := s.smartDisk(w, r)
	if !ok {
		return
	}
	res, err := s.SmartTests.SmartTest(r.Context(), d.ID, body.Type)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	if s.SmartDetail != nil {
		s.SmartDetail.Invalidate(d.Name)
	}
	writeJSON(w, http.StatusAccepted, res)
}
