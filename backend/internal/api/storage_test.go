package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/storage"
)

type fakeStorage struct{ err error }

func (f fakeStorage) Report() (storage.Report, error) {
	if f.err != nil {
		return storage.Report{}, f.err
	}
	return storage.Report{Disks: []storage.Disk{{ID: "wwn-0x1", IDStable: true, Name: "sda", System: true, SMART: true,
		Roles: []string{storage.RoleSystem}, Partitions: []storage.Partition{}},
		{ID: "usb-stick", IDStable: true, Name: "sdb", Roles: []string{}, Partitions: []storage.Partition{}}}}, nil
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
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &rep) != nil || len(rep.Disks) != 2 || rep.Disks[0].ID != "wwn-0x1" {
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

type fakeDetail struct{ invalidated []string }

func (f *fakeDetail) Detail(_ context.Context, dev string) smart.Detail {
	m := "model-of-" + dev
	return smart.Detail{Health: "passed", Model: &m, Attributes: []smart.Attribute{}, History: []smart.SelfTestEntry{}}
}
func (f *fakeDetail) Invalidate(dev string) { f.invalidated = append(f.invalidated, dev) }

type fakeTester struct {
	err   error
	calls []string
}

func (f *fakeTester) SmartTest(_ context.Context, id, typ string) (helper.SmartTestResult, error) {
	f.calls = append(f.calls, id+" "+typ)
	if f.err != nil {
		return helper.SmartTestResult{}, f.err
	}
	return helper.SmartTestResult{DiskID: id, Type: typ}, nil
}

func newSmartServer(t *testing.T, det *fakeDetail, tst *fakeTester) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Storage: fakeStorage{}}
	if det != nil {
		s.SmartDetail = det
	}
	if tst != nil {
		s.SmartTests = tst
	}
	hd := s.Handler()
	return hd, sessionCookie(t, login(t, hd, "secret"))
}

func TestDiskSmartDetail(t *testing.T) {
	h, c := newSmartServer(t, &fakeDetail{}, &fakeTester{})
	if rec := do(h, "GET", "/api/v1/storage/disks/wwn-0x1/smart", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/storage/disks/wwn-0x1/smart", "", nil, c)
	var d smart.Detail
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &d) != nil || *d.Model != "model-of-sda" {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}
	for path, code := range map[string]string{"wwn-0x9": "disk_not_found", "usb-stick": "smart_unsupported"} {
		rec := do(h, "GET", "/api/v1/storage/disks/"+path+"/smart", "", nil, c)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestDiskSmartTest(t *testing.T) {
	det, tst := &fakeDetail{}, &fakeTester{}
	h, c := newSmartServer(t, det, tst)
	if rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/smart/test", `{"type":"short"}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("without CSRF header = %d", rec.Code)
	}
	rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/smart/test", `{"type":"long"}`, xrw, c)
	if rec.Code != http.StatusAccepted || len(tst.calls) != 1 || tst.calls[0] != "wwn-0x1 long" || len(det.invalidated) != 1 {
		t.Fatalf("start: %d %s calls=%v inv=%v", rec.Code, rec.Body, tst.calls, det.invalidated)
	}
	for _, body := range []string{`{"type":"offline"}`, `{"type":"short","device":"/dev/sdb"}`, `nope`} {
		if rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/smart/test", body, xrw, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, rec.Code)
		}
	}
	if rec := do(h, "POST", "/api/v1/storage/disks/usb-stick/smart/test", `{"type":"short"}`, xrw, c); rec.Code != http.StatusNotFound {
		t.Errorf("usb = %d", rec.Code)
	}
	if len(tst.calls) != 1 {
		t.Fatalf("rejected requests must not reach the helper: %v", tst.calls)
	}

	tst.err = &helper.Error{Code: helper.CodeTestRunning, Message: "běží"}
	if rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/smart/test", `{"type":"short"}`, xrw, c); rec.Code != http.StatusConflict {
		t.Errorf("running = %d", rec.Code)
	}
	h, c = newSmartServer(t, nil, nil)
	if rec := do(h, "POST", "/api/v1/storage/disks/wwn-0x1/smart/test", `{"type":"short"}`, xrw, c); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no helper = %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/storage/disks/wwn-0x1/smart", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no detail = %d", rec.Code)
	}
}
