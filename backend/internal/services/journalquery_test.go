package services

import (
	"reflect"
	"strings"
	"testing"
)

var units = []string{"smbd.service", "ssh.service", WebUnit}

func ip(n int) *int { return &n }

func TestBuildJournalArgs(t *testing.T) {
	base := []string{"--output=json", "--no-pager", "--quiet", "--reverse", "--lines=201", "--output-fields=" + QueryFields}
	for name, tc := range map[string]struct {
		q    Query
		tail []string
	}{
		"default":  {Query{}, nil},
		"kernel":   {Query{Category: "kernel"}, []string{"--dmesg"}},
		"auth":     {Query{Category: "auth"}, []string{"SYSLOG_FACILITY=4", "SYSLOG_FACILITY=10"}},
		"nas":      {Query{Category: "nas"}, []string{"_SYSTEMD_UNIT=nas-revival.service", "_SYSTEMD_UNIT=nas-revival-helper.service"}},
		"services": {Query{Category: "services"}, []string{"--unit=smbd.service", "--unit=ssh.service", "--unit=nas-revival.service"}},
		"audit": {Query{Category: "audit"}, []string{"NAS_REVIVAL_AUDIT=1", "_SYSTEMD_UNIT=nas-revival.service", "+",
			"NAS_REVIVAL_AUDIT=1", "_SYSTEMD_UNIT=nas-revival-helper.service"}},
		"unit":     {Query{Unit: "ssh.service"}, []string{"--unit=ssh.service"}},
		"priority": {Query{Priority: ip(3)}, []string{"--priority=3"}},
		"times": {Query{Since: "2026-11-27T12:00:00Z", Until: "2026-11-27T13:00:00.123456789+01:00"},
			[]string{"--since=@1795780800", "--until=@1795780800.123456"}},
		"text": {Query{Text: "a.b*"}, []string{"--grep=a\\.b\\*", "--case-sensitive=false"}},
	} {
		got, err := BuildJournalArgs(tc.q, units)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		want := append(append([]string{}, base...), tc.tail...)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestBuildJournalArgsLimit(t *testing.T) {
	got, err := BuildJournalArgs(Query{Limit: 5}, units)
	if err != nil || !contains(got, "--lines=6") {
		t.Errorf("%q %v", got, err)
	}
	for _, l := range []int{-1, 1001} {
		if _, err := BuildJournalArgs(Query{Limit: l}, units); err == nil {
			t.Errorf("limit %d accepted", l)
		}
	}
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuildJournalArgsRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		q     Query
		field string
	}{
		"category":      {Query{Category: "x"}, "category"},
		"unit unknown":  {Query{Unit: "sshd.service"}, "unit"},
		"unit injected": {Query{Unit: "ssh.service\n--root=/"}, "unit"},
		"unit+category": {Query{Category: "auth", Unit: "ssh.service"}, "unit"},
		"priority":      {Query{Priority: ip(8)}, "priority"},
		"priority neg":  {Query{Priority: ip(-1)}, "priority"},
		"since":         {Query{Since: "yesterday"}, "since"},
		"until":         {Query{Until: "1700000000"}, "until"},
		"order":         {Query{Since: "2026-11-27T13:00:00Z", Until: "2026-11-27T12:00:00Z"}, "until"},
		"text long":     {Query{Text: strings.Repeat("a", 101)}, "text"},
		"text control":  {Query{Text: "a\nb"}, "text"},
	} {
		_, err := BuildJournalArgs(tc.q, units)
		qe, ok := err.(*QueryError)
		if !ok || qe.Field != tc.field {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := BuildJournalArgs(Query{Text: strings.Repeat("é", 100)}, units); err != nil {
		t.Errorf("100 runes: %v", err)
	}
}

func TestEscapeGrep(t *testing.T) {
	if got := EscapeGrep(`a b|c(d)[e]\ .*+?^$é`); got != `a\ b\|c\(d\)\[e\]\\\ \.\*\+\?\^\$\é` {
		t.Errorf("got %s", got)
	}
}

func TestParseQueryJournal(t *testing.T) {
	raw := `{"__REALTIME_TIMESTAMP":"1795780800000000","PRIORITY":"4","MESSAGE":"newest","SYSLOG_IDENTIFIER":"sshd","_PID":"12","_SYSTEMD_UNIT":"ssh.service"}
{"__REALTIME_TIMESTAMP":"1795780700000000","MESSAGE":"Účet bob vytvořen","SYSLOG_IDENTIFIER":"nas-revival","_SYSTEMD_UNIT":"nas-revival.service","NAS_REVIVAL_AUDIT":"1","AUDIT_ACTION":"user.create","AUDIT_USER":"alice","AUDIT_CLIENT":"10.0.0.5","AUDIT_TARGET":"bob","AUDIT_RESULT":"ok"}
not json
{"__REALTIME_TIMESTAMP":"1795780600000000","MESSAGE":[104,105],"PRIORITY":"9"}
{"__REALTIME_TIMESTAMP":"1795780500000000","MESSAGE":"older"}
`
	es, more := ParseQueryJournal([]byte(raw), 3)
	if !more || len(es) != 3 {
		t.Fatalf("more=%v len=%d", more, len(es))
	}
	if es[0].Message != "newest" || es[0].Priority != 4 || es[0].Unit != "ssh.service" || es[0].PID != 12 || es[0].Audit != nil {
		t.Errorf("e0 = %+v", es[0])
	}
	if es[0].Time.Unix() != 1795780800 {
		t.Errorf("time %v", es[0].Time)
	}
	a := es[1].Audit
	if a == nil || *a != (Audit{"user.create", "alice", "10.0.0.5", "bob", "ok"}) || es[1].Priority != 6 {
		t.Errorf("e1 = %+v %+v", es[1], a)
	}
	if es[2].Message != "hi" || es[2].Priority != 6 {
		t.Errorf("e2 = %+v", es[2])
	}
	es, more = ParseQueryJournal([]byte(raw), 10)
	if more || len(es) != 4 {
		t.Errorf("more=%v len=%d", more, len(es))
	}
}
