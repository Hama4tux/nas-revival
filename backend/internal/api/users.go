package api

import (
	"context"
	"net/http"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/helper"
)

// UserAdmin is implemented by the helper client (ops user.*, group.*).
type UserAdmin interface {
	UserList(ctx context.Context) (helper.UserList, error)
	UserCreate(ctx context.Context, u helper.UserCreate) (helper.UserList, error)
	UserUpdate(ctx context.Context, u helper.UserUpdate) (helper.UserList, error)
	UserSetPassword(ctx context.Context, name, password string) (helper.PasswordResult, error)
	UserSetDisabled(ctx context.Context, name string, disabled bool) (helper.UserList, error)
	UserDelete(ctx context.Context, name string) (helper.UserList, error)
	GroupCreate(ctx context.Context, name string) (helper.UserList, error)
	GroupSetMembers(ctx context.Context, name string, members []string) (helper.UserList, error)
	GroupDelete(ctx context.Context, name string) (helper.UserList, error)
}

const usersBodyLimit = 4 << 10

func (s *Server) registerUsers(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/users", s.requireSession(s.usersList))
	api.HandleFunc("POST /api/v1/users", s.requireSession(s.userCreate))
	api.HandleFunc("PUT /api/v1/users/{name}", s.requireSession(s.userUpdate))
	api.HandleFunc("PUT /api/v1/users/{name}/password", s.requireSession(s.userPassword))
	api.HandleFunc("PUT /api/v1/users/{name}/disabled", s.requireSession(s.userDisabled))
	api.HandleFunc("DELETE /api/v1/users/{name}", s.requireSession(s.userDelete))
	api.HandleFunc("GET /api/v1/groups", s.requireSession(s.groupsList))
	api.HandleFunc("POST /api/v1/groups", s.requireSession(s.groupCreate))
	api.HandleFunc("PUT /api/v1/groups/{name}/members", s.requireSession(s.groupMembers))
	api.HandleFunc("DELETE /api/v1/groups/{name}", s.requireSession(s.groupDelete))
}

func (s *Server) usersReady(w http.ResponseWriter) bool {
	if s.Users == nil {
		writeError(w, http.StatusServiceUnavailable, helper.CodeHelperUnavailable, "Služba pro správu uživatelů není dostupná.")
		return false
	}
	return true
}

// pathName validates {name} before it reaches the helper.
func pathName(w http.ResponseWriter, r *http.Request) (string, bool) {
	n := r.PathValue("name")
	if !helper.ValidName(n) {
		writeError(w, http.StatusNotFound, helper.CodeNotFound, "Uživatel nebo skupina neexistuje.")
		return "", false
	}
	return n, true
}

func (s *Server) listResult(w http.ResponseWriter, status int, l helper.UserList, err error) {
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, status, l)
}

func (s *Server) usersList(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	l, err := s.Users.UserList(r.Context())
	s.listResult(w, http.StatusOK, l, err)
}

func (s *Server) groupsList(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	l, err := s.Users.UserList(r.Context())
	if err != nil {
		writeHelperError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": l.Groups})
}

func (s *Server) userCreate(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	var body helper.UserCreate
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	body, err := helper.ValidateUserCreate(body)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	l, err := s.Users.UserCreate(r.Context(), body)
	if err == nil {
		auditOK(r, "user.create", body.Name, "Vytvořen účet "+body.Name)
	}
	s.listResult(w, http.StatusCreated, l, err)
}

func (s *Server) userUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		FullName string        `json:"fullName"`
		Groups   []string      `json:"groups"`
		Role     accounts.Role `json:"role"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	if body.Role != "" && body.Role != accounts.RoleAdmin && name == sessionUser(r) {
		writeInvalid(w, "role", "Nemůžete si odebrat roli administrátora, kterou jste přihlášeni.")
		return
	}
	before, _ := s.roleOf(name)
	l, err := s.Users.UserUpdate(r.Context(), helper.UserUpdate{Name: name, FullName: body.FullName, Groups: body.Groups, Role: body.Role})
	// ADR 0004: a role change ends every session of that user.
	if err == nil && body.Role != "" && body.Role != before {
		s.Sessions.DeleteUser(name)
		auditOK(r, "user.role", name, "Změněna role účtu "+name+" na "+string(body.Role))
	}
	s.listResult(w, http.StatusOK, l, err)
}

func (s *Server) userPassword(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	if err := helper.ValidatePassword(body.Password); err != nil {
		writeHelperError(w, err)
		return
	}
	res, err := s.Users.UserSetPassword(r.Context(), name, body.Password)
	if err != nil {
		writeHelperError(w, err)
		return
	}
	// ADR 0004: a password change ends every session of that user.
	s.Sessions.DeleteUser(name)
	auditOK(r, "user.password", name, "Změněno heslo účtu "+name)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) userDisabled(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		Disabled bool `json:"disabled"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	if body.Disabled && name == sessionUser(r) {
		writeInvalid(w, "", "Nemůžete zakázat účet, kterým jste přihlášeni.")
		return
	}
	l, err := s.Users.UserSetDisabled(r.Context(), name, body.Disabled)
	if err == nil && body.Disabled {
		s.Sessions.DeleteUser(name)
	}
	s.listResult(w, http.StatusOK, l, err)
}

func (s *Server) userDelete(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	if name == sessionUser(r) {
		writeInvalid(w, "", "Nemůžete smazat účet, kterým jste přihlášeni.")
		return
	}
	l, err := s.Users.UserDelete(r.Context(), name)
	if err == nil {
		s.Sessions.DeleteUser(name)
		auditOK(r, "user.delete", name, "Smazán účet "+name)
	}
	s.listResult(w, http.StatusOK, l, err)
}

func (s *Server) groupCreate(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	l, err := s.Users.GroupCreate(r.Context(), body.Name)
	s.listResult(w, http.StatusCreated, l, err)
}

func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	var body struct {
		Members []string `json:"members"`
	}
	if !decodeBody(w, r, usersBodyLimit, &body) {
		return
	}
	l, err := s.Users.GroupSetMembers(r.Context(), name, body.Members)
	s.listResult(w, http.StatusOK, l, err)
}

func (s *Server) groupDelete(w http.ResponseWriter, r *http.Request) {
	if !s.usersReady(w) {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	l, err := s.Users.GroupDelete(r.Context(), name)
	s.listResult(w, http.StatusOK, l, err)
}

func sessionUser(r *http.Request) string {
	sc, _ := r.Context().Value(ctxKey{}).(sessionCtx)
	return sc.username
}
