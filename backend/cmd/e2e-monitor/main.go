// Command e2e-monitor checks the Resource Monitor (W3.1) against the live
// /proc and /sys of the NAS: two readings 10 s apart must give one sample
// with plausible values. Read-only; run as the nas-revival user.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"nas-revival/backend/internal/monitor"
)

func main() {
	m := monitor.New("/")
	start := time.Now()
	m.Tick()
	time.Sleep(m.Interval)
	m.Tick()
	tickCost := time.Since(start) - m.Interval
	h := m.History(0)
	var fails []string
	check := func(ok bool, msg string) {
		if !ok {
			fails = append(fails, msg)
		}
	}
	check(len(h.Samples) == 1, fmt.Sprintf("want 1 sample, got %d", len(h.Samples)))
	check(h.MemTotalBytes > 0, "memTotalBytes is 0")
	if len(h.Samples) == 1 {
		s := h.Samples[0]
		busy := s.CPU.User + s.CPU.System + s.CPU.IOWait + s.CPU.IRQ + s.CPU.Steal
		check(busy >= 0 && busy <= 100.5, fmt.Sprintf("cpu busy %.1f %% out of range", busy))
		check(s.Memory.UsedBytes > 0 && s.Memory.UsedBytes < h.MemTotalBytes, "memory used out of range")
		check(len(s.Disks) > 0, "no disks")
		for _, d := range s.Disks {
			check(d.BusyPercent >= 0 && d.BusyPercent <= 100, d.Name+": busy out of range")
		}
		check(len(s.Net) > 0, "no network interfaces")
		check(len(h.NetTotals) == len(s.Net), "netTotals do not match interfaces")
		b, _ := json.Marshal(s)
		fmt.Printf("sample (%d bytes JSON): %s\n", len(b), b)
	}
	fmt.Printf("psiAvailable=%v, two readings took %s\n", h.PSIAvailable, tickCost.Round(time.Millisecond))
	if len(fails) > 0 {
		for _, f := range fails {
			fmt.Println("FAIL:", f)
		}
		os.Exit(1)
	}
	fmt.Println("OK")
}
