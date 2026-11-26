package api

import (
	"context"
	"net/http"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
)

// UsbAdmin is the helper side of /storage/usb (W4.2, ADR 0010).
type UsbAdmin interface {
	UsbList(ctx context.Context) (helper.UsbList, error)
	UsbMount(ctx context.Context, diskID, part string, readOnly bool) (helper.UsbList, error)
	UsbUnmount(ctx context.Context, diskID, part string) (helper.UsbList, error)
	UsbEject(ctx context.Context, diskID string) (helper.UsbList, error)
}

func (s *Server) registerUsb(api *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.HandlerFunc { return s.requireRole(accounts.RoleAdmin, h) }
	api.HandleFunc("GET /api/v1/storage/usb", s.requireSession(s.usbList))
	api.HandleFunc("POST /api/v1/storage/usb/mount", admin(s.usbMount))
	api.HandleFunc("POST /api/v1/storage/usb/unmount", admin(s.usbUnmount))
	api.HandleFunc("POST /api/v1/storage/usb/eject", admin(s.usbEject))
}

func (s *Server) usbReady(w http.ResponseWriter) bool {
	if s.Usb == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro USB disky není dostupná.")
		return false
	}
	return true
}

type usbBody struct {
	DiskID    string `json:"diskId"`
	Partition string `json:"partition"`
	ReadOnly  bool   `json:"readOnly"`
}

func (s *Server) usbReply(w http.ResponseWriter, l helper.UsbList, err error) {
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) usbList(w http.ResponseWriter, r *http.Request) {
	if !s.usbReady(w) {
		return
	}
	l, err := s.Usb.UsbList(r.Context())
	s.usbReply(w, l, err)
}

func (s *Server) usbBody(w http.ResponseWriter, r *http.Request, needPart bool) (usbBody, bool) {
	var b usbBody
	if !s.usbReady(w) || !decodeBody(w, r, 1024, &b) {
		return b, false
	}
	if b.DiskID == "" || len(b.DiskID) > 200 {
		writeInvalid(w, "diskId", "Neplatný disk.")
		return b, false
	}
	if needPart && (b.Partition == "" || len(b.Partition) > 16) {
		writeInvalid(w, "partition", "Neplatný oddíl.")
		return b, false
	}
	return b, true
}

func (s *Server) usbMount(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.usbBody(w, r, true); ok {
		l, err := s.Usb.UsbMount(r.Context(), b.DiskID, b.Partition, b.ReadOnly)
		s.usbReply(w, l, err)
	}
}

func (s *Server) usbUnmount(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.usbBody(w, r, true); ok {
		l, err := s.Usb.UsbUnmount(r.Context(), b.DiskID, b.Partition)
		s.usbReply(w, l, err)
	}
}

func (s *Server) usbEject(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.usbBody(w, r, false); ok {
		l, err := s.Usb.UsbEject(r.Context(), b.DiskID)
		s.usbReply(w, l, err)
	}
}
