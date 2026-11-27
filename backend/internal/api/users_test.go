package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/helper"
)

type fakeUsers struct {
	calls []string
	err   error
}

func (f *fakeUsers) rec(c string) (helper.UserList, error) {
	f.calls = append(f.calls, c)
	return helper.UserList{Users: []helper.UserInfo{}, Groups: []helper.GroupInfo{{Name: "rodina"}}}, f.err
}
func (f *fakeUsers) UserList(context.Context) (helper.UserList, error) { return f.rec("list") }
func (f *fakeUsers) UserCreate(_ context.Context, u helper.UserCreate) (helper.UserList, error) {
	return f.rec("create " + u.Name + " " + strings.Join(u.Groups, ","))
}
func (f *fakeUsers) UserUpdate(_ context.Context, u helper.UserUpdate) (helper.UserList, error) {
	return f.rec("update " + u.Name)
}
func (f *fakeUsers) UserSetPassword(_ context.Context, name, _ string) (helper.PasswordResult, error) {
	_, err := f.rec("password " + name)
	return helper.PasswordResult{}, err
}
func (f *fakeUsers) UserSetDisabled(_ context.Context, name string, d bool) (helper.UserList, error) {
	return f.rec("disabled " + name + map[bool]string{true: " on", false: " off"}[d])
}
func (f *fakeUsers) UserDelete(_ context.Context, name string) (helper.UserList, error) {
	return f.rec("delete " + name)
}
func (f *fakeUsers) GroupCreate(_ context.Context, name string) (helper.UserList, error) {
	return f.rec("group " + name)
}
func (f *fakeUsers) GroupSetMembers(_ context.Context, name string, m []string) (helper.UserList, error) {
	return f.rec("members " + name + " " + strings.Join(m, ","))
}
func (f *fakeUsers) GroupDelete(_ context.Context, name string) (helper.UserList, error) {
	return f.rec("groupdel " + name)
}

func newUsersServer(t *testing.T, u UserAdmin) (*Server, http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Users: u}
	hd := s.Handler()
	return s, hd, sessionCookie(t, login(t, hd, "secret"))
}

const goodPassword = `"password":"correct horse battery"`

func TestUsersRequireSessionAndCSRF(t *testing.T) {
	_, h, c := newUsersServer(t, &fakeUsers{})
	if rec := do(h, "GET", "/api/v1/users", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/users", `{"name":"jana",`+goodPassword+`}`, nil, c); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF = %d", rec.Code)
	}
	_, h, c = newUsersServer(t, nil)
	if rec := do(h, "GET", "/api/v1/users", "", nil, c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no backend = %d", rec.Code)
	}
}

func TestUserCreateValidatesBeforeHelper(t *testing.T) {
	f := &fakeUsers{}
	_, h, c := newUsersServer(t, f)
	for _, body := range []string{
		`{"name":"Jana",` + goodPassword + `}`,
		`{"name":"nas-admin",` + goodPassword + `}`,
		`{"name":"jana","password":""}`,
		`{"name":"jana","password":"correct horse\nroot:x"}`,
		`{"name":"jana","fullName":"a:b",` + goodPassword + `}`,
		`{"name":"jana",` + goodPassword + `,"uid":0}`,
	} {
		if rec := do(h, "POST", "/api/v1/users", body, xrw, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, rec.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("helper called: %v", f.calls)
	}
	rec := do(h, "POST", "/api/v1/users", `{"name":"jana","fullName":"Jana",`+goodPassword+`,"groups":["rodina","rodina"]}`, xrw, c)
	if rec.Code != http.StatusCreated || f.calls[0] != "create jana rodina" {
		t.Fatalf("create = %d %s %v", rec.Code, rec.Body, f.calls)
	}
	if strings.Contains(rec.Body.String(), "horse") {
		t.Fatal("password echoed")
	}
}

func TestPasswordChangeEndsSessionsOfThatUser(t *testing.T) {
	f := &fakeUsers{}
	s, h, c := newUsersServer(t, f)
	jana, _ := s.Sessions.Create("jana", auth.Meta{})
	if rec := do(h, "PUT", "/api/v1/users/jana/password", `{`+goodPassword+`}`, xrw, c); rec.Code != 200 {
		t.Fatalf("password = %d %s", rec.Code, rec.Body)
	}
	if _, _, ok := s.Sessions.Lookup(jana); ok {
		t.Error("jana's session survived the password change")
	}
	if _, _, ok := s.Sessions.Lookup(c.Value); !ok {
		t.Error("admin session ended")
	}
	jana, _ = s.Sessions.Create("jana", auth.Meta{})
	if rec := do(h, "PUT", "/api/v1/users/jana/disabled", `{"disabled":true}`, xrw, c); rec.Code != 200 {
		t.Fatalf("disable = %d", rec.Code)
	}
	if _, _, ok := s.Sessions.Lookup(jana); ok {
		t.Error("session survived disable")
	}
}

func TestCannotDeleteOrDisableOwnAccount(t *testing.T) {
	f := &fakeUsers{}
	_, h, c := newUsersServer(t, f)
	if rec := do(h, "DELETE", "/api/v1/users/admin", "", xrw, c); rec.Code != http.StatusBadRequest {
		t.Errorf("delete self = %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/v1/users/admin/disabled", `{"disabled":true}`, xrw, c); rec.Code != http.StatusBadRequest {
		t.Errorf("disable self = %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/users/Bad..Name", "", xrw, c); rec.Code != http.StatusNotFound {
		t.Errorf("bad name = %d", rec.Code)
	}
	if len(f.calls) != 0 {
		t.Errorf("helper called: %v", f.calls)
	}
}

func TestGroupsAndHelperErrors(t *testing.T) {
	f := &fakeUsers{}
	_, h, c := newUsersServer(t, f)
	if rec := do(h, "GET", "/api/v1/groups", "", nil, c); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"rodina"`) ||
		strings.Contains(rec.Body.String(), `"users"`) {
		t.Fatalf("groups = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/v1/groups/rodina/members", `{"members":["jana"]}`, xrw, c); rec.Code != 200 {
		t.Fatalf("members = %d", rec.Code)
	}
	f.err = &helper.Error{Code: helper.CodeNotFound, Message: "Skupina neexistuje."}
	if rec := do(h, "DELETE", "/api/v1/groups/sudo", "", xrw, c); rec.Code != http.StatusNotFound {
		t.Fatalf("helper not_found = %d", rec.Code)
	}
}
