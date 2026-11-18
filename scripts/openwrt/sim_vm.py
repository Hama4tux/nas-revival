#!/usr/bin/env python3
"""Drive an OpenWrt armsr VM in QEMU over its serial console.

Workstation-only simulation of the NAS software stack (procd, uci, storage,
services) under the NSA310 memory budget. It does not emulate Kirkwood, its
drivers or NAND; see docs/research/openwrt-simulation.md.

Usage: sim_vm.py --qemu BIN --kernel K --rootfs R --disk D --script S [--log L]
The script file holds one shell command per line; "#reboot" boots again,
"#powercut" kills QEMU without shutdown and boots again.
"""
import argparse
import os
import select
import subprocess
import sys
import time

READY = b"Please press Enter to activate this console."
# The hostname in the default prompt changes during boot; use our own marker.
PROMPT = b"__P# "


class Vm:
    def __init__(self, args, log):
        self.args = args
        self.log = log
        self.proc = None

    def start(self):
        cmd = [
            self.args.qemu, "-M", "virt", "-cpu", "cortex-a15", "-smp", "1",
            "-m", str(self.args.memory), "-nographic", "-no-reboot",
            "-kernel", self.args.kernel,
            "-append", "root=/dev/vda console=ttyAMA0 panic=-1",
            "-drive", f"file={self.args.rootfs},format=raw,if=virtio",
            "-drive", f"file={self.args.disk},format=qcow2,if=virtio",
            # User-mode NAT only: no listener is opened on the workstation.
            "-netdev", "user,id=wan", "-device", "virtio-net-device,netdev=wan",
        ]
        self.proc = subprocess.Popen(cmd, stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        started = time.monotonic()
        self.expect(READY, self.args.boot_timeout)
        self.log.write(f"\n### boot took {time.monotonic() - started:.1f} s\n")
        self.send(b"\n")
        time.sleep(1)
        # Split literal so the echoed command line does not match PROMPT.
        self.send(b"dmesg -n 1; export PS1='__P''# '\n")
        self.expect(PROMPT, 30)
        # The console appears before procd has run the init scripts; tests
        # must observe the system after boot, not race it.
        self.run("i=0; until logread 2>/dev/null | grep -q 'init complete';"
                 " do sleep 1; i=$((i+1)); [ $i -ge 180 ] && break; done;"
                 " echo BOOTWAIT=$i", 200)
        self.log.write(f"\n### services up after {time.monotonic() - started:.1f} s\n")

    def expect(self, token, timeout):
        buf = b""
        deadline = time.monotonic() + timeout
        while token not in buf:
            left = deadline - time.monotonic()
            if left <= 0 or self.proc.poll() is not None:
                raise RuntimeError(f"timeout or exit waiting for {token!r}")
            ready, _, _ = select.select([self.proc.stdout], [], [], left)
            if ready:
                chunk = os.read(self.proc.stdout.fileno(), 4096)
                buf += chunk
                self.log.write(chunk.decode(errors="replace"))
                self.log.flush()
        return buf

    def send(self, data):
        self.proc.stdin.write(data)
        self.proc.stdin.flush()

    def run(self, command, timeout):
        self.send(command.encode() + b"; echo __RC=$?\n")
        return self.expect(PROMPT, timeout)

    def kill(self):
        if self.proc and self.proc.poll() is None:
            self.proc.kill()
            self.proc.wait()

    def poweroff(self):
        self.send(b"poweroff\n")
        try:
            self.proc.wait(60)
        except subprocess.TimeoutExpired:
            self.kill()


def main():
    p = argparse.ArgumentParser()
    for name in ("qemu", "kernel", "rootfs", "disk", "script"):
        p.add_argument(f"--{name}", required=True)
    p.add_argument("--log", default="-")
    p.add_argument("--memory", type=int, default=256)
    p.add_argument("--boot-timeout", type=int, default=300)
    p.add_argument("--cmd-timeout", type=int, default=600)
    args = p.parse_args()

    log = sys.stdout if args.log == "-" else open(args.log, "w", encoding="utf-8")
    vm = Vm(args, log)
    try:
        vm.start()
        with open(args.script, encoding="utf-8") as script:
            for line in script:
                line = line.strip()
                if not line or line.startswith("# "):
                    continue
                if line == "#reboot":
                    vm.poweroff()
                    vm.start()
                elif line == "#powercut":
                    vm.kill()
                    vm.start()
                else:
                    vm.run(line, args.cmd_timeout)
        vm.poweroff()
    finally:
        vm.kill()
        if log is not sys.stdout:
            log.close()


if __name__ == "__main__":
    main()
