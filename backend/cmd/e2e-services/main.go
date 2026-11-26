// Command e2e-services is the W2.4 end-to-end check on the NAS (scripts/webui/e2e-services.sh runs it as
// nas-revival). Through the helper socket it lists the services twice (state, PID, memory, ports, CPU %),
// checks that SSH cannot be stopped, reads the SSH journal and restarts cron (harmless) and checks it runs again.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

type service struct {
	Name        string   `json:"name"`
	State       string   `json:"state"`
	PID         int      `json:"pid"`
	MemoryBytes *uint64  `json:"memoryBytes"`
	CPUPercent  *float64 `json:"cpuPercent"`
	Ports       []struct {
		Proto string `json:"proto"`
		Port  int    `json:"port"`
	} `json:"ports"`
	Actions []string `json:"actions"`
}

type helperErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var fails int

func check(ok bool, format string, a ...any) {
	status := "ok  "
	if !ok {
		status, fails = "FAIL", fails+1
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, a...))
}

func call(op string, args any, out any) *helperErr {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(60 * time.Second))
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w24-e2e"})
	c.Write(append(b, '\n'))
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *helperErr      `json:"error"`
	}
	if err := json.NewDecoder(c).Decode(&r); err != nil {
		panic(err)
	}
	if !r.OK {
		return r.Error
	}
	if out != nil {
		json.Unmarshal(r.Result, out)
	}
	return nil
}

func list() map[string]service {
	var l struct{ Services []service }
	if e := call("service.list", nil, &l); e != nil {
		panic(e.Message)
	}
	m := map[string]service{}
	for _, s := range l.Services {
		m[s.Name] = s
	}
	return m
}

func main() {
	t0 := time.Now()
	l := list()
	fmt.Printf("     service.list took %v\n", time.Since(t0).Round(time.Millisecond))
	ssh := l["ssh.service"]
	check(ssh.State == "active" && ssh.PID > 0, "ssh active with PID (%s, %d)", ssh.State, ssh.PID)
	check(ssh.MemoryBytes != nil && *ssh.MemoryBytes > 0, "ssh memory reported")
	has22 := false
	for _, p := range ssh.Ports {
		has22 = has22 || (p.Proto == "tcp" && p.Port == 22)
	}
	check(has22, "ssh listens on tcp 22 (%v)", ssh.Ports)
	web := l["nas-revival.service"]
	check(len(web.Actions) == 0, "web UI offers no actions (%v)", web.Actions)
	check(l["smbd.service"].State == "not-installed", "samba reported as not installed (%s)", l["smbd.service"].State)

	time.Sleep(1500 * time.Millisecond)
	l = list()
	check(l["ssh.service"].CPUPercent != nil, "ssh CPU %% on second list")

	e := call("service.action", map[string]string{"name": "ssh.service", "action": "stop"}, nil)
	check(e != nil && e.Code == "invalid", "ssh stop refused (%v)", e)
	e = call("service.action", map[string]string{"name": "getty@tty1.service", "action": "stop"}, nil)
	check(e != nil && e.Code == "not_found", "unit outside the catalog refused (%v)", e)

	var lg struct {
		Entries []struct{ Message string }
	}
	e = call("journal.unit", map[string]any{"name": "ssh.service", "lines": 20}, &lg)
	check(e == nil && len(lg.Entries) > 0 && len(lg.Entries) <= 20, "ssh journal: %d entries (%v)", len(lg.Entries), e)

	var cron service
	e = call("service.action", map[string]string{"name": "cron.service", "action": "restart"}, &cron)
	check(e == nil && cron.State == "active" && cron.PID > 0, "cron restart: %s pid %d (%v)", cron.State, cron.PID, e)

	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}
