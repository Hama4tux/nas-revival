package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tlscert"
)

const (
	settingsBodyLimit = 16 << 10
	tlsBodyLimit      = 64 << 10
)

// NetworkSettings is implemented by the helper client.
type NetworkSettings interface {
	NetworkGet(ctx context.Context) (helper.NetworkState, error)
	NetworkApply(ctx context.Context, cfg helper.NetConfig) (helper.NetworkState, error)
	NetworkConfirm(ctx context.Context) (helper.NetworkState, error)
}

// TimeSettings is implemented by the helper client.
type TimeSettings interface {
	TimeGet(ctx context.Context) (helper.TimeState, error)
	TimeApply(ctx context.Context, servers []string) (helper.TimeState, error)
}

// TLSSettings is implemented by *tlscert.Manager.
type TLSSettings interface {
	Info() tlscert.Info
	Install(certPEM, keyPEM []byte) error
	Generate() error
}

func (s *Server) registerSettings(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/settings/network", s.requireSession(s.networkGet))
	api.HandleFunc("PUT /api/v1/settings/network", s.requireSession(s.networkPut))
	api.HandleFunc("POST /api/v1/settings/network/confirm", s.requireSession(s.networkConfirm))
	api.HandleFunc("GET /api/v1/settings/time", s.requireSession(s.timeGet))
	api.HandleFunc("PUT /api/v1/settings/time", s.requireSession(s.timePut))
	api.HandleFunc("GET /api/v1/settings/tls", s.requireSession(s.tlsGet))
	api.HandleFunc("PUT /api/v1/settings/tls", s.requireSession(s.tlsPut))
	api.HandleFunc("POST /api/v1/settings/tls/generate", s.requireSession(s.tlsGenerate))
}

func writeInvalid(w http.ResponseWriter, field, msg string) {
	e := map[string]string{"code": "invalid", "message": msg}
	if field != "" {
		e["field"] = field
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": e})
}

// writeHelperError maps helper errors to HTTP statuses.
func writeHelperError(w http.ResponseWriter, err error) {
	var he *helper.Error
	if !errors.As(err, &he) {
		slog.Error("settings failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Interní chyba.")
		return
	}
	switch he.Code {
	case helper.CodeInvalid:
		writeInvalid(w, he.Field, he.Message)
	case helper.CodeChangePending, helper.CodeNothingPending, helper.CodeTestRunning, helper.CodeConfigDrift,
		helper.CodeActivityRunning:
		writeError(w, http.StatusConflict, he.Code, he.Message)
	case helper.CodeNotFound:
		writeError(w, http.StatusNotFound, he.Code, he.Message)
	case helper.CodeHelperUnavailable:
		writeError(w, http.StatusServiceUnavailable, he.Code, he.Message)
	default:
		slog.Error("helper error", "code", he.Code, "msg", he.Message)
		writeError(w, http.StatusInternalServerError, "internal", he.Message)
	}
}

// decodeBody reads one JSON object with a size limit and no unknown fields.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "Požadavek je příliš velký.")
		} else {
			// The reason (e.g. an unknown field) only goes to the log; it holds no values.
			slog.Warn("invalid request body", "method", r.Method, "path", r.URL.Path, "err", bodyErr(err))
			writeInvalid(w, "", "Neplatné tělo požadavku.")
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		writeInvalid(w, "", "Neplatné tělo požadavku.")
		return false
	}
	return true
}

// bodyErr keeps the decoder's reason without echoing request values (passwords).
func bodyErr(err error) string {
	var ute *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &ute):
		return "wrong type for field " + ute.Field
	case errors.As(err, &se):
		return "syntax error"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return err.Error()
	default:
		return "malformed"
	}
}

func unavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba není dostupná.")
}

func (s *Server) networkGet(w http.ResponseWriter, r *http.Request) {
	if s.Network == nil {
		unavailable(w)
		return
	}
	st, err := s.Network.NetworkGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) networkPut(w http.ResponseWriter, r *http.Request) {
	if s.Network == nil {
		unavailable(w)
		return
	}
	cfg := helper.NetConfig{UseDhcpDns: true}
	if !decodeBody(w, r, settingsBodyLimit, &cfg) {
		return
	}
	cfg, err := helper.ValidateNetConfig(cfg)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	st, err := s.Network.NetworkApply(r.Context(), cfg)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	slog.Info("network settings changed", "mode", cfg.Mode)
	writeJSON(w, http.StatusAccepted, st)
}

func (s *Server) networkConfirm(w http.ResponseWriter, r *http.Request) {
	if s.Network == nil {
		unavailable(w)
		return
	}
	st, err := s.Network.NetworkConfirm(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type timeResponse struct {
	helper.TimeState
	Now string `json:"now"`
}

func (s *Server) timeJSON(st helper.TimeState) timeResponse {
	return timeResponse{TimeState: st, Now: time.Now().UTC().Format(time.RFC3339)}
}

func (s *Server) timeGet(w http.ResponseWriter, r *http.Request) {
	if s.Time == nil {
		unavailable(w)
		return
	}
	st, err := s.Time.TimeGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.timeJSON(st))
}

func (s *Server) timePut(w http.ResponseWriter, r *http.Request) {
	if s.Time == nil {
		unavailable(w)
		return
	}
	var body struct {
		NTPServers []string `json:"ntpServers"`
	}
	if !decodeBody(w, r, settingsBodyLimit, &body) {
		return
	}
	servers, err := helper.ValidateNTP(body.NTPServers)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	st, err := s.Time.TimeApply(r.Context(), servers)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.timeJSON(st))
}

func (s *Server) tlsGet(w http.ResponseWriter, r *http.Request) {
	if s.TLS == nil {
		unavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, s.TLS.Info())
}

func (s *Server) writeTLSError(w http.ResponseWriter, err error) {
	var te *tlscert.Error
	if errors.As(err, &te) {
		writeInvalid(w, te.Field, te.Message)
		return
	}
	slog.Error("tls change failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Certifikát se nepodařilo uložit.")
}

func (s *Server) tlsPut(w http.ResponseWriter, r *http.Request) {
	if s.TLS == nil {
		unavailable(w)
		return
	}
	var body struct {
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"privateKey"`
	}
	if !decodeBody(w, r, tlsBodyLimit, &body) {
		return
	}
	if body.Certificate == "" {
		writeInvalid(w, "certificate", "Vložte certifikát.")
		return
	}
	if body.PrivateKey == "" {
		writeInvalid(w, "privateKey", "Vložte soukromý klíč.")
		return
	}
	if err := s.TLS.Install([]byte(body.Certificate), []byte(body.PrivateKey)); err != nil {
		s.writeTLSError(w, err)
		return
	}
	slog.Info("tls certificate installed")
	writeJSON(w, http.StatusOK, s.TLS.Info())
}

func (s *Server) tlsGenerate(w http.ResponseWriter, r *http.Request) {
	if s.TLS == nil {
		unavailable(w)
		return
	}
	if err := s.TLS.Generate(); err != nil {
		s.writeTLSError(w, err)
		return
	}
	slog.Info("tls certificate regenerated")
	writeJSON(w, http.StatusOK, s.TLS.Info())
}
