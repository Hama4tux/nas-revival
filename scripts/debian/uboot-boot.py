#!/usr/bin/env python3
"""Wait for the NSA310 U-Boot banner, stop autoboot and boot Debian (kernel from USB, RAM only, no saveenv).

Usage (on the machine with the serial adapter, run before powering on the NAS):
    sudo python3 -I uboot-boot.py <root PARTUUID> [/dev/ttyUSB0] [logfile]
"""
import sys, time, serial

ROOT = sys.argv[1]
DEV = sys.argv[2] if len(sys.argv) > 2 else "/dev/ttyUSB0"
LOG = sys.argv[3] if len(sys.argv) > 3 else "/tmp/nsa310-uart.log"
CMDS = [
    "usb start",
    "ext2load usb 0:1 0x2000000 /uImage",
    f"setenv bootargs console=ttyS0,115200 root=PARTUUID={ROOT} rootwait rw",
    "bootm 0x2000000",
]

s = serial.Serial(DEV, 115200, timeout=0.05)
log = open(LOG, "ab")
buf = b""

def pump(sec):
    global buf
    end = time.time() + sec
    while time.time() < end:
        d = s.read(4096)
        if d:
            buf += d; log.write(d); log.flush()
            sys.stdout.write(d.decode(errors="replace")); sys.stdout.flush()

print("== waiting for power-on (up to 10 min)", flush=True)
t0 = time.time()
while b"NSA310>>" not in buf and time.time() - t0 < 600:
    d = s.read(4096)
    if d:
        buf += d; log.write(d); log.flush()
        sys.stdout.write(d.decode(errors="replace")); sys.stdout.flush()
    if b"U-Boot" in buf or b"Hit any key" in buf:
        s.write(b" \x03")
if b"NSA310>>" not in buf:
    sys.exit("no U-Boot prompt")
pump(0.5)
for c in CMDS:
    buf = b""
    s.write(c.encode() + b"\r")
    wait = 15 if c.startswith(("usb", "ext2load")) else 2
    if c.startswith("bootm"):
        wait = 120
    pump(wait)
    if c.startswith("ext2load") and b"bytes read" not in buf:
        sys.exit("ext2load failed")
print("\n== done", flush=True)
