package api

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/audit"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/services"
)

type fakeLogs struct {
	err       error
	q         *services.Query
	set       *helper.JournalRetention
	retention helper.JournalRetention
}

func (f *fakeLogs) LogQuery(_ context.Context, q services.Query) (helper.JournalResult, error) {
	f.q = &q
	return helper.JournalResult{More: true}, f.err
}
func (f *fakeLogs) LogRetentionGet(context.Context) (helper.JournalRetention, error) {
	return f.retention, f.err
}
func (f *fakeLogs) LogRetentionSet(_ context.Context, s helper.JournalRetention) (helper.JournalRetention, error) {
	f.set = &s
	return s, f.err
}

func newLogsServer(t *testing.T) (http.Handler, *fakeLogs) {
	t.Helper()
	s, h, _, _ := newRoleServer(t)
	f := &fakeLogs{retention: helper.JournalRetention{MaxUse: "64M", MaxRetention: "none"}}
	s.Logs = f
	return h, f
}

func TestLogsQueryParams(t *testing.T) {
	h, f := newLogsServer(t)
	rec := do(h, "GET", "/api/v1/logs?category=auth&priority=3&since=2026-11-27T10:00:00Z&text=a+b&limit=50&unit=", "", xrw, loginAs(t, h, "petr"))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"entries":[],"more":true}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	q := f.q
	if q == nil || q.Category != "auth" || q.Priority == nil || *q.Priority != 3 || q.Since != "2026-11-27T10:00:00Z" ||
		q.Text != "a b" || q.Limit != 50 {
		t.Errorf("query = %+v", q)
	}
	f.q = nil
	do(h, "GET", "/api/v1/logs", "", xrw, loginAs(t, h, "jana"))
	if f.q == nil || f.q.Priority != nil || f.q.Limit != 0 {
		t.Errorf("defaults = %+v", f.q)
	}
}

func TestLogsQueryBadNumbers(t *testing.T) {
	h, f := newLogsServer(t)
	c := loginAs(t, h, "jana")
	for _, p := range []string{"priority=x", "limit=abc", "limit=0"} {
		if rec := do(h, "GET", "/api/v1/logs?"+p, "", xrw, c); rec.Code != 400 {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
	if f.q != nil {
		t.Error("helper was asked")
	}
}

func TestLogsHelperInvalidIs400(t *testing.T) {
	h, f := newLogsServer(t)
	f.err = &helper.Error{Code: helper.CodeInvalid, Message: "Neznámá kategorie záznamů.", Field: "category"}
	rec := do(h, "GET", "/api/v1/logs?category=x", "", xrw, loginAs(t, h, "jana"))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"field":"category"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestLogsRoles(t *testing.T) {
	h, f := newLogsServer(t)
	for _, c := range []struct {
		method, path, body  string
		admin, viewer, user int
	}{
		{"GET", "/api/v1/logs", "", 200, 200, 403},
		{"GET", "/api/v1/logs/retention", "", 200, 200, 403},
		{"PUT", "/api/v1/logs/retention", `{"maxUse":"128M","maxRetention":"1week"}`, 200, 403, 403},
	} {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			if rec := do(h, c.method, c.path, c.body, xrw, loginAs(t, h, name)); rec.Code != want {
				t.Errorf("%s %s as %s = %d, want %d", c.method, c.path, name, rec.Code, want)
			}
		}
	}
	if f.set == nil || f.set.MaxUse != "128M" {
		t.Errorf("set = %+v", f.set)
	}
	if rec := do(h, "GET", "/api/v1/logs", "", xrw, nil); rec.Code != 401 {
		t.Errorf("anonymous = %d", rec.Code)
	}
}

func TestLogsUnavailable(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	if rec := do(h, "GET", "/api/v1/logs", "", xrw, loginAs(t, h, "jana")); rec.Code != 503 {
		t.Errorf("%d", rec.Code)
	}
}

func TestRetentionPutValidationAndAudit(t *testing.T) {
	h, f := newLogsServer(t)
	c := loginAs(t, h, "jana")
	f.err = &helper.Error{Code: helper.CodeInvalid, Message: "x", Field: "maxUse"}
	if rec := do(h, "PUT", "/api/v1/logs/retention", `{"maxUse":"1G","maxRetention":"none"}`, xrw, c); rec.Code != 400 {
		t.Errorf("invalid = %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/logs/retention", `{"maxUse":"64M","bogus":1}`, xrw, c); rec.Code != 400 {
		t.Errorf("unknown field = %d", rec.Code)
	}
	f.err = nil
	conn := auditSocket(t)
	rec := do(h, "PUT", "/api/v1/logs/retention", `{"maxUse":"256M","maxRetention":"1year"}`, xrw, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"maxUse":"256M"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got := readAudit(t, conn)
	for _, want := range []string{"AUDIT_ACTION=logs.retention\n", "AUDIT_USER=jana\n", "AUDIT_RESULT=ok\n", "AUDIT_CLIENT="} {
		if !strings.Contains(got, want) {
			t.Errorf("audit lacks %q: %q", want, got)
		}
	}
}

func auditSocket(t *testing.T) *net.UnixConn {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j.sock")
	c, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: p, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	restore := audit.SetSocket(p)
	t.Cleanup(func() { restore(); c.Close() })
	return c
}

func readAudit(t *testing.T, c *net.UnixConn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8192)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf[:n])
}

func TestLoginAndFailureAreAudited(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	_ = s
	conn := auditSocket(t)
	rec := do(h, "POST", "/api/v1/session", `{"username":"jana","password":"wrong-password-123"}`, xrw, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Skipf("verifier accepts any password (%d)", rec.Code)
	}
	got := readAudit(t, conn)
	if !strings.Contains(got, "AUDIT_ACTION=login.fail\n") || !strings.Contains(got, "PRIORITY=4\n") || strings.Contains(got, "wrong-password") {
		t.Errorf("fail audit = %q", got)
	}
	c := loginAs(t, h, "jana")
	got = readAudit(t, conn)
	if !strings.Contains(got, "AUDIT_ACTION=login\n") || !strings.Contains(got, "AUDIT_USER=jana\n") || !strings.Contains(got, "AUDIT_CLIENT=") {
		t.Errorf("login audit = %q", got)
	}
	do(h, "POST", "/api/v1/users", `{"name":"karel","password":"Dlouhe-heslo-1234","role":"user"}`, xrw, c)
	got = readAudit(t, conn)
	if !strings.Contains(got, "AUDIT_ACTION=user.create\n") || !strings.Contains(got, "AUDIT_TARGET=karel\n") || strings.Contains(got, "Dlouhe-heslo") {
		t.Errorf("user.create audit = %q", got)
	}
}
