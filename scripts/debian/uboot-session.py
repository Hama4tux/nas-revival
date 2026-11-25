#!/usr/bin/env python3
"""NSA310 stock U-Boot console session: stop autoboot, then run commands from a file.

Usage (on the machine with the serial adapter, start before the NAS resets):
    sudo python3 -I uboot-session.py <commands file> [/dev/ttyUSB0] [logfile]

Commands file: one U-Boot command per line; '#' lines are ignored. A line
'@wait <seconds>' only reads the console for that long. Everything is logged.
"""
import sys, time, serial

CMDS = sys.argv[1]
DEV = sys.argv[2] if len(sys.argv) > 2 else "/dev/ttyUSB0"
LOG = sys.argv[3] if len(sys.argv) > 3 else "/tmp/nsa310-uart.log"
PROMPT = b"NSA310>>"

s = serial.Serial(DEV, 115200, timeout=0.05)
log = open(LOG, "ab")
buf = b""


def feed(d):
    global buf
    buf += d
    log.write(d)
    log.flush()
    sys.stdout.write(d.decode(errors="replace"))
    sys.stdout.flush()


def pump(sec, until=None):
    end = time.time() + sec
    while time.time() < end:
        d = s.read(4096)
        if d:
            feed(d)
            if until and until in buf:
                return True
    return False


t0 = time.time()
while PROMPT not in buf and time.time() - t0 < 600:
    d = s.read(4096)
    if d:
        feed(d)
    if b"U-Boot" in buf or b"Hit any key" in buf:
        s.write(b" \x03")
if PROMPT not in buf:
    sys.exit("no U-Boot prompt")
pump(0.5)

for line in open(CMDS):
    line = line.rstrip("\n")
    if not line.strip() or line.lstrip().startswith("#"):
        continue
    if line.startswith("@wait"):
        pump(float(line.split()[1]))
        continue
    buf = b""
    s.write(line.encode() + b"\r")
    if line.startswith(("bootm", "run", "boot", "reset")):
        pump(2)  # leaves U-Boot; follow with '@wait N' to capture the boot
    else:
        # the echoed command line itself contains no prompt, so wait for the next one
        pump(30, until=PROMPT)
print("\n== done", flush=True)
