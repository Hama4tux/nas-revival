package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSecuritySessions(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	jana := loginAs(t, h, "jana")
	jana2 := loginAs(t, h, "jana")
	petr := loginAs(t, h, "petr")

	if rec := do(h, "GET", "/api/v1/security/sessions", "", xrw, petr); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer lists sessions: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/security/advisor", "", xrw, petr); rec.Code != http.StatusOK {
		t.Fatalf("viewer advisor: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/v1/security/sessions/revoke-others", "", xrw, petr); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer revokes: %d", rec.Code)
	}

	rec := do(h, "GET", "/api/v1/security/sessions", "", xrw, jana)
	var body struct {
		Sessions []struct {
			ID, Username, Client string
			Current              bool
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Sessions) != 3 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var petrID string
	current := 0
	for _, v := range body.Sessions {
		if v.Current {
			current++
		}
		if v.Username == "petr" {
			petrID = v.ID
		}
		if v.Client == "" {
			t.Errorf("client address missing: %+v", v)
		}
	}
	if current != 1 || petrID == "" {
		t.Fatalf("current=%d petr=%q", current, petrID)
	}

	if rec := do(h, "DELETE", "/api/v1/security/sessions/"+petrID, "", xrw, jana); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/session", "", xrw, petr); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still works: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/v1/security/sessions/"+petrID, "", xrw, jana); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke twice: %d", rec.Code)
	}

	rec = do(h, "POST", "/api/v1/security/sessions/revoke-others", "", xrw, jana)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"revoked\":1}\n" {
		t.Fatalf("revoke others: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/v1/session", "", xrw, jana2); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session of the same user survived: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/v1/session", "", xrw, jana); rec.Code != http.StatusOK {
		t.Fatalf("own session ended: %d", rec.Code)
	}
}

func TestSecurityRevokeOwnClearsCookie(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	jana := loginAs(t, h, "jana")
	id := s.Sessions.ID(jana.Value)
	rec := do(h, "DELETE", "/api/v1/security/sessions/"+id, "", xrw, jana)
	if rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) != 1 || rec.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("own revoke: %d %v", rec.Code, rec.Result().Cookies())
	}
	if rec := do(h, "GET", "/api/v1/session", "", xrw, jana); rec.Code != http.StatusUnauthorized {
		t.Fatalf("own revoked session works: %d", rec.Code)
	}
}

func TestSecurityAdvisorUnknownWithoutBackends(t *testing.T) {
	_, h, _, _ := newRoleServer(t)
	rec := do(h, "GET", "/api/v1/security/advisor", "", xrw, loginAs(t, h, "jana"))
	var body struct {
		Findings []struct{ ID, Status string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Findings) == 0 {
		t.Fatalf("advisor: %d %s", rec.Code, rec.Body)
	}
	for _, f := range body.Findings {
		if f.ID == "ssh.rootPassword" && f.Status != "unknown" {
			t.Errorf("ssh without backend: %s", f.Status)
		}
	}
}
