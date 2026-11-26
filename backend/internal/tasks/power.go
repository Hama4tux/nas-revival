package tasks

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrPowerBlocked is what PowerBackend.Power returns when the helper's own
// preflight found work that a restart or shutdown would cut off.
var ErrPowerBlocked = errors.New("power action blocked")

// PowerBackend restarts or shuts down the NAS through the helper.
type PowerBackend interface {
	// PowerBlockers runs the power preflight (CORE-13) and returns the
	// blocker codes; empty means nothing would be interrupted.
	PowerBlockers(ctx context.Context) ([]string, error)
	// Power starts "reboot" or "shutdown" without force; user goes to the
	// helper log.
	Power(ctx context.Context, action, user string) error
}

// Power task defaults.
const (
	// PowerWait is how long a power task waits for running work to finish.
	PowerWait = time.Hour
	// PowerRetry is the preflight polling interval while waiting.
	PowerRetry = time.Minute
	// MinUptime: a power task right after boot is skipped (a clock that
	// jumps forward at boot can make a timer elapse at once).
	MinUptime = 15 * time.Minute
)

// Messages of the power tasks.
const (
	MsgJustBooted   = "NAS běží teprve krátce po startu, úloha se přeskočila."
	MsgUptimeFailed = "Dobu běhu NASu se nepodařilo zjistit."
	MsgPowerFailed  = "Restart ani vypnutí se nepodařilo spustit."
)

// blockerText describes the preflight blocker codes (helper.Block*).
var blockerText = map[string]string{
	"smart_test":      "běží test SMART",
	"copy_running":    "běží kopírování tlačítkem COPY",
	"backup_running":  "běží záloha",
	"task_running":    "běží jiná náročná úloha",
	"package_manager": "probíhá instalace balíčků",
	"network_pending": "změna sítě čeká na potvrzení",
	"check_failed":    "kontrolu se nepodařilo provést",
}

// BlockerCodes lists the codes blockerText knows (for the helper test).
func BlockerCodes() []string {
	out := make([]string, 0, len(blockerText))
	for c := range blockerText {
		out = append(out, c)
	}
	return out
}

// describeBlockers: "běží záloha, probíhá instalace balíčků" (unique, in order).
func describeBlockers(codes []string) string {
	seen := map[string]bool{}
	parts := []string{}
	for _, c := range codes {
		s, ok := blockerText[c]
		if !ok {
			s = "probíhá jiná činnost"
		}
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// ProcUptime reads /proc/uptime.
func ProcUptime() (time.Duration, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, errors.New("empty /proc/uptime")
	}
	sec, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(sec * float64(time.Second)), nil
}

// power runs a reboot or shutdown task: never right after boot, never with
// force; while the preflight finds work it waits up to PowerWait, then the
// run is skipped. It does not take the heavy lock: the preflight checks it.
func (r *Runner) power(ctx context.Context, t Task) (string, string) {
	pb, ok := r.Backend.(PowerBackend)
	if !ok {
		return ResultFailed, MsgPowerFailed
	}
	if r.Uptime == nil {
		return ResultFailed, MsgUptimeFailed
	}
	up, err := r.Uptime()
	if err != nil {
		return ResultFailed, MsgUptimeFailed
	}
	if up < r.MinUptime {
		return ResultSkipped, MsgJustBooted
	}
	action, done := "reboot", "Restart zahájen."
	if t.Type == TypeShutdown {
		action, done = "shutdown", "Vypnutí zahájeno."
	}
	deadline := r.Now().Add(r.PowerWait)
	for {
		codes, err := pb.PowerBlockers(ctx)
		switch {
		case ctx.Err() != nil:
			return ResultStopped, MsgStopped
		case err != nil:
			return ResultFailed, failMsg(err)
		}
		if len(codes) == 0 {
			err := pb.Power(ctx, action, "task:"+t.ID)
			if err == nil {
				return ResultOK, done
			}
			if !errors.Is(err, ErrPowerBlocked) {
				return ResultFailed, failMsg(err)
			}
			// Something started between the two checks: wait like for a blocker.
			codes = []string{"other"}
		}
		if !r.Now().Before(deadline) {
			return ResultSkipped, "Přeskočeno, " + describeBlockers(codes) + "."
		}
		if err := r.Sleep(ctx, r.PowerRetry); err != nil {
			return ResultStopped, MsgStopped
		}
	}
}
