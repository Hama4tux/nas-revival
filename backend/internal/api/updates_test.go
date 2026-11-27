package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/updates"
)

type fakeUpdates struct {
	err      error
	checks   int
	listedAt *string
}

func (f *fakeUpdates) state() helper.UpdatesStatus {
	return helper.UpdatesStatus{State: updates.State{Packages: []updates.Package{{Name: "openssl", To: "2", Security: true},
		{Name: "tzdata", To: "3"}}, KeptBack: []string{}, RestartPackages: []string{}}}
}
func (f *fakeUpdates) UpdatesGet(context.Context) (helper.UpdatesStatus, error) {
	return f.state(), f.err
}
func (f *fakeUpdates) UpdatesCheck(context.Context) (helper.UpdatesStatus, error) {
	f.checks++
	return f.state(), f.err
}
func (f *fakeUpdates) UpdatesUpgrade(_ context.Context, at string) (helper.UpdatesStatus, error) {
	f.listedAt = &at
	return f.state(), f.err
}

func newUpdatesServer(t *testing.T) (http.Handler, *fakeUpdates) {
	t.Helper()
	s, h, _, _ := newRoleServer(t)
	f := &fakeUpdates{}
	s.Updates = f
	return h, f
}

func TestUpdatesRoles(t *testing.T) {
	h, f := newUpdatesServer(t)
	petr, eva := loginAs(t, h, "petr"), loginAs(t, h, "eva")
	if rec := do(h, "GET", "/api/v1/updates", "", xrw, petr); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"versions"`) {
		t.Errorf("viewer get: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/updates", "", xrw, eva); rec.Code != 403 {
		t.Errorf("user get: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/updates/check", "", xrw, petr); rec.Code != 403 {
		t.Errorf("viewer check: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/updates/upgrade", `{"listedAt":"x"}`, xrw, petr); rec.Code != 403 || f.listedAt != nil {
		t.Errorf("viewer upgrade: %d", rec.Code)
	}
	if f.checks != 0 {
		t.Error("helper asked")
	}
}

func TestUpdatesCheckAndUpgrade(t *testing.T) {
	h, f := newUpdatesServer(t)
	jana := loginAs(t, h, "jana")
	if rec := do(h, "POST", "/api/v1/updates/check", "", xrw, jana); rec.Code != 202 || f.checks != 1 {
		t.Errorf("check: %d %d", rec.Code, f.checks)
	}
	rec := do(h, "POST", "/api/v1/updates/upgrade", `{"listedAt":"2026-11-27T12:00:00Z"}`, xrw, jana)
	if rec.Code != 202 || f.listedAt == nil || *f.listedAt != "2026-11-27T12:00:00Z" {
		t.Errorf("upgrade: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/updates/upgrade", `{"listedAt":"x","all":true}`, xrw, jana); rec.Code != 400 {
		t.Errorf("unknown field: %d", rec.Code)
	}
}

func TestUpdatesHelperConflicts(t *testing.T) {
	h, f := newUpdatesServer(t)
	jana := loginAs(t, h, "jana")
	for _, c := range []string{helper.CodeUpdatesRunning, helper.CodeUpdatesStale, helper.CodeUpdatesSpace} {
		f.err = &helper.Error{Code: c, Message: "x"}
		if rec := do(h, "POST", "/api/v1/updates/upgrade", `{"listedAt":"x"}`, xrw, jana); rec.Code != 409 || !strings.Contains(rec.Body.String(), c) {
			t.Errorf("%s: %d %s", c, rec.Code, rec.Body)
		}
	}
}

func TestUpdatesUnavailable(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	s.Updates = nil
	if rec := do(h, "GET", "/api/v1/updates", "", xrw, loginAs(t, h, "jana")); rec.Code != 503 {
		t.Errorf("%d", rec.Code)
	}
}
