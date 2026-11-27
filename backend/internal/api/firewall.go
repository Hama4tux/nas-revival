package api

import (
	"context"
	"errors"
	"net/http"
	"net/netip"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/firewall"
	"nas-revival/backend/internal/helper"
)

// FirewallAdmin is implemented by the helper client (firewall.*).
type FirewallAdmin interface {
	FirewallGet(ctx context.Context) (helper.FirewallState, error)
	FirewallApply(ctx context.Context, s firewall.Settings, clientAddr string) (helper.FirewallState, error)
	FirewallConfirm(ctx context.Context) (helper.FirewallState, error)
}

// Firewall (W5.2, ADR 0014).
func (s *Server) registerFirewall(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/security/firewall", s.requireSession(s.firewallGet))
	api.HandleFunc("PUT /api/v1/security/firewall", s.requireRole(accounts.RoleAdmin, s.firewallPut))
	api.HandleFunc("POST /api/v1/security/firewall/confirm", s.requireRole(accounts.RoleAdmin, s.firewallConfirm))
}

type firewallView struct {
	helper.FirewallState
	ClientAddress string             `json:"clientAddress"`
	LanNetwork    *string            `json:"lanNetwork"`
	Services      []firewall.Service `json:"services"`
}

// lanNetwork suggests the IPv4 network of the interface that reaches the client
// (else the first global IPv4 network).
func (s *Server) lanNetwork(ctx context.Context, client string) *string {
	if s.NetInfo == nil {
		return nil
	}
	info, err := s.NetInfo.Read(ctx)
	if err != nil {
		return nil
	}
	c, _ := netip.ParseAddr(client)
	var first *netip.Prefix
	for _, it := range info.Interfaces {
		for _, a := range it.Addresses {
			p, err := netip.ParsePrefix(a)
			if err != nil || !p.Addr().Is4() || p.Addr().IsLoopback() || p.Addr().IsLinkLocalUnicast() {
				continue
			}
			p = p.Masked()
			if p.Contains(c) {
				v := p.String()
				return &v
			}
			if first == nil {
				first = &p
			}
		}
	}
	if first == nil {
		return nil
	}
	v := first.String()
	return &v
}

func (s *Server) firewallView(ctx context.Context, r *http.Request, st helper.FirewallState) firewallView {
	client := clientIP(r)
	if st.Listening == nil {
		st.Listening = []firewall.Listen{}
	}
	return firewallView{FirewallState: st, ClientAddress: client, LanNetwork: s.lanNetwork(ctx, client),
		Services: firewall.Catalogue}
}

func (s *Server) firewallGet(w http.ResponseWriter, r *http.Request) {
	if s.Firewall == nil {
		unavailable(w)
		return
	}
	st, err := s.Firewall.FirewallGet(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.firewallView(r.Context(), r, st))
}

func (s *Server) firewallPut(w http.ResponseWriter, r *http.Request) {
	if s.Firewall == nil {
		unavailable(w)
		return
	}
	var body firewall.Settings
	if !decodeBody(w, r, settingsBodyLimit, &body) {
		return
	}
	client := clientIP(r)
	set, err := firewall.Validate(body, client)
	if err != nil {
		var fe *firewall.FieldError
		if errors.As(err, &fe) {
			writeInvalid(w, fe.Field, fe.Msg)
		} else {
			writeHelperError(w, err)
		}
		return
	}
	st, err := s.Firewall.FirewallApply(r.Context(), set, client)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "firewall.apply", "", "Použito nastavení firewallu ("+onOff(set.Enabled)+"), čeká na potvrzení")
	writeJSON(w, http.StatusOK, s.firewallView(r.Context(), r, st))
}

func (s *Server) firewallConfirm(w http.ResponseWriter, r *http.Request) {
	if s.Firewall == nil {
		unavailable(w)
		return
	}
	st, err := s.Firewall.FirewallConfirm(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	auditOK(r, "firewall.confirm", "", "Potvrzeno nastavení firewallu ("+onOff(st.Settings.Enabled)+")")
	writeJSON(w, http.StatusOK, s.firewallView(r.Context(), r, st))
}
