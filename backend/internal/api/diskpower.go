package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/storage"
)

// DiskPowerSource reads the drive's power state and the stored settings.
type DiskPowerSource interface {
	Read(ctx context.Context, device string) diskpower.Live
	Config() (diskpower.Config, error)
}

// DiskPowerSetter changes settings through the helper (ops disk.setPower, disk.standby).
type DiskPowerSetter interface {
	DiskSetPower(ctx context.Context, diskID string, s diskpower.Settings) (helper.DiskPowerResult, error)
	DiskStandby(ctx context.Context, diskID string) (helper.DiskStandbyResult, error)
}

// DiskPowerHistorySource is the standby history tracker (PWR-04).
type DiskPowerHistorySource interface {
	View(diskID string, at time.Time) diskpower.HistoryView
}

type diskPowerView struct {
	DiskID     string `json:"diskId"`
	SystemDisk bool   `json:"systemDisk"`
	diskpower.Live
	Settings diskpower.Settings `json:"settings"`
	// Stored is false while the disk runs on the default profile.
	Stored         bool                 `json:"stored"`
	Profiles       []diskpower.Settings `json:"profiles"` // fixed profiles as they apply to this disk
	StandbyChoices []int                `json:"standbyChoices"`
}

func (s *Server) registerDiskPower(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/storage/disks/{id}/power", s.requireSession(s.diskPower))
	api.HandleFunc("PUT /api/v1/storage/disks/{id}/power", s.requireSession(s.diskPowerPut))
	api.HandleFunc("POST /api/v1/storage/disks/{id}/standby", s.requireSession(s.diskStandby))
	api.HandleFunc("GET /api/v1/storage/disks/{id}/power/history", s.requireSession(s.diskPowerHistory))
}

func (s *Server) diskPowerHistory(w http.ResponseWriter, r *http.Request) {
	if s.DiskPowerHistory == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Historie uspávání disků není dostupná.")
		return
	}
	if d, ok := s.powerDisk(w, r); ok {
		writeJSON(w, http.StatusOK, s.DiskPowerHistory.View(d.ID, time.Now()))
	}
}

func (s *Server) powerDisk(w http.ResponseWriter, r *http.Request) (storage.Disk, bool) {
	return s.internalDisk(w, r, "power_unsupported", "Disk nepodporuje správu napájení.")
}

func (s *Server) powerView(ctx context.Context, d storage.Disk) (diskPowerView, error) {
	cfg, err := s.DiskPower.Config()
	if err != nil {
		return diskPowerView{}, err
	}
	v := diskPowerView{DiskID: d.ID, SystemDisk: d.System, Live: s.DiskPower.Read(ctx, d.Name),
		StandbyChoices: diskpower.StandbyChoices, Profiles: []diskpower.Settings{}}
	v.Settings, v.Stored = cfg.Disks[d.ID]
	if !v.Stored {
		v.Settings = diskpower.Default(d.System)
	}
	for _, id := range diskpower.ProfileIDs {
		if p, ok := diskpower.ForProfile(id, d.System); ok {
			v.Profiles = append(v.Profiles, p)
		}
	}
	return v, nil
}

func (s *Server) writePowerView(w http.ResponseWriter, r *http.Request, d storage.Disk) {
	v, err := s.powerView(r.Context(), d)
	if err != nil {
		slog.Error("disks.json unreadable", "err", err)
		writeError(w, http.StatusInternalServerError, "config_unreadable", "Nastavení napájení disků nelze načíst.")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) diskPower(w http.ResponseWriter, r *http.Request) {
	if s.DiskPower == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Správa napájení disků není dostupná.")
		return
	}
	if d, ok := s.powerDisk(w, r); ok {
		s.writePowerView(w, r, d)
	}
}

func (s *Server) diskPowerPut(w http.ResponseWriter, r *http.Request) {
	if s.DiskPower == nil || s.DiskPowerSet == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	var body struct {
		Profile           string `json:"profile"`
		StandbyMinutes    int    `json:"standbyMinutes"`
		APM               *int   `json:"apm"`
		ConfirmSystemDisk bool   `json:"confirmSystemDisk"`
	}
	if !decodeBody(w, r, 1024, &body) {
		return
	}
	d, ok := s.powerDisk(w, r)
	if !ok {
		return
	}
	set, err := diskpower.Resolve(body.Profile, body.StandbyMinutes, body.APM, d.System)
	if err != nil {
		var de *diskpower.Error
		if errors.As(err, &de) {
			writeInvalid(w, de.Field, de.Message)
		} else {
			writeInvalid(w, "", "Neplatné nastavení napájení.")
		}
		return
	}
	// ADR 0007: spinning down the system disk only after an explicit warning.
	if d.System && set.StandbyMinutes > 0 && !body.ConfirmSystemDisk {
		writeError(w, http.StatusConflict, "confirm_required",
			"Systémový disk systém brzy znovu probudí; časté uspávání a probouzení disk opotřebovává. Potvrďte uspávání systémového disku.")
		return
	}
	if _, err := s.DiskPowerSet.DiskSetPower(r.Context(), d.ID, set); err != nil {
		writeHelperError(w, err)
		return
	}
	s.writePowerView(w, r, d)
}

func (s *Server) diskStandby(w http.ResponseWriter, r *http.Request) {
	if s.DiskPowerSet == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	d, ok := s.powerDisk(w, r)
	if !ok {
		return
	}
	res, err := s.DiskPowerSet.DiskStandby(r.Context(), d.ID)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	if s.SmartDetail != nil {
		s.SmartDetail.Invalidate(d.Name)
	}
	writeJSON(w, http.StatusOK, res)
}
