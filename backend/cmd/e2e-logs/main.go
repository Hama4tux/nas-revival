// Command e2e-logs drives the Log Center operations of the helper (W5.3, ADR 0015) for
// scripts/webui/e2e-logs.sh, which runs it on the NAS as nas-revival. Usage:
//
//	e2e-logs query <category> [text] – prints `n=<entries> more=<bool> audit=<entries with audit fields>`
//	e2e-logs bad                     – sends invalid queries, prints one line per refused one
//	e2e-logs forge <marker>          – writes an audit entry from this process (not from the service unit)
//	e2e-logs retention [maxUse maxRetention] – prints `maxUse=… maxRetention=… usage=<bool>`, sets first when given
//
// It exits 1 when the helper refuses an operation the caller expected to work.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"nas-revival/backend/internal/audit"
)

func call(op string, args any) (json.RawMessage, error) {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		return nil, err
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w53-e2e"})
	if _, err := c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.NewDecoder(c).Decode(&r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("%v", r.Error)
	}
	return r.Result, nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func query(cat, text string) {
	raw, err := call("journal.query", map[string]any{"category": cat, "text": text, "limit": 1000})
	if err != nil {
		die(err)
	}
	var r struct {
		Entries []struct {
			Audit *struct{ Action string } `json:"audit"`
		} `json:"entries"`
		More bool `json:"more"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		die(err)
	}
	n := 0
	for _, e := range r.Entries {
		if e.Audit != nil {
			n++
		}
	}
	fmt.Printf("n=%d more=%v audit=%d\n", len(r.Entries), r.More, n)
}

func retention(set []string) {
	if len(set) == 2 {
		if _, err := call("journal.retention.set", map[string]any{"maxUse": set[0], "maxRetention": set[1]}); err != nil {
			die(err)
		}
	}
	raw, err := call("journal.retention.get", nil)
	if err != nil {
		die(err)
	}
	var r struct {
		MaxUse       string  `json:"maxUse"`
		MaxRetention string  `json:"maxRetention"`
		UsageBytes   *uint64 `json:"usageBytes"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		die(err)
	}
	fmt.Printf("maxUse=%s maxRetention=%s usage=%v\n", r.MaxUse, r.MaxRetention, r.UsageBytes != nil && *r.UsageBytes > 0)
}

func main() {
	if len(os.Args) < 2 {
		die(fmt.Errorf("usage: e2e-logs query|bad|forge|retention …"))
	}
	switch arg := os.Args[2:]; os.Args[1] {
	case "query":
		text := ""
		if len(arg) > 1 {
			text = arg[1]
		}
		query(arg[0], text)
	case "bad":
		for _, q := range []map[string]any{
			{"category": "nope"},
			{"category": "kernel", "unit": "ssh.service"},
			{"unit": "evil.service"},
			{"priority": 9},
			{"since": "yesterday"},
			{"limit": 5000},
			{"text": "a\nb"},
		} {
			if _, err := call("journal.query", q); err != nil {
				fmt.Println("refused")
			} else {
				fmt.Println("accepted")
			}
		}
		if _, err := call("journal.retention.set", map[string]any{"maxUse": "10G", "maxRetention": "none"}); err != nil {
			fmt.Println("refused")
		} else {
			fmt.Println("accepted")
		}
	case "forge":
		audit.Log(audit.Entry{Action: "user.create", User: "forged", Target: arg[0], Result: audit.OK, Message: "forged " + arg[0]})
	case "retention":
		retention(arg)
	default:
		die(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}
