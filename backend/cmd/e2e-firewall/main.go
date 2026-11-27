// Command e2e-firewall drives the firewall operations of the helper (W5.2, ADR 0014) for
// scripts/webui/e2e-firewall.sh, which runs it on the NAS as nas-revival and checks the effect from the
// workstation. Usage: e2e-firewall get | lockout <client> <lan> | enable <client> <lan> | disable <client> |
// confirm. It prints `supported=… active=… enabled=… pending=…` (and the error for refused operations)
// and exits 1 when the helper refuses an operation the caller expected to work.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

type state struct {
	Supported bool `json:"supported"`
	Active    bool `json:"active"`
	Settings  struct {
		Enabled bool                `json:"enabled"`
		Allow   map[string][]string `json:"allow"`
	} `json:"settings"`
	Pending *struct {
		Deadline string `json:"deadline"`
	} `json:"pending"`
}

func call(op string, args any) (json.RawMessage, error) {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		return nil, err
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w52-e2e"})
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

func settings(enabled bool, web, ssh []string) map[string]any {
	return map[string]any{"enabled": enabled, "allow": map[string][]string{
		// NFS stays open to everyone so Proxmox backups are not disturbed; SMB is the blocked probe.
		"web": web, "ssh": ssh, "smb": {}, "nfs": {"0.0.0.0/0"}}}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: e2e-firewall get|lockout|enable|disable|confirm …")
		os.Exit(2)
	}
	arg := func(i int) string {
		if len(os.Args) <= i {
			fmt.Fprintln(os.Stderr, "missing argument")
			os.Exit(2)
		}
		return os.Args[i]
	}
	var res json.RawMessage
	var err error
	switch os.Args[1] {
	case "get":
		res, err = call("firewall.get", struct{}{})
	case "lockout": // web allows only the LAN, ssh nothing: must be refused
		res, err = call("firewall.apply", map[string]any{"settings": settings(true, []string{arg(3)}, []string{}), "clientAddr": arg(2)})
		if err != nil {
			fmt.Println("refused:", err)
			return
		}
		fmt.Println("NOT refused")
		os.Exit(1)
	case "enable": // web and ssh from the LAN only, SMB blocked, NFS open
		res, err = call("firewall.apply", map[string]any{"settings": settings(true, []string{arg(3)}, []string{arg(3)}), "clientAddr": arg(2)})
	case "disable":
		res, err = call("firewall.apply", map[string]any{"settings": settings(false, []string{}, []string{}), "clientAddr": arg(2)})
	case "confirm":
		res, err = call("firewall.confirm", struct{}{})
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	var st state
	if err := json.Unmarshal(res, &st); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Printf("supported=%t active=%t enabled=%t pending=%t\n", st.Supported, st.Active, st.Settings.Enabled, st.Pending != nil)
}
