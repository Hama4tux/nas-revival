// Command e2e-updates drives the Debian update operations of the helper (W5.4, ADR 0016)
// for scripts/webui/e2e-updates.sh, which runs it on the NAS as nas-revival. Usage:
//
//	e2e-updates get              – prints `running=<job> checked=<bool> error=<bool> n=<updates> security=<n> listed=<RFC 3339>`
//	e2e-updates check            – starts a check; prints `started` or the helper's error code
//	e2e-updates upgrade <listed> – prints the helper's error code (never used with the current list)
//
// It exits 1 when the helper cannot be reached.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

type helperErr struct {
	Code string `json:"code"`
}

func call(op string, args any) (json.RawMessage, *helperErr) {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		die(err)
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w54-e2e"})
	if _, err := c.Write(append(b, '\n')); err != nil {
		die(err)
	}
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *helperErr      `json:"error"`
	}
	if err := json.NewDecoder(c).Decode(&r); err != nil {
		die(err)
	}
	if !r.OK {
		return nil, r.Error
	}
	return r.Result, nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func outcome(he *helperErr) {
	if he == nil {
		fmt.Println("started")
		return
	}
	fmt.Println(he.Code)
}

func main() {
	if len(os.Args) < 2 {
		die(fmt.Errorf("usage: e2e-updates get|check|upgrade <listed>"))
	}
	switch os.Args[1] {
	case "get":
		raw, he := call("updates.get", nil)
		if he != nil {
			die(fmt.Errorf("updates.get: %s", he.Code))
		}
		var s struct {
			Running    string  `json:"running"`
			CheckedAt  *string `json:"checkedAt"`
			CheckError string  `json:"checkError"`
			ListedAt   string  `json:"listedAt"`
			Packages   []struct {
				Security bool `json:"security"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			die(err)
		}
		sec := 0
		for _, p := range s.Packages {
			if p.Security {
				sec++
			}
		}
		fmt.Printf("running=%s checked=%t error=%t n=%d security=%d listed=%s\n", s.Running, s.CheckedAt != nil,
			s.CheckError != "", len(s.Packages), sec, s.ListedAt)
	case "check":
		_, he := call("updates.check", nil)
		outcome(he)
	case "upgrade":
		if len(os.Args) != 3 {
			die(fmt.Errorf("usage: e2e-updates upgrade <listed>"))
		}
		_, he := call("updates.upgrade", map[string]string{"listedAt": os.Args[2]})
		outcome(he)
	default:
		die(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}
