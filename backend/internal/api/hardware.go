package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"nas-revival/backend/internal/hardware"
)

// HardwareSource reads the hardware state and the 24 h sensor history.
type HardwareSource interface {
	State(ctx context.Context) hardware.State
	History(sinceMs int64) []hardware.Point
}

// HardwareAdmin is the helper client (hardware.setThresholds, fan.setProfile, led.set).
type HardwareAdmin interface {
	HardwareThresholds(ctx context.Context, t hardware.Thresholds) (hardware.Settings, error)
	FanProfile(ctx context.Context, profile string) (hardware.Settings, error)
	LEDSettings(ctx context.Context, l hardware.LEDs) (hardware.Settings, error)
}

func (s *Server) registerHardware(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/hardware", s.requireSession(s.hardwareState))
	api.HandleFunc("GET /api/v1/hardware/history", s.requireSession(s.hardwareHistory))
	// PUT: administrators only (requireSession).
	api.HandleFunc("PUT /api/v1/hardware/thresholds", s.requireSession(s.hardwareThresholds))
	api.HandleFunc("PUT /api/v1/hardware/fan", s.requireSession(s.hardwareFan))
	api.HandleFunc("PUT /api/v1/hardware/leds", s.requireSession(s.hardwareLEDs))
}

func (s *Server) hardwareState(w http.ResponseWriter, r *http.Request) {
	if s.Hardware == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Údaje o hardwaru nejsou dostupné.")
		return
	}
	writeJSON(w, http.StatusOK, s.Hardware.State(r.Context()))
}

// hardwareHistory returns 24 h of minute points; with ?since=<Unix ms> only
// newer ones (the last point is the running minute and changes until it closes).
func (s *Server) hardwareHistory(w http.ResponseWriter, r *http.Request) {
	if s.Hardware == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Historie teplot není dostupná.")
		return
	}
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_since", "Neplatný parametr since.")
			return
		}
		since = n
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervalSeconds": 60, "points": s.Hardware.History(since)})
}

// hardwareWrite validates in the web process too, so a typo gets its field
// error without a helper round trip; the helper validates again.
func (s *Server) hardwareWrite(w http.ResponseWriter, r *http.Request, what string, apply func(ctx context.Context) (hardware.Settings, error)) {
	if s.HardwareAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "helper_unavailable", "Služba pro nastavení hardwaru není dostupná.")
		return
	}
	st, err := apply(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("hardware settings changed", "what", what, "by", sessionUser(r))
	writeJSON(w, http.StatusOK, st)
}

func writeHardwareField(w http.ResponseWriter, err error) bool {
	if fe, ok := err.(*hardware.FieldError); ok {
		writeInvalid(w, fe.Field, fe.Msg)
		return true
	}
	return false
}

func (s *Server) hardwareThresholds(w http.ResponseWriter, r *http.Request) {
	var t hardware.Thresholds
	if !decodeBody(w, r, 1<<10, &t) {
		return
	}
	v := hardware.Defaults()
	v.Thresholds = t
	if writeHardwareField(w, hardware.Validate(v)) {
		return
	}
	s.hardwareWrite(w, r, "thresholds", func(ctx context.Context) (hardware.Settings, error) {
		return s.HardwareAdmin.HardwareThresholds(ctx, t)
	})
}

func (s *Server) hardwareFan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profile string `json:"profile"`
	}
	if !decodeBody(w, r, 1<<10, &body) {
		return
	}
	if _, ok := hardware.ProfileByID(body.Profile); !ok {
		writeInvalid(w, "profile", "Neznámý profil ventilátoru.")
		return
	}
	s.hardwareWrite(w, r, "fan "+body.Profile, func(ctx context.Context) (hardware.Settings, error) {
		return s.HardwareAdmin.FanProfile(ctx, body.Profile)
	})
}

func (s *Server) hardwareLEDs(w http.ResponseWriter, r *http.Request) {
	var l hardware.LEDs
	if !decodeBody(w, r, 1<<10, &l) {
		return
	}
	v := hardware.Defaults()
	v.LEDs = l
	if writeHardwareField(w, hardware.Validate(v)) {
		return
	}
	s.hardwareWrite(w, r, "leds", func(ctx context.Context) (hardware.Settings, error) {
		return s.HardwareAdmin.LEDSettings(ctx, l)
	})
}
