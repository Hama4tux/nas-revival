// Command e2e-network is the W2.8 end-to-end check on the NAS (scripts/webui/e2e-network.sh runs it as
// nas-revival). It reads the network overview the way the web process does, sets the current host name
// again through the helper (no change) and runs every diagnostic once. It never renames the NAS; the only
// packets it sends are pings of the gateway and of a TEST-NET address, a DNS lookup and one HTTPS connect.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/netinfo"
)

var fails int

func check(ok bool, format string, a ...any) {
	status := "ok  "
	if !ok {
		status, fails = "FAIL", fails+1
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, a...))
}

func code(err error) string {
	if he, ok := helper.AsError(err); ok {
		return he.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func main() {
	ctx := helper.WithUser(context.Background(), "w28-e2e")
	info, err := netinfo.Reader{Root: "/", Run: netinfo.Runner(helper.ExecRunner), Now: time.Now}.Read(ctx)
	check(err == nil && info.Hostname != "", "overview as nas-revival: hostname %q, err %v", info.Hostname, err)
	up := slices.IndexFunc(info.Interfaces, func(i netinfo.Interface) bool { return i.State == "up" && i.SpeedMbps != nil })
	check(up >= 0, "an interface is up with a link speed: %+v", info.Interfaces)
	check(slices.ContainsFunc(info.Routes, func(r netinfo.Route) bool { return r.Destination == "default" && r.Gateway != "" }),
		"default route present (%d routes)", len(info.Routes))

	hc := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock"}
	st, err := hc.HostnameSet(ctx, info.Hostname)
	check(err == nil && st.Hostname == info.Hostname, "net.hostname with the current name: %+v %v", st, err)
	_, err = hc.HostnameSet(ctx, "-bad")
	check(code(err) == helper.CodeInvalid, "net.hostname rejects an invalid name: %v", err)
	if h, _ := os.Hostname(); h != info.Hostname {
		check(false, "host name changed to %q", h)
	}

	r, err := hc.NetDiag(ctx, "gateway", "")
	check(err == nil && r.OK && r.Ping != nil && r.Ping.Received > 0, "gateway answers: %+v %v", r.Ping, err)
	r, err = hc.NetDiag(ctx, "ping", "192.0.2.1")
	check(err == nil && !r.OK && r.Ping != nil && r.Ping.Received == 0 && r.Message != "", "TEST-NET address does not answer: %+v %v", r, err)
	r, err = hc.NetDiag(ctx, "dns", helper.InternetHost)
	check(err == nil && r.OK && len(r.Addresses) > 0, "dns %s → %v %v", helper.InternetHost, r.Addresses, err)
	r, err = hc.NetDiag(ctx, "dns", "nas-revival-e2e.invalid")
	check(err == nil && !r.OK, "dns of an .invalid name fails: %+v %v", r, err)
	r, err = hc.NetDiag(ctx, "internet", "")
	check(err == nil && r.OK && len(r.Steps) == 2, "internet: %+v %v", r.Steps, err)
	r, err = hc.NetDiag(ctx, "ports", "")
	has := func(port int, proc string) bool {
		return slices.ContainsFunc(r.Ports, func(p helper.ListenPort) bool { return p.Port == port && p.Process == proc })
	}
	check(err == nil && has(22, "sshd") && has(8443, "nas-revival"), "listening ports with processes (root): %d ports %v", len(r.Ports), err)
	r, err = hc.NetDiag(ctx, "connections", "")
	check(err == nil && slices.ContainsFunc(r.Connections, func(c helper.Connection) bool { return c.LocalPort == 22 }),
		"connections include this SSH session: %d %v", len(r.Connections), err)
	_, err = hc.NetDiag(ctx, "ping", "255.255.255.255")
	check(code(err) == helper.CodeInvalid, "broadcast ping rejected: %v", err)

	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}
