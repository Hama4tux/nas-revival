// Command e2e-firewall drives the firewall operations of the helper (W5.2, ADR 0014) for
// scripts/webui/e2e-firewall.sh, which runs it on the NAS as nas-revival and checks the effect from the
// workstation. Usage: e2e-firewall get [ports] | lockout <client> <lan> | enable <client> <lan> | rule|dropclient|lockoutrule <client> <lan> | disable <client> |
// confirm. It prints `supported=… active=… enabled=… pending=…` (and the error for refused operations)
// and exits 1 when the helper refuses an operation the caller expected to work.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

type state struct {
	Supported bool `json:"supported"`
	Active    bool `json:"active"`
	Settings  struct {
		Enabled bool                `json:"enabled"`
		Allow   map[string][]string `json:"allow"`
	} `json:"settings"`
	Listening []struct {
		Proto   string `json:"proto"`
		Port    int    `json:"port"`
		Allowed bool   `json:"allowed"`
	} `json:"listening"`
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

func rule(source, ports, action string) map[string]any {
	return map[string]any{"source": source, "ports": ports, "proto": "tcp", "action": action, "note": ""}
}

func withRules(s map[string]any, r ...map[string]any) map[string]any {
	s["rules"] = r
	return s
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
	case "save": // print the stored settings, so the script can put them back at the end
		res, err = call("firewall.get", struct{}{})
		if err == nil {
			var st struct {
				Settings json.RawMessage `json:"settings"`
			}
			if err = json.Unmarshal(res, &st); err == nil {
				fmt.Println(string(st.Settings))
				return
			}
		}
	case "restore": // restore <client> <settings JSON from save>: apply and confirm
		res, err = call("firewall.apply", map[string]any{"settings": json.RawMessage(arg(3)), "clientAddr": arg(2)})
		if err == nil {
			res, err = call("firewall.confirm", struct{}{})
		}
	case "lockout": // web allows only the LAN, ssh nothing: must be refused
		res, err = call("firewall.apply", map[string]any{"settings": settings(true, []string{arg(3)}, []string{}), "clientAddr": arg(2)})
		if err != nil {
			if !strings.Contains(err.Error(), "odpojilo") {
				fmt.Println("error:", err)
				os.Exit(1)
			}
			fmt.Println("refused:", err)
			return
		}
		fmt.Println("NOT refused")
		os.Exit(1)
	case "enable": // web and ssh from the LAN only, SMB blocked, NFS open
		res, err = call("firewall.apply", map[string]any{"settings": settings(true, []string{arg(3)}, []string{arg(3)}), "clientAddr": arg(2)})
	case "rule": // like enable, SMB list empty, but a rule opens SMB for the LAN
		res, err = call("firewall.apply", map[string]any{"settings": withRules(settings(true, []string{arg(3)}, []string{arg(3)}),
			rule(arg(3), "445", "accept")), "clientAddr": arg(2)})
	case "dropclient": // SMB open for the LAN, but a rule drops it for the workstation
		set := withRules(settings(true, []string{arg(3)}, []string{arg(3)}), rule(arg(2), "445", "drop"))
		set["allow"].(map[string][]string)["smb"] = []string{arg(3)}
		res, err = call("firewall.apply", map[string]any{"settings": set, "clientAddr": arg(2)})
	case "lockoutrule": // a rule drops the web UI for the client: must be refused
		res, err = call("firewall.apply", map[string]any{"settings": withRules(settings(true, []string{arg(3)}, []string{arg(3)}),
			rule(arg(2), "8443", "drop")), "clientAddr": arg(2)})
		if err != nil {
			if !strings.Contains(err.Error(), "odpojilo") {
				fmt.Println("error:", err)
				os.Exit(1)
			}
			fmt.Println("refused:", err)
			return
		}
		fmt.Println("NOT refused")
		os.Exit(1)
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
	if os.Args[1] == "get" && len(os.Args) > 2 && os.Args[2] == "ports" {
		var blocked []string
		for _, l := range st.Listening {
			if !l.Allowed {
				blocked = append(blocked, fmt.Sprintf("%s/%d", l.Proto, l.Port))
			}
		}
		fmt.Println("blocked=" + strings.Join(blocked, ","))
	}
}
