# Debian deployment of the web UI

Deployed to the NAS on 2026-11-25 (about 24 MiB RSS after start). Build on the workstation with
`scripts/webui/build.sh` (output in `output/webui/`), then on the NAS as root:

```sh
apt install smartmontools hdparm     # SMART card; disk power state and APM
useradd --system --no-create-home --shell /usr/sbin/nologin nas-revival
install -m 0755 nas-revival-armv5 /usr/local/bin/nas-revival
install -m 0644 nas-revival.service nas-revival-helper.service /etc/systemd/system/
install -d -m 0700 -o nas-revival -g nas-revival /etc/nas-revival
nas-revival set-password -config /etc/nas-revival     # at least 12 characters
chown nas-revival:nas-revival /etc/nas-revival/admin.json
systemctl daemon-reload && systemctl enable --now nas-revival-helper nas-revival
```

Web login (W2.2, ADR 0004): `admin.json` is the recovery administrator (set over SSH as above);
NAS accounts log in with the web hash the helper keeps in `/etc/nas-revival/credentials.json`, role by
membership in `nas-admin` / `nas-viewer` (created by the helper on first use). After deploying, run
`scripts/webui/e2e-roles.sh` from the workstation: it creates three throw-away accounts, checks login,
the role policy, own password change, role change and disable over HTTPS, and deletes them again.

Shared folders (W2.3) live in `/etc/nas-revival/shares.json` (root, 0644, written only by the helper);
folders are `<data volume>/<name>`, the helper unit needs `ReadWritePaths=-/srv`. With Samba installed
the helper also writes `/etc/samba/nas-revival-shares.conf` (checked with `testparm`, smb.conf has to
include it – W2.5). After deploying, run `scripts/webui/e2e-shares.sh` from the workstation: it creates a
throw-away admin, user and share `nrt-e2e`, checks create/edit/permissions/delete over HTTPS and removes them.

Disk power settings (W1.5) live in `/etc/nas-revival/disks.json` (root, 0644, written only by the
helper). The helper applies them to every internal disk at start; a disk without an entry gets the
balanced profile (APM 254, no standby on the system disk).

Disk activity (W1.9, op `disk.activity`) samples `/proc/diskstats`, jbd2 counters and the files every
process holds open for writing, waits 10–45 s and searches the mounted file systems of disks that
did I/O for files changed in the window. Under `ProtectHome=yes` the helper does not see `/home` and
`/root`, so writes there are counted on the disk but not attributed to a file.

Restart and shutdown (W1.6) go through the helper ops `power.preflight`, `power.reboot` and
`power.shutdown`: preflight checks running SMART self-tests (sleeping disks are not woken),
`nsa310-copy.service`, apt/dpkg processes and a pending network change; the helper then calls `sync` and
`systemctl reboot`/`poweroff` 2 s after answering. The NSA310 device tree has `gpio-poweroff`, so a
shutdown should cut the power; verify on the first real shutdown.

The helper (`nas-revival helper`, ADR 0003) runs as root and lets the web UI change IP, DNS, NTP:
socket `/run/nas-revival-helper/helper.sock` (0660 root:nas-revival, only uid `nas-revival` is served),
it writes only `/etc/systemd/network/20-wired.network` and `/etc/systemd/timesyncd.conf.d/50-nas-revival.conf`.
A network change must be confirmed within 180 s from the new address, otherwise the previous file
is restored (also after a helper restart or reboot). Defaults: interface `enp1s0` (`-iface`),
state `/var/lib/nas-revival-helper`. If the web UI shows "helper_unavailable", check
`systemctl status nas-revival-helper`. The TLS certificate is handled by the web service itself
(Settings, or `cert.pem`/`key.pem` in `/var/lib/nas-revival/tls`).

NAS users (W2.1): the helper runs `useradd`, `usermod`, `userdel`, `chpasswd`, `gpasswd`,
`groupadd`, `groupdel` (and `smbpasswd`/`pdbedit` once Samba is installed) with validated names, so
its unit has `ReadWritePaths=/etc` (the shadow tools write lock and temp files next to
`/etc/passwd`). The first created user also creates the system group `nas-users`. Web password
hashes go to `/etc/nas-revival/credentials.json` (0600, owner `nas-revival`). After updating the
unit: `systemctl daemon-reload && systemctl restart nas-revival-helper`.

Fan control (independent of the web UI; the LM85 regulates the fan itself once configured):

```sh
install -m 0755 nsa310-fan /usr/local/sbin/nsa310-fan
install -m 0644 nsa310-fan.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now nsa310-fan
```

Installed on the NAS 2026-11-25. Thresholds are in the script header.

Front panel and network port LEDs (SYS green after boot, HDD disk activity, USB port activity,
RTL8111D link/activity/gigabit; needs the LED triggers enabled by `scripts/debian/build-kernel.sh`):

```sh
install -m 0755 nsa310-leds /usr/local/sbin/nsa310-leds
install -m 0644 nsa310-leds.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now nsa310-leds
```

`scripts/debian/build-rootfs.sh` already installs and enables the fan and LED services.

COPY button backup (`nsa310-button` is built from `backend/cmd/nsa310-button` with
`GOARCH=arm GOARM=5`; needs `apt install rsync`):

```sh
install -m 0755 nsa310-button-armv5 /usr/local/bin/nsa310-button
install -m 0755 nsa310-copy /usr/local/sbin/nsa310-copy
install -m 0644 nsa310-copy.service nsa310-button.service /etc/systemd/system/
blkid -o value -s UUID /dev/sdX1 >> /etc/nsa310-copy.exclude   # every rescue stick partition
systemctl daemon-reload && systemctl enable --now nsa310-button
```

Installed on the NAS 2026-11-25. `/etc/nsa310-copy.exclude` stays on the device only.

The UI listens on HTTPS port 8443 with a self-signed certificate generated on first start
in `/var/lib/nas-revival/tls` (replace `cert.pem`/`key.pem` to avoid the browser warning).
There is one administrator account, independent of Linux and Samba users.

To verify on the device: memory use, `systemctl show` inside the sandbox, and whether
`smartctl` works with only `CAP_SYS_RAWIO` and group `disk`.

Base system tuning (owner decision 2026-11-25, ADR 0007):

```sh
install -d /etc/systemd/resolved.conf.d /etc/systemd/journald.conf.d
install -m 0644 50-nas-revival-resolved.conf /etc/systemd/resolved.conf.d/50-nas-revival.conf   # LLMNR off
install -m 0644 50-nas-revival-journald.conf /etc/systemd/journald.conf.d/50-nas-revival.conf   # 15 min sync, 64M cap
systemctl restart systemd-resolved systemd-journald
```
