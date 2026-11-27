// Command helper-call sends one request to the root helper and prints the JSON
// result. For the owner's maintenance over SSH as root on the NAS, when the web
// UI is not at hand; the helper validates every op exactly as for the web.
//
//	helper-call [-user NAME] OP ['{"json":"args"}']
//	helper-call task.get
//	helper-call -user hama task.set "$(cat tasks.json)"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"nas-revival/backend/internal/helper"
)

func main() {
	user := flag.String("user", "helper-call", "user name recorded in the audit log")
	socket := flag.String("socket", "/run/nas-revival-helper/helper.sock", "helper socket")
	timeout := flag.Duration("timeout", 120*time.Second, "request timeout")
	flag.Parse()
	if flag.NArg() < 1 || flag.NArg() > 2 {
		fmt.Fprintln(os.Stderr, "usage: helper-call [-user NAME] OP [JSON-ARGS]")
		os.Exit(2)
	}
	var args any
	if flag.NArg() == 2 {
		raw := json.RawMessage(flag.Arg(1))
		if !json.Valid(raw) {
			fmt.Fprintln(os.Stderr, "helper-call: JSON-ARGS is not valid JSON")
			os.Exit(2)
		}
		args = raw
	}
	c := &helper.Client{Socket: *socket, Timeout: *timeout}
	var res json.RawMessage
	if err := c.Call(helper.WithUser(context.Background(), *user), flag.Arg(0), args, &res); err != nil {
		fmt.Fprintln(os.Stderr, "helper-call:", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}
