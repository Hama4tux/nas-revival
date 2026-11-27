package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Journal query (Log Center, ADR 0015). Everything that reaches journalctl is
// built here from validated fields, as an argument array (no shell).

// Journal categories.
const (
	CatAll      = "all"
	CatKernel   = "kernel"
	CatAuth     = "auth"
	CatNas      = "nas"
	CatServices = "services"
	CatAudit    = "audit"
)

// Limits of a query.
const (
	DefaultQueryLimit = 200
	MaxQueryLimit     = 1000
	MaxQueryText      = 100
)

// Units of the NAS Revival itself (also in Catalog).
const (
	WebUnit    = "nas-revival.service"
	HelperUnit = "nas-revival-helper.service"
)

// Query is what the Log Center asks for. Empty fields mean "no filter".
type Query struct {
	Category string `json:"category"`
	Unit     string `json:"unit"`
	Priority *int   `json:"priority"`
	Since    string `json:"since"` // RFC 3339
	Until    string `json:"until"` // RFC 3339
	Text     string `json:"text"`
	Limit    int    `json:"limit"`
}

// QueryError is a rejected query field; Msg is user-facing (Czech).
type QueryError struct{ Field, Msg string }

func (e *QueryError) Error() string { return e.Field + ": " + e.Msg }

// EffectiveLimit is the number of entries the caller gets (default 200).
func (q Query) EffectiveLimit() int {
	if q.Limit == 0 {
		return DefaultQueryLimit
	}
	return q.Limit
}

// QueryFields is the journal field list of a query.
const QueryFields = "PRIORITY,MESSAGE,SYSLOG_IDENTIFIER,_PID,_SYSTEMD_UNIT,NAS_REVIVAL_AUDIT," +
	"AUDIT_ACTION,AUDIT_USER,AUDIT_CLIENT,AUDIT_TARGET,AUDIT_RESULT"

// EscapeGrep makes text a literal pattern: every rune that is not an ASCII
// letter or digit gets a backslash.
func EscapeGrep(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('\\')
		b.WriteRune(r)
	}
	return b.String()
}

func parseQueryTime(field, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, &QueryError{field, "Neplatné datum a čas."}
	}
	return t, nil
}

// BuildJournalArgs validates q and returns the journalctl arguments (without
// the program name). It asks for limit+1 lines so the caller can tell whether
// there are more. catalogUnits is the Services catalog.
func BuildJournalArgs(q Query, catalogUnits []string) ([]string, error) {
	args := []string{"--output=json", "--no-pager", "--quiet", "--reverse"}
	limit := q.EffectiveLimit()
	if limit < 1 || limit > MaxQueryLimit {
		return nil, &QueryError{"limit", "Počet záznamů musí být 1 až 1000."}
	}
	args = append(args, "--lines="+strconv.Itoa(limit+1), "--output-fields="+QueryFields)

	cat := q.Category
	if cat == "" {
		cat = CatAll
	}
	var matches []string
	switch cat {
	case CatAll:
	case CatKernel:
		args = append(args, "--dmesg")
	case CatAuth:
		matches = []string{"SYSLOG_FACILITY=4", "SYSLOG_FACILITY=10"}
	case CatNas:
		matches = []string{"_SYSTEMD_UNIT=" + WebUnit, "_SYSTEMD_UNIT=" + HelperUnit}
	case CatServices:
		for _, u := range catalogUnits {
			args = append(args, "--unit="+u)
		}
	case CatAudit:
		// NAS_REVIVAL_AUDIT is a user field anyone can send, so it only counts together
		// with the trusted unit field.
		matches = []string{"NAS_REVIVAL_AUDIT=1", "_SYSTEMD_UNIT=" + WebUnit, "+",
			"NAS_REVIVAL_AUDIT=1", "_SYSTEMD_UNIT=" + HelperUnit}
	default:
		return nil, &QueryError{"category", "Neznámá kategorie záznamů."}
	}
	if q.Unit != "" {
		if cat != CatAll {
			return nil, &QueryError{"unit", "Službu nelze spojit s kategorií."}
		}
		ok := false
		for _, u := range catalogUnits {
			ok = ok || u == q.Unit
		}
		if !ok {
			return nil, &QueryError{"unit", "Tato služba se nespravuje."}
		}
		args = append(args, "--unit="+q.Unit)
	}

	if q.Priority != nil {
		if *q.Priority < 0 || *q.Priority > 7 {
			return nil, &QueryError{"priority", "Závažnost musí být 0 až 7."}
		}
		args = append(args, "--priority="+strconv.Itoa(*q.Priority))
	}
	var since, until time.Time
	if q.Since != "" {
		t, err := parseQueryTime("since", q.Since)
		if err != nil {
			return nil, err
		}
		since = t
		args = append(args, "--since=@"+strconv.FormatInt(t.Unix(), 10))
	}
	if q.Until != "" {
		t, err := parseQueryTime("until", q.Until)
		if err != nil {
			return nil, err
		}
		until = t
		args = append(args, fmt.Sprintf("--until=@%d.%06d", t.Unix(), t.Nanosecond()/1000))
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return nil, &QueryError{"until", "Konec období je před jeho začátkem."}
	}
	if q.Text != "" {
		if utf8.RuneCountInString(q.Text) > MaxQueryText || !utf8.ValidString(q.Text) {
			return nil, &QueryError{"text", "Hledaný text může mít nejvýše 100 znaků."}
		}
		for _, r := range q.Text {
			if unicode.IsControl(r) {
				return nil, &QueryError{"text", "Hledaný text nesmí obsahovat řídicí znaky."}
			}
		}
		args = append(args, "--grep="+EscapeGrep(q.Text), "--case-sensitive=false")
	}
	return append(args, matches...), nil
}

// Audit is the structured part of an administrative action entry.
type Audit struct {
	Action string `json:"action"`
	User   string `json:"user,omitempty"`
	Client string `json:"client,omitempty"`
	Target string `json:"target,omitempty"`
	Result string `json:"result,omitempty"`
}

// QueryEntry is one journal entry of a query.
type QueryEntry struct {
	Time     time.Time `json:"time"`
	Priority int       `json:"priority"`
	Ident    string    `json:"ident"`
	Unit     string    `json:"unit,omitempty"`
	PID      int       `json:"pid,omitempty"`
	Message  string    `json:"message"`
	Audit    *Audit    `json:"audit,omitempty"`
}

// ParseQueryJournal parses `journalctl -o json --reverse` output (newest
// first) and keeps at most limit entries; more reports that there were
// further ones.
func ParseQueryJournal(raw []byte, limit int) (entries []QueryEntry, more bool) {
	entries = []QueryEntry{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if len(entries) == limit {
			return entries, true
		}
		e := QueryEntry{Priority: 6, Message: field(m["MESSAGE"]), Ident: field(m["SYSLOG_IDENTIFIER"]),
			Unit: field(m["_SYSTEMD_UNIT"])}
		if us, err := strconv.ParseInt(field(m["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
			e.Time = time.UnixMicro(us).UTC()
		}
		if p, err := strconv.Atoi(field(m["PRIORITY"])); err == nil && p >= 0 && p <= 7 {
			e.Priority = p
		}
		e.PID, _ = strconv.Atoi(field(m["_PID"]))
		e.Message = truncate(strings.ToValidUTF8(e.Message, "�"), MaxMessage)
		if field(m["NAS_REVIVAL_AUDIT"]) == "1" {
			clean := func(k string) string { return truncate(strings.ToValidUTF8(field(m[k]), "�"), 200) }
			e.Audit = &Audit{Action: clean("AUDIT_ACTION"), User: clean("AUDIT_USER"), Client: clean("AUDIT_CLIENT"),
				Target: clean("AUDIT_TARGET"), Result: clean("AUDIT_RESULT")}
		}
		entries = append(entries, e)
	}
	return entries, false
}
