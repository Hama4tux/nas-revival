// Package audit records administrative actions as structured journal entries
// (ADR 0015). One entry is one datagram on the journal's native socket with
// the fields NAS_REVIVAL_AUDIT=1, AUDIT_ACTION, AUDIT_USER, AUDIT_CLIENT,
// AUDIT_TARGET and AUDIT_RESULT. Without the socket (development machine) the
// entry goes to slog with the same attributes. Values never carry secrets: the
// callers pass only names, addresses and fixed identifiers.
package audit

import (
	"encoding/binary"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// DefaultSocket is the native protocol socket of systemd-journald.
const DefaultSocket = "/run/systemd/journal/socket"

// MaxValue caps one field value (runes).
const MaxValue = 200

// Results of an action.
const (
	OK   = "ok"
	Fail = "fail"
)

// Entry is one administrative action.
type Entry struct {
	Action   string // fixed identifier such as user.create
	User     string // who did it
	Client   string // client address, when known
	Target   string // the object: account, unit, ...
	Result   string // ok or fail
	Message  string // readable sentence
	Priority int    // syslog priority; 0 means the default 6 (info)
}

// Logger writes entries to one journal socket.
type Logger struct {
	Socket string
}

var std = &Logger{Socket: DefaultSocket}

// Log records e on the system journal.
func Log(e Entry) { std.Log(e) }

// SetSocket redirects the package-level Log (tests).
func SetSocket(path string) (restore func()) {
	old := std
	std = &Logger{Socket: path}
	return func() { std = old }
}

// Sanitize makes a value safe for the journal: valid UTF-8, control
// characters replaced by spaces, at most MaxValue runes.
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > MaxValue {
		s = string([]rune(s)[:MaxValue]) + "…"
	}
	return s
}

type field struct{ k, v string }

func (e Entry) fields() []field {
	prio := e.Priority
	if prio < 0 || prio > 7 || (prio == 0 && e.Priority == 0) {
		prio = 6
	}
	msg := e.Message
	if msg == "" {
		msg = e.Action
	}
	f := []field{
		{"MESSAGE", Sanitize(msg)},
		{"PRIORITY", strconv.Itoa(prio)},
		{"SYSLOG_IDENTIFIER", "nas-revival"},
		{"NAS_REVIVAL_AUDIT", "1"},
	}
	for _, o := range []field{{"AUDIT_ACTION", e.Action}, {"AUDIT_USER", e.User}, {"AUDIT_CLIENT", e.Client},
		{"AUDIT_TARGET", e.Target}, {"AUDIT_RESULT", e.Result}} {
		if v := Sanitize(o.v); v != "" {
			f = append(f, field{o.k, v})
		}
	}
	return f
}

// Encode renders fields in the journal native protocol: KEY=value\n, or for
// values with a newline KEY\n + 64-bit little-endian length + value + \n.
func Encode(keys, values []string) []byte {
	var b []byte
	for i, k := range keys {
		v := values[i]
		if strings.ContainsRune(v, '\n') {
			b = append(b, k...)
			b = append(b, '\n')
			b = binary.LittleEndian.AppendUint64(b, uint64(len(v)))
			b = append(b, v...)
			b = append(b, '\n')
			continue
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, v...)
		b = append(b, '\n')
	}
	return b
}

// Log sends e to the journal, or to slog when the socket is unavailable.
func (l *Logger) Log(e Entry) {
	fs := e.fields()
	keys := make([]string, len(fs))
	vals := make([]string, len(fs))
	for i, f := range fs {
		keys[i], vals[i] = f.k, f.v
	}
	if l.send(Encode(keys, vals)) == nil {
		return
	}
	attrs := []any{}
	for _, f := range fs {
		switch f.k {
		case "MESSAGE", "SYSLOG_IDENTIFIER", "NAS_REVIVAL_AUDIT", "PRIORITY":
		default:
			attrs = append(attrs, strings.ToLower(f.k), f.v)
		}
	}
	msg := fs[0].v
	if p, _ := strconv.Atoi(fs[1].v); p <= 4 {
		slog.Warn(msg, attrs...)
	} else {
		slog.Info(msg, attrs...)
	}
}

func (l *Logger) send(b []byte) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: l.Socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = conn.Write(b)
	return err
}
