// Package helper implements the privileged helper (ADR 0003): the root-side
// socket server, the validated network and time settings, and the client the
// web process uses to reach it.
package helper

// Error codes shared by helper responses and the client.
const (
	CodeInvalid           = "invalid"
	CodeChangePending     = "change_pending"
	CodeNothingPending    = "nothing_pending"
	CodeHelperUnavailable = "helper_unavailable"
	CodeInternal          = "internal"
	CodeNotFound          = "not_found"
	CodeConfigDrift       = "config_drift"
	CodeNotInstalled      = "not_installed"
)

// Error is a typed error that travels over the helper protocol.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func invalid(field, msg string) *Error {
	return &Error{Code: CodeInvalid, Message: msg, Field: field}
}

func internalErr(msg string) *Error { return &Error{Code: CodeInternal, Message: msg} }

func notFound(msg string) *Error { return &Error{Code: CodeNotFound, Message: msg} }
