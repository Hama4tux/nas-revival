package api

import (
	"net/http"

	"nas-revival/backend/internal/audit"
)

// auditOK records a successful administrative action of the logged-in user
// (ADR 0015). msg is a readable Czech sentence without secrets.
func auditOK(r *http.Request, action, target, msg string) {
	audit.Log(audit.Entry{Action: action, User: sessionUser(r), Client: clientIP(r), Target: target,
		Result: audit.OK, Message: msg})
}

func onOff(b bool) string {
	if b {
		return "zapnuto"
	}
	return "vypnuto"
}

func forcedNote(forced bool) string {
	if forced {
		return " (vynuceně)"
	}
	return ""
}

func deleteNote(data bool) string {
	if data {
		return " včetně dat"
	}
	return ""
}
