package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"nas-revival/backend/internal/notify"
)

func TestNotifications(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	if rec := do(h, "GET", "/api/v1/notifications", "", nil, loginAs(t, h, "jana")); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without centre = %d", rec.Code)
	}
	c := &notify.Center{Now: time.Now}
	c.Update([]notify.Problem{{Severity: "crit", Code: "backupFailed", Subject: "web"}, {Severity: "warn", Code: "fsWarn", Subject: "/"}}, nil)
	s.Notifications = c
	h = s.Handler()
	jana, petr, eva := loginAs(t, h, "jana"), loginAs(t, h, "petr"), loginAs(t, h, "eva")

	list := func(ck *http.Cookie) notify.List {
		t.Helper()
		rec := do(h, "GET", "/api/v1/notifications", "", nil, ck)
		var l notify.List
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("list = %d %s", rec.Code, rec.Body)
		}
		return l
	}
	l := list(petr)
	if len(l.Items) != 2 || l.Unread != 2 || l.Active != 2 {
		t.Fatalf("list = %+v", l)
	}
	if rec := do(h, "GET", "/api/v1/notifications", "", nil, eva); rec.Code != http.StatusForbidden {
		t.Fatalf("standard user = %d", rec.Code)
	}
	// The read-only administrator marks their own notifications read.
	if rec := do(h, "POST", "/api/v1/notifications/read", `{"ids":["`+l.Items[0].ID+`"]}`, xrw, petr); rec.Code != http.StatusNoContent {
		t.Fatalf("read ids = %d %s", rec.Code, rec.Body)
	}
	if got := list(petr).Unread; got != 1 {
		t.Fatalf("petr unread = %d", got)
	}
	if rec := do(h, "POST", "/api/v1/notifications/read", `{"all":true}`, xrw, jana); rec.Code != http.StatusNoContent {
		t.Fatalf("read all = %d", rec.Code)
	}
	if list(jana).Unread != 0 || list(petr).Unread != 1 {
		t.Fatal("read state not per user")
	}
	for _, body := range []string{`{}`, `{"all":true,"ids":["a"]}`, `{"ids":[""]}`, `{"ids":["x"],"extra":1}`} {
		if rec := do(h, "POST", "/api/v1/notifications/read", body, xrw, jana); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, rec.Code)
		}
	}
	if rec := do(h, "POST", "/api/v1/notifications/read", `{"all":true}`, map[string]string{"Content-Type": "application/json"}, jana); rec.Code != http.StatusForbidden {
		t.Errorf("without CSRF header = %d", rec.Code)
	}
}
