#!/usr/bin/env python3
"""Run a local script on the NAS over the stock root Telnet and print its output.

Usage: telnet_run.py HOST SECRET_ENV SCRIPT > output/FILE
Logs in with admin_user/admin_password from SECRET_ENV (Git-ignored), uses su
with su_password when not root. Credentials are never printed or logged; keep
the output in output/ (Git-ignored)."""
import re
import socket
import sys
import time

IAC, DONT, DO, WONT, WILL, SB, SE = 255, 254, 253, 252, 251, 250, 240


def load_secrets(path):
    out = {}
    for line in open(path, encoding="utf-8"):
        if "=" in line and not line.lstrip().startswith("#"):
            k, v = line.rstrip("\n").split("=", 1)
            out[k.strip()] = v.strip().strip('"').strip("'")
    return out


class Telnet:
    def __init__(self, host):
        self.s = socket.create_connection((host, 23), timeout=15)
        self.buf = b""

    def _recv(self, timeout):
        self.s.settimeout(timeout)
        try:
            data = self.s.recv(65536)
        except socket.timeout:
            return False
        if not data:
            raise EOFError("connection closed")
        i, clean = 0, bytearray()
        while i < len(data):
            b = data[i]
            if b == IAC and i + 1 < len(data):
                cmd = data[i + 1]
                if cmd in (DO, DONT, WILL, WONT) and i + 2 < len(data):
                    opt = data[i + 2]
                    reply = WONT if cmd in (DO, DONT) else DONT
                    self.s.sendall(bytes([IAC, reply, opt]))
                    i += 3
                    continue
                if cmd == SB:
                    end = data.find(bytes([IAC, SE]), i)
                    i = end + 2 if end != -1 else len(data)
                    continue
                i += 2
                continue
            clean.append(b)
            i += 1
        self.buf += bytes(clean)
        return True

    def expect(self, pattern, timeout=20):
        rx = re.compile(pattern)
        deadline = time.monotonic() + timeout
        while True:
            m = rx.search(self.buf)
            if m:
                out = self.buf[:m.end()]
                self.buf = self.buf[m.end():]
                return out
            left = deadline - time.monotonic()
            if left <= 0:
                raise TimeoutError(f"waiting for {pattern!r}")
            self._recv(min(left, 2))

    def send(self, text):
        self.s.sendall(text.encode() + b"\n")


def main():
    host, secrets_path, script_path = sys.argv[1:4]
    sec = load_secrets(secrets_path)
    t = Telnet(host)
    t.expect(rb"(?i)login:\s*$", 20)
    t.send(sec["admin_user"])
    t.expect(rb"(?i)password:\s*$", 20)
    t.send(sec["admin_password"])
    t.expect(rb"[#$>] ?$", 20)
    t.send("id -u")
    out = t.expect(rb"\n\d+\s*\r?\n.*[#$>] ?$", 20)
    if not re.search(rb"\n0\s*\r?\n", out):
        t.send("su")
        t.expect(rb"(?i)password:\s*$", 20)
        t.send(sec["su_password"])
        t.expect(rb"[#$>] ?$", 20)
        t.send("id -u")
        out = t.expect(rb"\n\d+\s*\r?\n.*[#$>] ?$", 20)
        if not re.search(rb"\n0\s*\r?\n", out):
            sys.exit("not root after su")
    t.send("stty -echo 2>/dev/null; PS1='__P''# '; export PS1")
    t.expect(rb"__P# $", 20)
    body = open(script_path, encoding="utf-8").read()
    # Feed the script on stdin of a subshell; output ends with a marker.
    t.send("sh -s <<'__PROBE_EOF__'\n" + body + "\n__PROBE_EOF__\necho __DONE__")
    out = t.expect(rb"__DONE__\s*\r?\n", 300)
    sys.stdout.write(out.decode(errors="replace").replace("\r", ""))
    t.send("exit")
    t.send("exit")


if __name__ == "__main__":
    main()
