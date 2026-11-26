// Command e2e-power-tasks is the W3.5 end-to-end check on the NAS (scripts/webui/e2e-power-tasks.sh runs it
// as nas-revival). It adds a disabled reboot and a disabled shutdown task, checks the rendered timers
// (not persistent, not active), that "run now" is refused, that every preflight blocker code is known to the
// runner, and restores the previous task list. It never restarts or shuts down the NAS.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/tasks"
)

var fails int

func check(ok bool, format string, a ...any) {
	status := "ok  "
	if !ok {
		status, fails = "FAIL", fails+1
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, a...))
}

func show(unit string) string {
	out, _ := exec.Command("systemctl", "show", "--property=Persistent,ActiveState,UnitFileState", "--", unit).Output()
	return strings.Join(strings.Fields(string(out)), " ")
}

func main() {
	ctx := helper.WithUser(context.Background(), "w35-e2e")
	c := &helper.Client{Socket: "/run/nas-revival-helper/helper.sock"}
	orig, err := c.TaskGet(ctx)
	if err != nil {
		fmt.Println("FAIL task.get:", err)
		os.Exit(1)
	}
	check(true, "%d tasks before", len(orig.Tasks))
	defer func() {
		r, err := c.TaskSet(ctx, orig.Tasks)
		check(err == nil && len(r.Tasks) == len(orig.Tasks), "previous task list restored: %d tasks, err %v", len(r.Tasks), err)
		if fails > 0 {
			fmt.Printf("%d check(s) failed\n", fails)
			os.Exit(1)
		}
		fmt.Println("all checks passed")
	}()

	sched := tasks.Schedule{Kind: tasks.KindMonthly, Time: "04:44", Day: 28}
	add := []tasks.Task{
		{Name: "E2E restart", Type: tasks.TypeReboot, Enabled: false, Schedule: sched},
		{Name: "E2E vypnutí", Type: tasks.TypeShutdown, Enabled: false, Schedule: sched},
	}
	r, err := c.TaskSet(ctx, append(slices.Clone(orig.Tasks), add...))
	check(err == nil && len(r.Tasks) == len(orig.Tasks)+2, "power tasks saved: err %v", err)
	if err != nil {
		return
	}
	for _, t := range r.Tasks[len(orig.Tasks):] {
		b, err := os.ReadFile("/etc/systemd/system/" + tasks.TimerName(t.ID))
		check(err == nil && strings.Contains(string(b), "Persistent=false\n") && strings.Contains(string(b), "OnCalendar=*-*-28 04:44:00\n"),
			"%s timer file not persistent: err %v", t.Type, err)
		st := show(tasks.TimerName(t.ID))
		check(strings.Contains(st, "Persistent=no") && strings.Contains(st, "ActiveState=inactive"), "%s timer: %s", t.Type, st)
		err = c.TaskRun(ctx, t.ID)
		he, _ := helper.AsError(err)
		check(he != nil && he.Code == helper.CodeRunNotAllowed, "%s run now refused: %v", t.Type, err)
		st = show(tasks.ServiceName(t.ID))
		check(strings.Contains(st, "ActiveState=inactive"), "%s service not started: %s", t.Type, st)
	}

	pf, err := c.PowerPreflight(ctx)
	known := tasks.BlockerCodes()
	unknown := []string{}
	for _, b := range pf.Blockers {
		if !slices.Contains(known, b.Code) {
			unknown = append(unknown, b.Code)
		}
	}
	check(err == nil && len(unknown) == 0, "preflight: %d blockers %+v, unknown codes %v, err %v", len(pf.Blockers), pf.Blockers, unknown, err)
}
