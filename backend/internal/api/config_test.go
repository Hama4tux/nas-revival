package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/helper"
)

type fakeConfig struct {
	rollbacks []string
	imported  []string
	err       error
}

func (f *fakeConfig) ConfigExport(context.Context) (helper.ConfigArchive, error) {
	return helper.ConfigArchive{Format: helper.ArchiveFormat, Schema: 1, Hostname: "nas/../x",
		Created: time.Date(2026, 11, 27, 9, 5, 0, 0, time.UTC), Files: []helper.ArchiveFile{}}, f.err
}
func (f *fakeConfig) ConfigImportPreview(_ context.Context, a json.RawMessage) (helper.ConfigImportPreview, error) {
	f.imported = append(f.imported, "preview:"+string(a))
	return helper.ConfigImportPreview{Hostname: "nas"}, f.err
}
func (f *fakeConfig) ConfigImport(_ context.Context, a json.RawMessage, takeover bool) (helper.ConfigImportResult, error) {
	f.imported = append(f.imported, fmt.Sprintf("import:%s:%v", a, takeover))
	return helper.ConfigImportResult{Restored: []string{"smb"}}, f.err
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

func TestConfigExportIsAnAttachment(t *testing.T) {
	h, c := newConfigServer(t, &fakeConfig{})
	rec := do(h, "GET", "/api/v1/config/export", "", nil, c)
	cd := rec.Header().Get("Content-Disposition")
	if rec.Code != 200 || !strings.Contains(cd, `filename="nas-revival-config-nasx-2026112`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("export = %d %q %q", rec.Code, cd, rec.Header().Get("Cache-Control"))
	}
	var a helper.ConfigArchive
	if json.Unmarshal(rec.Body.Bytes(), &a) != nil || a.Format != helper.ArchiveFormat {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestConfigImportPassesTheFileThrough(t *testing.T) {
	f := &fakeConfig{}
	h, c := newConfigServer(t, f)
	if rec := do(h, "POST", "/api/v1/config/import/preview", `{"archive":{"format":"x"}}`, xrw, c); rec.Code != 200 {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/config/import", `{"archive":{"format":"x"},"takeover":true}`, xrw, c); rec.Code != 200 {
		t.Fatalf("import = %d %s", rec.Code, rec.Body)
	}
	if len(f.imported) != 2 || f.imported[0] != `preview:{"format":"x"}` || f.imported[1] != `import:{"format":"x"}:true` {
		t.Fatalf("calls = %q", f.imported)
	}
	for _, bad := range []string{`{}`, `{"archive":"text"}`, `{"archive":[1]}`, `{"archive":{},"extra":1}`} {
		if rec := do(h, "POST", "/api/v1/config/import", bad, xrw, c); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d", bad, rec.Code)
		}
	}
	big := `{"archive":{"x":"` + strings.Repeat("a", helper.MaxArchive+2048) + `"}}`
	if rec := do(h, "POST", "/api/v1/config/import", big, xrw, c); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large = %d", rec.Code)
	}
}
