// Command e2e-processes is the W3.2 end-to-end check on the NAS (scripts/webui/e2e-processes.sh runs it
// as nas-revival). It reads the process list the way the web process does, asks the helper to stop
// protected processes (PID 1, a kernel thread, the helper, sshd) and a PID with a wrong start time (all
// must be refused without a signal), then starts its own `sleep` and stops it through the helper.
// The only process it ends is that sleep.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/procs"
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
	ctx := helper.WithUser(context.Background(), "w32-e2e")
	r := &procs.Reader{Root: "/"}
	t0 := time.Now()
	l, err := r.List(ctx)
	cold := time.Since(t0)
	check(err == nil && len(l.Processes) > 10, "list: %d processes, err %v (cold start %s incl. 1 s baseline)", len(l.Processes), err, cold.Round(time.Millisecond))
	r.MinInterval = time.Nanosecond
	t1 := time.Now()
	l, err = r.List(ctx)
	check(err == nil, "warm list err %v, one reading took %s", err, time.Since(t1).Round(time.Millisecond))
	byName := map[string]procs.Process{}
	var kthread, self *procs.Process
	var cpuSum float64
	for i, p := range l.Processes {
		if _, ok := byName[p.Name]; !ok {
			byName[p.Name] = p
		}
		if p.Protected == procs.ReasonKernel && kthread == nil && p.PID != 2 {
			kthread = &l.Processes[i]
		}
		if p.PID == os.Getpid() {
			self = &l.Processes[i]
		}
		cpuSum += p.CPU
	}
	check(l.MemTotal > 0, "memTotal %d", l.MemTotal)
	check(cpuSum >= 0 && cpuSum <= 101, "CPU sum %.1f %%", cpuSum)
	check(self != nil && self.User == "nas-revival" && self.RSS > 0, "own process listed with user and RSS: %+v", self)
	var init procs.Process
	for _, p := range l.Processes {
		if p.PID == 1 {
			init = p
		}
	}
	check(init.PID == 1 && init.Protected == procs.ReasonInit, "PID 1 protected: %+v", init)
	check(kthread != nil && kthread.Cmd != "" && kthread.Cmd[0] == '[', "kernel thread protected and bracketed: %+v", kthread)
	sshd, ok := byName["sshd"]
	check(ok && sshd.Protected == procs.ReasonSystem, "sshd protected: %+v", sshd)
	web, ok := byName["nas-revival"]
	check(ok && web.Protected == procs.ReasonRevival, "nas-revival protected: %+v", web)

	hc := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock"}
	targets := []procs.Process{init, sshd, web}
	if kthread != nil {
		targets = append(targets, *kthread)
	}
	for _, p := range targets {
		_, err := hc.ProcessKill(ctx, p.PID, p.Start, true)
		check(code(err) == helper.CodeProcessProtected, "kill %s (%d) refused: %v", p.Name, p.PID, err)
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		check(false, "start sleep: %v", err)
		os.Exit(1)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// Until exec the child still carries the parent's name.
	var st procs.Stat
	for i := 0; i < 50; i++ {
		if st, err = procs.ReadStat("/", cmd.Process.Pid); err != nil || st.Name == "sleep" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	check(err == nil && st.Name == "sleep", "sleep %d started: %+v %v", cmd.Process.Pid, st, err)
	_, err = hc.ProcessKill(ctx, st.PID, st.Start+1, false)
	check(code(err) == helper.CodeProcessChanged, "wrong start time refused: %v", err)
	select {
	case <-done:
		check(false, "sleep ended after a refused request")
	case <-time.After(300 * time.Millisecond):
	}
	res, err := hc.ProcessKill(ctx, st.PID, st.Start, false)
	check(err == nil && res.Signal == "TERM" && res.Name == "sleep", "stop sleep: %+v %v", res, err)
	select {
	case err := <-done:
		check(err != nil, "sleep ended by the signal: %v", err)
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		check(false, "sleep still running 5 s after TERM")
	}
	_, err = hc.ProcessKill(ctx, st.PID, st.Start, true)
	check(code(err) == helper.CodeNotFound, "gone process: %v", err)

	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("OK")
}
