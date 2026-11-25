# ADR 0003: Privileged helper for network, DNS and time settings

- Status: accepted (implementation 2026-11-25); the owner asked for IP, DNS, NTP and the certificate in the GUI
- Context: the web service runs as the unprivileged user `nas-revival` in a systemd sandbox
  (ADR 0002). Network, DNS and NTP are configured on the NAS by systemd-networkd
  (`/etc/systemd/network/20-wired.network`, DHCP), systemd-resolved and systemd-timesyncd.

## Decision

- **Helper:** the same Go binary in mode `nas-revival helper`, run as root by
  `nas-revival-helper.service` and listening on the Unix socket
  `/run/nas-revival-helper/helper.sock` (directory 0750 root:nas-revival, socket 0660).
  It accepts only connections whose `SO_PEERCRED` uid is the `nas-revival` user.
- **Protocol:** one JSON request per connection, `{"op": "...", "args": {...}}`, response
  `{"ok": true, "result": ...}` or `{"ok": false, "error": {"code", "message"}}`, request size ≤ 16 KiB,
  5 s read deadline. Operations are a fixed list (`network.get`, `network.apply`,
  `network.confirm`, `time.get`, `time.apply`). The helper validates every field itself and never
  trusts the web process. No shell; external commands are fixed argument lists
  (`networkctl reload`, `networkctl reconfigure <iface>`, `systemctl restart systemd-timesyncd`,
  `timedatectl show`, `timedatectl show-timesync`).
- **Files the helper may write (and nothing else):**
  - `/etc/systemd/network/20-wired.network` (IP mode, address, gateway, DNS),
  - `/etc/systemd/timesyncd.conf.d/50-nas-revival.conf` (NTP servers).
  Writes are atomic (temp file + fsync + rename + fsync dir), mode 0644. The unit uses
  `ProtectSystem=strict` with `ReadWritePaths` limited to these directories and `/var/lib/nas-revival-helper`.
- **DNS** is part of the network file (`DNS=`, and `[DHCPv4] UseDNS=no` when custom servers
  replace the DHCP ones), so one file and one rollback cover IP and DNS.
- **Safe network change:** `network.apply` keeps the previous file as a backup, writes a pending marker
  (`/var/lib/nas-revival-helper/pending.json` with the deadline), applies the change and starts a
  180 s timer. `network.confirm` (called by the UI from the new address after re-login) removes the
  backup and marker. If the timer expires, or the helper starts and finds an expired marker
  (crash, reboot), it restores the backup and reapplies it. Only one pending change at a time.
- **TLS certificate** stays with the web service (its own state directory, no helper):
  upload of PEM certificate + key, or regenerate the self-signed one; served via
  `GetCertificate` so a change needs no restart.

## Consequences

- The attack surface of root code is the helper's small validated JSON API; a compromised web
  process can change IP/DNS/NTP within the validated ranges, but cannot write arbitrary files or run commands.
- A wrong IP cannot lock the owner out permanently: without confirmation the old setting comes back
  within 3 minutes. The serial console remains the last resort.
