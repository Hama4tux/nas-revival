package helper

import (
	"context"
	"strings"
	"testing"

	"nas-revival/backend/internal/accounts"
)

func TestCreateWithRoleAddsRoleGroup(t *testing.T) {
	m, f := newUsers(t, false, "")
	if _, err := m.Create(context.Background(), UserCreate{Name: "petr", Password: testPassword, Groups: []string{"rodina"},
		Role: accounts.RoleViewer}, "admin"); err != nil {
		t.Fatal(err)
	}
	// nas-viewer does not exist yet in the base group file.
	if f.calls[0] != "groupadd --system nas-viewer" || !strings.HasSuffix(f.calls[1], "--groups rodina,nas-viewer petr") {
		t.Errorf("calls = %q", f.calls)
	}
}

func TestCreateDefaultsToStandardUser(t *testing.T) {
	c, err := ValidateUserCreate(UserCreate{Name: "petr", Password: testPassword})
	if err != nil || c.Role != accounts.RoleUser {
		t.Fatalf("role = %q, %v", c.Role, err)
	}
	if _, err := ValidateUserCreate(UserCreate{Name: "petr", Password: testPassword, Role: "root"}); codeOf(err) != CodeInvalid {
		t.Fatalf("unknown role: %v", err)
	}
}

func TestUpdateRoleReplacesRoleGroup(t *testing.T) {
	m, f := newUsers(t, false, "")
	f.write(f.p.Group, strings.Replace(baseGroup, "nas-admin:x:991:", "nas-admin:x:991:jana\nnas-viewer:x:992:", 1))
	l, err := m.List(context.Background())
	if err != nil || l.Users[0].Role != accounts.RoleAdmin {
		t.Fatalf("jana listed as %+v, %v", l.Users, err)
	}
	f.calls = nil
	if _, err := m.Update(context.Background(), UserUpdate{Name: "jana", FullName: "Jana", Groups: []string{"rodina"},
		Role: accounts.RoleViewer}, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "usermod --comment Jana --groups sudo,rodina,nas-viewer jana" {
		t.Errorf("usermod = %q", f.calls[0])
	}
	// No role in the request keeps the current one (nas-admin).
	f.calls = nil
	if _, err := m.Update(context.Background(), UserUpdate{Name: "jana", FullName: "Jana"}, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "usermod --comment Jana --groups sudo,nas-admin jana" {
		t.Errorf("keep role: usermod = %q", f.calls[0])
	}
	if _, err := m.Update(context.Background(), UserUpdate{Name: "jana", Role: "superuser"}, "admin"); codeOf(err) != CodeInvalid {
		t.Errorf("unknown role: %v", err)
	}
}
