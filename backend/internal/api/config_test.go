package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/helper"
)

type fakeConfig struct {
	rollbacks []string
	err       error
}

func (f *fakeConfig) ConfigHistory(ctx context.Context) (config.History, error) {
	return config.History{Entries: []config.Entry{{ID: 2, Kind: config.KindChange}}, Drift: []config.Drift{}}, f.err
}
func (f *fakeConfig) ConfigVersion(_ context.Context, id int) (config.Detail, error) {
	if id != 2 {
		return config.Detail{}, &helper.Error{Code: helper.CodeNotFound, Message: "nenalezeno"}
	}
	return config.Detail{Entry: config.Entry{ID: 2}}, nil
}
func (f *fakeConfig) ConfigRollback(ctx context.Context, id int, takeover bool) (helper.ConfigRollbackResult, error) {
	if f.err != nil {
		return helper.ConfigRollbackResult{}, f.err
	}
	f.rollbacks = append(f.rollbacks, map[bool]string{true: "takeover", false: "plain"}[takeover])
	return helper.ConfigRollbackResult{RollbackOf: id, Restored: []string{"time"}}, nil
}

func newConfigServer(t *testing.T, c ConfigHistory) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Config: c}
	hd := s.Handler()
	return hd, sessionCookie(t, login(t, hd, "secret"))
}

func TestConfigRequiresSessionAndCSRF(t *testing.T) {
	h, c := newConfigServer(t, &fakeConfig{})
	if rec := do(h, "GET", "/api/v1/config/history", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/config/rollback/2", `{}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF header = %d", rec.Code)
	}
}

func TestConfigHistoryAndVersion(t *testing.T) {
	h, c := newConfigServer(t, &fakeConfig{})
	rec := do(h, "GET", "/api/v1/config/history", "", nil, c)
	var hist config.History
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &hist) != nil || hist.Entries[0].ID != 2 {
		t.Fatalf("history = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/config/history/2", "", nil, c); rec.Code != 200 {
		t.Fatalf("version = %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/config/history/7", "", nil, c); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown version = %d", rec.Code)
	}
	for _, bad := range []string{"0", "-1", "02", "abc", "1e3"} {
		if rec := do(h, "GET", "/api/v1/config/history/"+bad, "", nil, c); rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q = %d", bad, rec.Code)
		}
	}
}

func TestConfigRollback(t *testing.T) {
	f := &fakeConfig{}
	h, c := newConfigServer(t, f)
	if rec := do(h, "POST", "/api/v1/config/rollback/2", `{"takeover":true}`, xrw, c); rec.Code != 200 {
		t.Fatalf("rollback = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/config/rollback/2", `{"path":"/etc/shadow"}`, xrw, c); rec.Code != 400 {
		t.Fatalf("unknown field = %d", rec.Code)
	}
	if len(f.rollbacks) != 1 || f.rollbacks[0] != "takeover" {
		t.Fatalf("rollbacks = %v", f.rollbacks)
	}
	f.err = &helper.Error{Code: helper.CodeConfigDrift, Message: "změněno"}
	if rec := do(h, "POST", "/api/v1/config/rollback/2", `{}`, xrw, c); rec.Code != http.StatusConflict {
		t.Fatalf("drift = %d", rec.Code)
	}
}
