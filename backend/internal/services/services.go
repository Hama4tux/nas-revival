// Package services describes the allowlisted systemd units of the Services
// app (SVC-10..12) and parses what systemctl, ss, /proc and journalctl report
// about them. Unit names never come from a request: every name is looked up
// in Catalog first.
package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Actions a unit may allow.
const (
	ActStart   = "start"
	ActStop    = "stop"
	ActRestart = "restart"
	ActReload  = "reload"
	ActEnable  = "enable"
	ActDisable = "disable"
)

var allActions = []string{ActStart, ActStop, ActRestart, ActReload, ActEnable, ActDisable}

// Def is one allowlisted unit. Protected units keep running: they cannot be
// stopped or disabled from the web UI because that would cut off the NAS
// (SSH, network, the web UI and its helper) or overheat the disk (fan).
type Def struct {
	Name      string
	Label     string
	Protected bool
	Actions   []string
}

// Catalog is the fixed list of units shown in the Services app, in display order.
var Catalog = []Def{
	{Name: "smbd.service", Label: "Samba (SMB)", Actions: allActions},
	{Name: "nfs-server.service", Label: "NFS", Actions: allActions},
	{Name: "ssh.service", Label: "SSH", Protected: true, Actions: []string{ActRestart, ActReload}},
	{Name: "smartmontools.service", Label: "SMART dohled", Actions: allActions},
	{Name: "cron.service", Label: "Plánovač (cron)", Actions: allActions},
	{Name: "systemd-timesyncd.service", Label: "Čas (NTP)", Actions: allActions},
	{Name: "systemd-resolved.service", Label: "DNS", Actions: []string{ActRestart}},
	{Name: "systemd-networkd.service", Label: "Síť", Protected: true},
	{Name: "nsa310-fan.service", Label: "Ventilátor", Protected: true, Actions: []string{ActRestart}},
	{Name: "nsa310-leds.service", Label: "LED", Actions: allActions},
	{Name: "nsa310-button.service", Label: "Tlačítko COPY", Actions: allActions},
	{Name: "nas-revival.service", Label: "Webová správa", Protected: true},
	{Name: "nas-revival-helper.service", Label: "Pomocná služba správy", Protected: true},
}

// Lookup returns the catalog entry of name.
func Lookup(name string) (Def, bool) {
	for _, d := range Catalog {
		if d.Name == name {
			return d, true
		}
	}
	return Def{}, false
}

// Allows reports whether the catalog permits action on the unit at all.
func (d Def) Allows(action string) bool {
	for _, a := range d.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// ShowProperties are the systemctl show properties the app needs.
const ShowProperties = "Id,Description,LoadState,ActiveState,SubState,UnitFileState,Type,Result," +
	"ExecMainStatus,MainPID,CPUUsageNSec,ControlGroup,CanReload,StateChangeTimestamp"

// Port is one listening socket of a unit.
type Port struct {
	Proto   string `json:"proto"` // tcp, udp
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// Unit is the parsed systemctl show block of one unit.
type Unit struct {
	ID, Description         string
	LoadState, ActiveState  string
	SubState, UnitFileState string
	Type, Result            string
	ExecMainStatus          int
	MainPID                 int
	CPUNSec                 *uint64
	ControlGroup            string
	CanReload               bool
	Since                   *time.Time
}

// Installed is false for units systemd does not know.
func (u Unit) Installed() bool { return u.LoadState != "" && u.LoadState != "not-found" }

// State is the display state: active, inactive, failed, activating,
// deactivating, done (oneshot finished cleanly), not-installed or unknown.
func (u Unit) State() string {
	if !u.Installed() {
		return "not-installed"
	}
	switch u.ActiveState {
	case "active", "reloading":
		return "active"
	case "inactive":
		if u.Type == "oneshot" && u.Result == "success" {
			return "done"
		}
		return "inactive"
	case "failed", "activating", "deactivating":
		return u.ActiveState
	}
	return "unknown"
}

// Available lists the actions that make sense now: allowed by the catalog
// and by the current state.
func (u Unit) Available(d Def) []string {
	out := []string{}
	if !u.Installed() || u.LoadState == "masked" || u.UnitFileState == "masked" {
		return out
	}
	running := u.ActiveState == "active" || u.ActiveState == "reloading" || u.ActiveState == "activating"
	for _, a := range d.Actions {
		ok := false
		switch a {
		case ActStart:
			ok = !running
		case ActStop:
			ok = running
		case ActRestart:
			ok = true
		case ActReload:
			ok = running && u.CanReload
		case ActEnable:
			ok = u.UnitFileState == "disabled"
		case ActDisable:
			ok = u.UnitFileState == "enabled"
		}
		if ok {
			out = append(out, a)
		}
	}
	return out
}

// ParseShow splits systemctl show output into units (blocks separated by
// blank lines).
func ParseShow(raw []byte) []Unit {
	var out []Unit
	cur := map[string]string{}
	flush := func() {
		if len(cur) > 0 {
			out = append(out, unitFrom(cur))
			cur = map[string]string{}
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur[k] = v
		}
	}
	flush()
	return out
}

func unitFrom(m map[string]string) Unit {
	u := Unit{ID: m["Id"], Description: m["Description"], LoadState: m["LoadState"], ActiveState: m["ActiveState"],
		SubState: m["SubState"], UnitFileState: m["UnitFileState"], Type: m["Type"], Result: m["Result"],
		ControlGroup: m["ControlGroup"], CanReload: m["CanReload"] == "yes"}
	u.ExecMainStatus, _ = strconv.Atoi(m["ExecMainStatus"])
	u.MainPID, _ = strconv.Atoi(m["MainPID"])
	if n, err := strconv.ParseUint(m["CPUUsageNSec"], 10, 64); err == nil && n != ^uint64(0) {
		u.CPUNSec = &n
	}
	if v := m["StateChangeTimestamp"]; strings.HasPrefix(v, "@") {
		if sec, err := strconv.ParseInt(v[1:], 10, 64); err == nil && sec > 0 {
			t := time.Unix(sec, 0).UTC()
			u.Since = &t
		}
	}
	return u
}

// Listener is one line of `ss -H -l -t -u -n -p`.
type Listener struct {
	Port
	PIDs []int
}

// ParseSS parses `ss -H -l -t -u -n -p` output. Lines it does not
// understand are skipped.
func ParseSS(raw []byte) []Listener {
	var out []Listener
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || (f[0] != "tcp" && f[0] != "udp") {
			continue
		}
		addr, port, ok := splitHostPort(f[4])
		if !ok {
			continue
		}
		l := Listener{Port: Port{Proto: f[0], Address: addr, Port: port}}
		for _, x := range f[6:] {
			l.PIDs = append(l.PIDs, parsePIDs(x)...)
		}
		out = append(out, l)
	}
	return out
}

// splitHostPort handles "0.0.0.0:22", "[::]:22", "*:22" and "127.0.0.53%lo:53".
func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 {
		return "", 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 65535 {
		return "", 0, false
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]")
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	return host, p, true
}

// parsePIDs extracts every pid=N from a users:((...)) column.
func parsePIDs(s string) []int {
	var out []int
	for {
		i := strings.Index(s, "pid=")
		if i < 0 {
			return out
		}
		s = s[i+4:]
		j := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if n, err := strconv.Atoi(s[:j]); err == nil {
			out = append(out, n)
		}
		s = s[j:]
	}
}

// LogEntry is one journal line.
type LogEntry struct {
	Time     time.Time `json:"time"`
	Priority int       `json:"priority"` // 0 emerg … 7 debug
	Ident    string    `json:"ident"`
	PID      int       `json:"pid,omitempty"`
	Message  string    `json:"message"`
}

// MaxLogLines bounds a log request (SVC-12); MaxMessage bounds one message.
const (
	MaxLogLines = 200
	MaxMessage  = 1000
)

// ParseJournal parses `journalctl -o json` output (one object per line).
func ParseJournal(raw []byte) []LogEntry {
	out := []LogEntry{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		e := LogEntry{Priority: 6, Message: field(m["MESSAGE"]), Ident: field(m["SYSLOG_IDENTIFIER"])}
		if us, err := strconv.ParseInt(field(m["__REALTIME_TIMESTAMP"]), 10, 64); err == nil {
			e.Time = time.UnixMicro(us).UTC()
		}
		if p, err := strconv.Atoi(field(m["PRIORITY"])); err == nil && p >= 0 && p <= 7 {
			e.Priority = p
		}
		e.PID, _ = strconv.Atoi(field(m["_PID"]))
		e.Message = truncate(strings.ToValidUTF8(e.Message, "�"), MaxMessage)
		out = append(out, e)
	}
	if len(out) > MaxLogLines {
		out = out[len(out)-MaxLogLines:]
	}
	return out
}

// field decodes a journal JSON field: a string, or an array of bytes for
// binary messages.
func field(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	var nums []int
	if json.Unmarshal(raw, &nums) == nil {
		for _, n := range nums {
			b = append(b, byte(n))
		}
		return string(b)
	}
	return ""
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
