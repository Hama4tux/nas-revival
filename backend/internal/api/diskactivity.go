package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"nas-revival/backend/internal/diskactivity"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/helper"
)

// DiskActivityMeasurer measures through the helper (op disk.activity).
type DiskActivityMeasurer interface {
	DiskActivity(ctx context.Context, seconds int) (diskactivity.Result, error)
}

const defaultActivitySeconds = 15

type diskActivityView struct {
	diskactivity.Result
	Disks []diskactivity.DiskSummary `json:"disks"`
}

func (s *Server) registerDiskActivity(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/storage/activity", s.requireSession(s.diskActivity))
}

// diskActivity blocks for the whole measurement; the UI shows a countdown.
func (s *Server) diskActivity(w http.ResponseWriter, r *http.Request) {
	if s.DiskActivity == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro měření aktivity disků není dostupná.")
		return
	}
	seconds := defaultActivitySeconds
	if v := r.URL.Query().Get("seconds"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < diskactivity.MinSeconds || n > diskactivity.MaxSeconds {
			writeInvalid(w, "seconds", "Délka měření musí být "+strconv.Itoa(diskactivity.MinSeconds)+
				" až "+strconv.Itoa(diskactivity.MaxSeconds)+" sekund.")
			return
		}
		seconds = n
	}
	// The server-wide write timeout is shorter than a long measurement.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(seconds)*time.Second + 30*time.Second))
	res, err := s.DiskActivity.DiskActivity(r.Context(), seconds)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, diskActivityView{Result: res, Disks: diskactivity.Explain(res, s.activityDisks())})
}

// activityDisks joins /storage with the standby timeouts from disks.json.
// Without either source the measurement is still shown, only with fewer hints.
func (s *Server) activityDisks() []diskactivity.DiskInfo {
	if s.Storage == nil {
		return nil
	}
	rep, err := s.Storage.Report()
	if err != nil {
		slog.Error("storage report failed", "err", err)
		return nil
	}
	var cfg diskpower.Config
	cfgOK := false
	if s.DiskPower != nil {
		if c, err := s.DiskPower.Config(); err == nil {
			cfg, cfgOK = c, true
		}
	}
	out := make([]diskactivity.DiskInfo, 0, len(rep.Disks))
	for _, d := range rep.Disks {
		di := diskactivity.DiskInfo{Name: d.Name, ID: d.ID, System: d.System, SharedWithData: d.SharedWithData}
		if cfgOK && d.SMART {
			set, ok := cfg.Disks[d.ID]
			if !ok {
				set = diskpower.Default(d.System)
			}
			m := set.StandbyMinutes
			di.StandbyMinutes = &m
		}
		out = append(out, di)
	}
	return out
}
