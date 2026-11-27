# Debian deployment of the web UI

Deployed to the NAS on 2026-11-25 (about 24 MiB RSS after start). Build on the workstation with
`scripts/webui/build.sh` (output in `output/webui/`), then on the NAS as root:

```sh
apt install smartmontools hdparm     # SMART card; disk power state and APM
useradd --system --no-create-home --shell /usr/sbin/nologin nas-revival
install -m 0755 nas-revival-armv5 /usr/local/bin/nas-revival
install -m 0644 nas-revival.service nas-revival-helper.service /etc/systemd/system/
install -d -m 0700 -o nas-revival -g nas-revival /etc/nas-revival
nas-revival set-password -config /etc/nas-revival     # must not be empty
chown nas-revival:nas-revival /etc/nas-revival/admin.json
systemctl daemon-reload && systemctl enable --now nas-revival-helper nas-revival
```

Scheduled tasks (W3.3): the helper keeps `/etc/nas-revival/tasks.json` (root, 0644; without it only the
daily recycle bin cleanup at 03:30 exists) and writes one `nas-revival-task-<id>.timer` per task to
`/etc/systemd/system`; all of them start the template `nas-revival-task@.service`, which runs
`nas-revival task run <id>` as `nas-revival` and records the outcome in `/var/lib/nas-revival/task-history.json`.
Heavy jobs are serialised with `flock` on `/run/lock/nas-heavy.lock` (group `nas-heavy`, created by tmpfiles).
Install once, as root (the helper reconciles the timers when it starts):

```sh
groupadd --system nas-heavy
install -m 0644 nas-revival-task@.service /etc/systemd/system/
install -m 0644 nas-revival-tmpfiles.conf /etc/tmpfiles.d/nas-revival.conf
systemd-tmpfiles --create /etc/tmpfiles.d/nas-revival.conf
systemctl daemon-reload && systemctl restart nas-revival-helper
```

Backup pull jobs (W4.3, ADR 0008): task type `backup-pull`, one `nas-revival-backup@<id>.service` per task
(unit `nas-revival-backup@.service`), run as the unprivileged account `nas-backup` (home `/var/lib/nas-backup`,
0700; key `.ssh/id_ed25519` 0600; history `task-history.json` 0640, read by the web UI through its supplementary
group `nas-backup`). Data: `/srv/data/apps/<id>/<path>/` is the mirror (`rsync --delete`),
`/srv/data/versions/<date>/<id>/` the hardlink snapshots (kept `keepDays` newest days). Since 2026-11-27 these names are
no longer reserved and the pseudo share `zalohy` is gone; the destination becomes a shared folder with "Cíl záloh"
(`backupTarget`, see docs/api/v1.md). Server host keys are trusted in the UI (Tasks, "Ověřit server") and stored in
`/etc/nas-revival/backup_known_hosts` (root, 0644, written only by the helper). The NAS public key goes to the server's
`authorized_keys` with `from="<NAS IP>",restrict,command="rrsync -ro <folder>"` (the UI shows the line). Install once, as root:

```sh
groupadd --system nas-backup
useradd --system --gid nas-backup --home-dir /var/lib/nas-backup --no-create-home --shell /usr/sbin/nologin nas-backup
usermod -aG nas-heavy nas-backup
apt install rsync openssh-client
install -d -m 0700 -o nas-backup -g nas-backup /var/lib/nas-backup /srv/data/apps /srv/data/versions
install -m 0644 nas-revival-backup@.service nas-revival.service nas-revival-helper.service /etc/systemd/system/
systemctl daemon-reload && systemctl restart nas-revival-helper nas-revival
```

Migration of the interim `nas-pull` job (once, as root, after installing the new binary and the unit):
`./migrate-nas-pull.sh [HH:MM]` creates the account, copies the key and known hosts, takes over `/srv/data/apps` and
`/srv/data/versions`, imports `/etc/nas-pull/jobs.conf` as backup-pull tasks (`nas-revival task import-pull`; daily
06:45, 35 days, freshness file `nas-export/last-ok` 26 h when a path is `nas-export`) and disables `nas-pull.timer`.
It is idempotent. Afterwards check Tasks in the web UI and run the task once. Versions and restore (phase 2): "Verze a obnova" on a backup task lists the versions, browses them and downloads single
files; the helper opens each file as root and passes the descriptor to the web process, which keeps no read access to
`/srv/data/versions`. After deploying, run `scripts/webui/e2e-backup.sh` (read-only; also walks the newest version of
every job to a file and reads it).

Web login (W2.2, ADR 0004): `admin.json` is the recovery administrator (set over SSH as above);
NAS accounts log in with the web hash the helper keeps in `/etc/nas-revival/credentials.json`, role by
membership in `nas-admin` / `nas-viewer` (created by the helper on first use). After deploying, run
`scripts/webui/e2e-roles.sh` from the workstation: it creates three throw-away accounts, checks login,
the role policy, own password change, role change and disable over HTTPS, and deletes them again.

Shared folders (W2.3) live in `/etc/nas-revival/shares.json` (root, 0644, written only by the helper);
folders are `<data volume>/<name>`, the helper unit needs `ReadWritePaths=-/srv`. With Samba installed
the helper also writes `/etc/samba/nas-revival-shares.conf` (checked with `testparm`, smb.conf has to
include it – done by the SMB settings, see below). After deploying, run `scripts/webui/e2e-shares.sh` from the workstation: it creates a
throw-away admin, user and share `nrt-e2e`, checks create/edit/permissions/delete over HTTPS and removes them.

Services (W2.4): the helper runs `systemctl show/start/stop/…`, `ss -H -l -t -u -n -p` and
`journalctl --unit=… --output=json` only for the fixed catalog (`backend/internal/services`). RAM is the RSS
of the unit's cgroup processes because the NAS kernel has no memory controller. After deploying, run
`scripts/webui/e2e-services.sh` from the workstation: it lists the services, checks SSH (PID, memory,
port 22, CPU %), that SSH cannot be stopped, the SSH journal, and restarts cron once.

SMB (W2.5): install Samba with `install-samba.sh` (as root on the NAS; `--no-install-recommends`, a
temporary `policy-rc.d` keeps Debian from starting smbd/nmbd with its default config, both stay disabled and
the Debian template is kept as `smb.conf.debian`). The helper owns `/etc/samba/smb.conf` from the first save in
Services → SMB: it renders it from `/etc/nas-revival/smb.json` (enabled, workgroup, description, minimum
protocol SMB2_10/SMB3_00/SMB3_11, NetBIOS), checks it with `testparm`, includes the share fragment and runs
`systemctl enable/disable --now smbd [nmbd]` or `smbcontrol smbd reload-config`. Fixed: standalone server,
no guest (`map to guest = never`), `restrict anonymous = 2`, no printers, no user shares, SMB1 off, port 445 only
unless NetBIOS is on (then also 139 and nmbd). Sessions and open files come from `smbstatus --json` (admins only).
The helper unit needs `ReadWritePaths=-/var/lib/samba -/var/cache/samba -/run/samba` for `smbpasswd`/`smbstatus`.
NAS accounts get their Samba password with the next password change; accounts created before Samba was
installed have none (the SMB tab lists them). After deploying, run `scripts/webui/e2e-smb.sh` from the
workstation (needs `smbclient`): it creates throw-away accounts and shares, enables SMB, checks read/write/deny,
guest, wrong password, SMB1/SMB2 refused, hidden and read-only share, recycle bin, session and open-file list
with a real client, then restores the previous SMB settings and removes everything.

NFS (W2.6): needs a kernel with `CONFIG_NFSD` (module `nfsd`, in `scripts/debian/build-kernel.sh` since
W2.6; in slot A since 2026-11-26). Install with `install-nfs.sh` (as root on the NAS;
`--no-install-recommends`, a temporary `policy-rc.d`): it writes `/etc/nfs.conf.d/nas-revival.conf`
(NFSv4.1/4.2 only, TCP, 4 threads), leaves `nfs-server` disabled on a fresh install (a re-run keeps the UI state) and masks `rpc-statd`. Ports: TCP 2049
and TCP 111 – Proxmox checks an NFSv4 storage with `rpcinfo -T tcp <nas> nfs 4`, which asks rpcbind, and reports
"not online" without it; a socket drop-in limits rpcbind to TCP and `/etc/default/rpcbind` binds its UDP socket to loopback. The helper owns `/etc/exports.d/nas-revival.exports` (`/etc/exports` stays untouched):
it renders it from `/etc/nas-revival/nfs.json` (exports by share name, clients as IP or network, read-only per
client, root or all squash, always `sync,secure,no_subtree_check,sec=sys`, `anonuid=65534`,
`anongid=<nas-nfs>`), runs `systemctl enable/disable --now nfs-server` and `exportfs -ra` and puts the old
files back when the server refuses them. NFS off renders an empty exports file. A share that is exported
cannot be deleted; a share change (read-only, rollback) re-renders the exports. Connected NFSv4 clients come
from `/proc/fs/nfsd/clients` (admins only). The helper unit needs `ReadWritePaths=-/var/lib/nfs` (etab). The helper has to be restarted after the install (the script does it):
its sandbox only sees `/var/lib/nfs` as writable if the directory existed when it started. For the same reason the
helper unit has `Wants=`/`After=proc-fs-nfsd.mount`: otherwise, after a reboot, it never sees `/proc/fs/nfsd/clients`
(the connected client list stays empty). That loads the `nfsd` module (~360 KB) at boot even with NFS off. Installed on the NAS
2026-11-26. The NAS kernel has the NFS server but no NFS client, so `scripts/webui/e2e-nfs.sh <client>` mounts
from another machine (needs `mount.nfs4` and root or sudo there); passed from Proxmox 2026-11-26. With ACLs (new
kernel) squashed clients can only write where the member-less group `nas-nfs` (created by install-nfs.sh or the
first NFS save) has read/write; it gives SMB users no access. The NFS tab warns when it is missing.
Proxmox: `pvesm add nfs <id> --server <nas> --export /srv/data/<share> --content backup --options vers=4.2`
(the Proxmox export scan uses NFSv3 and finds nothing; the UI shows the line).

SSH/SFTP (W2.7): the helper owns `/etc/ssh/sshd_config.d/10-nas-revival.conf` from the first save in
Services → SSH, rendered from `/etc/nas-revival/ssh.json` (port, password login, SFTP groups). Debian
includes the drop-ins before its own options and sshd keeps the first value, so it wins over `sshd_config`.
Fixed: `PermitRootLogin prohibit-password`, `AllowGroups root <SFTP groups>`, `AuthorizedKeysFile
.ssh/authorized_keys /etc/ssh/nas-revival-keys/%u` (directory 0755; one root-owned 0644 file per NAS user
written by Services → SSH → Keys, W2.7b), SFTP groups
in `Match Group` with `internal-sftp` chrooted to `/srv`. The helper runs `sshd -t`, puts the old drop-in
back when sshd rejects it and runs `systemctl reload ssh.service`; open sessions stay. No package to install.
After deploying, run `scripts/webui/e2e-ssh.sh` from the workstation: it creates throw-away accounts and the
group `nrt-sftp`, logs in with a real SSH client on the NAS (SFTP in the chroot, no command or TTY, account
outside the group and wrong password refused, password login off, user keys uploaded over HTTPS – login
with password off, refused outside the SFTP groups, for a disabled account and after removal, removed with
the account – port 2222 and back) and restores the previous settings. Passed 2026-11-26 (keys too).

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

Network page (W2.8): nothing to install (ping, ss, hostnamectl and resolvectl are in the base system). After deploying,
run `scripts/webui/e2e-network.sh` from the workstation: it reads the network overview as `nas-revival`, sets the current
host name again (no change) and runs every diagnostic once (pings of the gateway and of a TEST-NET address, a DNS lookup,
one HTTPS connect to deb.debian.org, the listening ports and connections).

Resource Monitor (W3.1): nothing to install. After deploying, run `scripts/webui/e2e-monitor.sh` from the
workstation: it takes two readings of `/proc` and `/sys` 10 s apart as `nas-revival` and checks the sample.

Processes (W3.2): nothing to install. After deploying (wait until the helper logs `helper listening`, ~40 s after a
restart), run `scripts/webui/e2e-processes.sh`: as `nas-revival` it reads the list, has the helper refuse PID 1, a kernel
thread, sshd and nas-revival and a wrong start time, then starts its own `sleep` and stops it through the helper.

Hardware (W3.4): the helper keeps `/etc/nas-revival/hardware.json` (root, 0644; without it the defaults: balanced
fan profile, board 55/65 °C, disk 50/55 °C, night mode off, fault blink on) and renders `/etc/default/nsa310-fan`
(profile limits, read by `nsa310-fan`, which ignores values outside the safe window and falls back to the balanced
profile), `/etc/default/nsa310-leds` (night window) and `/etc/systemd/system/nsa310-leds.timer` (re-runs
`nsa310-leds.service` at both switch times; the script decides day or night from the clock). The red system LED
blinks through `nsa310-led-fault@on|off.service`, because the helper's sandbox keeps `/sys` read-only.
Install once, as root, then restart the helper:

```sh
install -m 0755 nsa310-fan nsa310-leds /usr/local/sbin/
install -m 0644 nsa310-led-fault@.service /etc/systemd/system/
systemctl daemon-reload && systemctl restart nsa310-fan nsa310-leds nas-revival-helper
```

After deploying, run `scripts/webui/e2e-hardware.sh`: it reads the hardware state as `nas-revival`, switches the fan
profile to quiet and back (read back from the LM85), switches night mode on for the current minute and off again, and
blinks the red LED for a moment.

Scheduled restart and shutdown (W3.5) are task types `reboot` and `shutdown` in `tasks.json`; their timers have
`Persistent=false`, the runner skips them within 15 min of boot and waits up to 1 h for an empty power preflight.
After deploying, run `scripts/webui/e2e-power-tasks.sh`: it adds two disabled power tasks, checks the timers and that
"run now" is refused, and restores the task list. It never restarts the NAS.

## File Station (W4.1)

No unit or package change: the binary serves `/api/v1/files` and the helper runs `fs.*` (ADR 0009). Check with
`scripts/webui/e2e-files.sh [host] [MiB]` from the workstation: it creates the throw-away users `nrt-fu`/`nrt-fr`
and share `nrt-files`, tests the operations over HTTPS, prints owner and mode of the created entries and
removes everything. The optional big upload (second argument, default 0) keeps the NAS CPU at 100 % for minutes
(TLS on one ARM core) – run it only when that is acceptable.

## USB storage (W4.2)

Storage → USB mounts USB partitions (ext2/3/4, FAT) through `systemd-mount` below `/srv/usb/<label>`
(nosuid, nodev, noexec; FAT as root:nas-users 0770/0660), where shared folders can use them, and ejects a
disk: unmount, flush, remove the device. The removal runs outside the helper's sandbox:

```sh
install -m 0755 nas-revival-usb-eject /usr/local/sbin/nas-revival-usb-eject
install -m 0644 nas-revival-usb-eject@.service /etc/systemd/system/
systemctl daemon-reload
```

Partitions whose UUID is in `/etc/nsa310-copy.exclude` (rescue stick) or labelled `nrinstall` (disk swap
stick) are never mounted. exFAT and NTFS need the kernel options `CONFIG_EXFAT_FS` / `CONFIG_NTFS3_FS`.

Config export/import (W4.4, ADR 0011) needs nothing extra. After deploying, run `scripts/webui/e2e-config.sh`: it
exports, previews and imports the own export (no change), checks that foreign files are refused, imports an extra NTP
server and then the original export again (two `import` versions stay in the config history).

Notification centre (W4.5, ADR 0012) needs nothing extra: the web service creates
`/var/lib/nas-revival/notifications.json` (0600) when the first health problem appears. Check after deploying with
`journalctl -u nas-revival | grep notification` and the bell in the header.
