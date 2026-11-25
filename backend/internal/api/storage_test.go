package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/storage"
)

type fakeStorage struct{ err error }

func (f fakeStorage) Report() (storage.Report, error) {
	if f.err != nil {
		return storage.Report{}, f.err
	}
	return storage.Report{Disks: []storage.Disk{{ID: "wwn-0x1", IDStable: true, Name: "sda", System: true,
		Roles: []string{storage.RoleSystem}, Partitions: []storage.Partition{}}}}, nil
}

func newStorageServer(t *testing.T, src StorageSource) http.Handler {
	t.Helper()
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}}
	if src != nil {
		s.Storage = src
	}
	return s.Handler()
}

func TestStorageNeedsSession(t *testing.T) {
	h := newStorageServer(t, fakeStorage{})
	if rec := do(h, "GET", "/api/v1/storage", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d", rec.Code)
	}
}

func TestStorageReport(t *testing.T) {
	h := newStorageServer(t, fakeStorage{})
	c := sessionCookie(t, login(t, h, "secret"))
	rec := do(h, "GET", "/api/v1/storage", "", nil, c)
	var rep storage.Report
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &rep) != nil || len(rep.Disks) != 1 || rep.Disks[0].ID != "wwn-0x1" {
		t.Fatalf("storage: %d %s", rec.Code, rec.Body)
	}
}

func TestStorageErrors(t *testing.T) {
	h := newStorageServer(t, nil)
	c := sessionCookie(t, login(t, h, "secret"))
	if rec := do(h, "GET", "/api/v1/storage", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil source = %d", rec.Code)
	}
	h = newStorageServer(t, fakeStorage{err: errors.New("boom")})
	c = sessionCookie(t, login(t, h, "secret"))
	rec := do(h, "GET", "/api/v1/storage", "", nil, c)
	if rec.Code != http.StatusInternalServerError || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("failing source = %d %s", rec.Code, rec.Body)
	}
}
