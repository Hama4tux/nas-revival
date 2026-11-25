// nsa310-button waits for the front panel COPY button and starts the USB
// copy job. The job itself is a separate systemd unit, so this process only
// needs to read the input device and ask systemd to start one fixed unit.
package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"nas-revival/backend/internal/evdev"
)

func main() {
	dev := flag.String("device", "/dev/input/by-path/platform-gpio_keys-event", "input device of the front panel buttons")
	unit := flag.String("unit", "nsa310-copy.service", "systemd unit started on COPY press")
	flag.Parse()

	f, err := os.Open(*dev)
	if err != nil {
		slog.Error("cannot open button device", "device", *dev, "err", err)
		os.Exit(1)
	}
	defer f.Close()

	buf := make([]byte, evdev.EventSize)
	var last time.Time
	for {
		if _, err := io.ReadFull(f, buf); err != nil {
			slog.Error("reading button device failed", "err", err)
			os.Exit(1) // systemd restarts the service
		}
		e, err := evdev.Decode(buf)
		if err != nil || !e.IsPress(evdev.KeyCopy) {
			continue
		}
		// A bouncing or impatiently repeated press must not queue copies.
		if time.Since(last) < 3*time.Second {
			continue
		}
		last = time.Now()
		// --no-block: a running job keeps running; systemd ignores a start
		// request for an already active unit.
		out, err := exec.Command("systemctl", "start", "--no-block", *unit).CombinedOutput()
		if err != nil {
			slog.Error("starting copy job failed", "unit", *unit, "err", err, "output", string(out))
			continue
		}
		slog.Info("COPY pressed, copy job requested", "unit", *unit)
	}
}
