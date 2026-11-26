package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/shares"
)

type fakeShares struct {
	calls []string
	err   error
}

func (f *fakeShares) rec(c string) (helper.ShareList, error) {
	f.calls = append(f.calls, c)
	return helper.ShareList{Shares: []helper.ShareInfo{}, Volumes: []shares.Volume{}}, f.err
}
func (f *fakeShares) ShareList(context.Context) (helper.ShareList, error) { return f.rec("list") }
func (f *fakeShares) ShareCreate(_ context.Context, s shares.Share) (helper.ShareList, error) {
	return f.rec("create " + s.Name + " " + s.Folder)
}
func (f *fakeShares) ShareUpdate(_ context.Context, s shares.Share) (helper.ShareList, error) {
	return f.rec("update " + s.Name)
}
func (f *fakeShares) ShareDelete(_ context.Context, d helper.ShareDelete) (helper.ShareDeleteResult, error) {
	l, err := f.rec("delete " + d.Name + " " + d.ConfirmName)
	return helper.ShareDeleteResult{ShareList: l, DataRemoval: "none"}, err
}
func (f *fakeShares) ShareRecycleCleanup(context.Context) (shares.RecycleResult, error) {
	_, err := f.rec("recycle")
	return shares.RecycleResult{RemovedFiles: 2, RemovedBytes: 10}, err
}

func newSharesServer(t *testing.T, sh ShareAdmin) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Shares: sh}
	hd := s.Handler()
	return hd, sessionCookie(t, login(t, hd, "secret"))
}

func TestSharesEndpoints(t *testing.T) {
	f := &fakeShares{}
	h, c := newSharesServer(t, f)
	if rec := do(h, "GET", "/api/v1/shares", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/shares", `{"name":"Fotky","volume":"/srv/data"}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF = %d", rec.Code)
	}
	steps := []struct {
		method, path, body string
		code               int
		call               string
	}{
		{"GET", "/api/v1/shares", "", 200, "list"},
		{"POST", "/api/v1/shares", `{"name":"Fotky","volume":"/srv/data"}`, 201, "create Fotky Fotky"},
		{"PUT", "/api/v1/shares/Fotky", `{"name":"Other","volume":"/srv/data","folder":"Fotky"}`, 200, "update Fotky"},
		{"DELETE", "/api/v1/shares/Fotky", `{"deleteData":true,"confirmName":"Fotky"}`, 200, "delete Fotky Fotky"},
		{"DELETE", "/api/v1/shares/Fotky", ``, 200, "delete Fotky "},
		{"POST", "/api/v1/shares/recycle-cleanup", ``, 200, "recycle"},
	}
	for _, s := range steps {
		f.calls = nil
		rec := do(h, s.method, s.path, s.body, xrw, c)
		if rec.Code != s.code || len(f.calls) != 1 || f.calls[0] != s.call {
			t.Errorf("%s %s = %d calls %v (%s)", s.method, s.path, rec.Code, f.calls, rec.Body)
		}
	}
}

func TestSharesValidateBeforeHelper(t *testing.T) {
	f := &fakeShares{}
	h, c := newSharesServer(t, f)
	for _, body := range []string{
		`{"name":"a b","volume":"/srv/data"}`,
		`{"name":"global","volume":"/srv/data"}`,
		`{"name":"x","volume":"/etc"}`,
		`{"name":"x","volume":"/srv/data","folder":"../x"}`,
		`{"name":"x","volume":"/srv/data","permissions":[{"kind":"group","name":"rodina","access":"none"}]}`,
		`{"name":"x","volume":"/srv/data","path":"/etc"}`,
	} {
		if rec := do(h, "POST", "/api/v1/shares", body, xrw, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, rec.Code)
		}
	}
	if rec := do(h, "PUT", "/api/v1/shares/a%20b", `{"volume":"/srv/data"}`, xrw, c); rec.Code != http.StatusNotFound {
		t.Errorf("bad path name = %d", rec.Code)
	}
	rec := do(h, "DELETE", "/api/v1/shares/Fotky", `{"deleteData":true,"confirmName":"x"}`, xrw, c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "confirmName") {
		t.Errorf("confirm = %d %s", rec.Code, rec.Body)
	}
	big := `{"name":"x","volume":"/srv/data","description":"` + strings.Repeat("a", 9000) + `"}`
	if rec := do(h, "POST", "/api/v1/shares", big, xrw, c); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body = %d", rec.Code)
	}
	if len(f.calls) != 0 {
		t.Fatalf("helper was called: %v", f.calls)
	}
}

func TestSharesUnavailableAndHelperErrors(t *testing.T) {
	h, c := newSharesServer(t, nil)
	if rec := do(h, "GET", "/api/v1/shares", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no backend = %d", rec.Code)
	}
	f := &fakeShares{err: &helper.Error{Code: helper.CodeNotFound, Message: "x"}}
	h, c = newSharesServer(t, f)
	if rec := do(h, "PUT", "/api/v1/shares/Fotky", `{"volume":"/srv/data"}`, xrw, c); rec.Code != http.StatusNotFound {
		t.Fatalf("not found = %d", rec.Code)
	}
}

func TestSharesRolePolicy(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	// The role server has no Shares backend: allowed callers get 503, denied 403.
	for _, c := range []struct {
		method, path, body  string
		admin, viewer, user int
	}{
		{"GET", "/api/v1/shares", "", 503, 503, 403},
		{"POST", "/api/v1/shares", `{"name":"x","volume":"/srv/data"}`, 503, 403, 403},
		{"PUT", "/api/v1/shares/x", `{"volume":"/srv/data"}`, 503, 403, 403},
		{"DELETE", "/api/v1/shares/x", `{}`, 503, 403, 403},
		{"POST", "/api/v1/shares/recycle-cleanup", ``, 503, 403, 403},
	} {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			if rec := do(h, c.method, c.path, c.body, xrw, cookies[name]); rec.Code != want {
				t.Errorf("%s %s as %s = %d, want %d", c.method, c.path, name, rec.Code, want)
			}
		}
	}
}
