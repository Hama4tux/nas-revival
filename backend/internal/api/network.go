package api

import (
	"context"
	"log/slog"
	"net/http"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/netinfo"
)

// NetworkInfo reads the network overview (netinfo.Reader).
type NetworkInfo interface {
	Read(ctx context.Context) (netinfo.Info, error)
}

// NetworkAdmin is implemented by the helper client (net.*).
type NetworkAdmin interface {
	HostnameSet(ctx context.Context, name string) (helper.HostnameState, error)
	NetDiag(ctx context.Context, test, target string) (helper.DiagResult, error)
}

const hostnameBodyLimit = 1 << 10

func (s *Server) registerNetwork(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/network", s.requireSession(s.networkInfo))
	api.HandleFunc("PUT /api/v1/network/hostname", s.requireSession(s.hostnameSet))
	// POST: runs commands on the NAS, so administrators only (requireSession).
	api.HandleFunc("POST /api/v1/network/diagnostics", s.requireSession(s.netDiag))
}

func (s *Server) netDiag(w http.ResponseWriter, r *http.Request) {
	if s.NetAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro diagnostiku sítě není dostupná.")
		return
	}
	var body struct {
		Test   string `json:"test"`
		Target string `json:"target"`
	}
	if !decodeBody(w, r, hostnameBodyLimit, &body) {
		return
	}
	target, err := helper.ValidateDiag(body.Test, body.Target)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	res, err := s.NetAdmin.NetDiag(r.Context(), body.Test, target)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) networkInfo(w http.ResponseWriter, r *http.Request) {
	if s.NetInfo == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Přehled sítě není dostupný.")
		return
	}
	info, err := s.NetInfo.Read(r.Context())
	if err != nil {
		slog.Error("network info failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Přehled sítě se nepodařilo načíst.")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) hostnameSet(w http.ResponseWriter, r *http.Request) {
	if s.NetAdmin == nil || s.NetInfo == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro změnu nastavení není dostupná.")
		return
	}
	var body struct {
		Hostname string `json:"hostname"`
	}
	if !decodeBody(w, r, hostnameBodyLimit, &body) {
		return
	}
	name, err := helper.ValidateHostname(body.Hostname)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	if _, err := s.NetAdmin.HostnameSet(r.Context(), name); err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "hostname", name, "Změněn název NAS na "+name)
	s.networkInfo(w, r)
}
