# NAS Revival – architecture

Status: 2026-11-25, Milestone 0 (architecture & audit). This document describes the **running
system** and the **target architecture** for milestones M1–M6 ([ROADMAP](ROADMAP.md)). Decisions are
recorded as ADRs in [adr/](adr/).

## 1. What runs today (audit 2026-11-25)

| Layer | Component | State |
|---|---|---|
| Boot | Stock U-Boot 1.1.4 → NAND slot A (`kernel_2`) → slot B (start of `rootfs2`) → stock firmware | in use, [debian/boot.md](debian/boot.md) |
| Kernel | mainline 6.12.119 LTS, own build, `kirkwood-nsa310a.dtb` appended | in use; updated manually |
| OS | Debian 13.8 armel, systemd, root on internal disk `sda1` (ext4, 30 GB), swap `sda2` (1 GB), data `sda3` → `/srv/data` | in use |
| Web | `nas-revival` – one static Go binary (`GOARM=5`), HTTPS :8443, embedded React UI, API v1 | deployed, read-only overview + SMART |
| Privileged | `nas-revival helper` – same binary, root, Unix socket, typed ops for network/DNS/NTP ([ADR 0003](adr/0003-privileged-helper-settings.md)) | deployed 2026-11-25 (`nas-revival-helper.service`) |
| Board services | `nsa310-leds`, `nsa310-fan` (LM85 automatic mode), `nsa310-button` + `nsa310-copy` (COPY button backup) | in use, [deploy/debian](../deploy/debian/README.md) |
| System services | ssh, systemd-networkd/resolved/timesyncd, smartmontools, cron, journald (persistent) | in use |
| File services | samba 4.22 (Debian, GPL-3.0; smbd, nmbd only with NetBIOS), configured only by the helper (`smb.json` → `smb.conf`) | installed 2026-11-26 ([install-samba.sh](../deploy/debian/install-samba.sh)), off until enabled in the UI |
| SSH/SFTP (W2.7) | openssh-server (Debian, BSD); the helper owns the drop-in `/etc/ssh/sshd_config.d/10-nas-revival.conf` rendered from `ssh.json` (port, password auth, `AllowGroups root <SFTP groups>`, `Match Group` → `internal-sftp` in chroot `/srv`), checked with `sshd -t` and applied with `systemctl reload ssh`. User keys (W2.7b): one root-owned 0644 file per NAS account in `/etc/ssh/nas-revival-keys/<user>` (`AuthorizedKeysFile`), keys parsed and rebuilt with `golang.org/x/crypto/ssh` (already a dependency); sshd reads the file at every login, no reload | in the Debian base system; managed since 2026-11-26 |
| NFS (W2.6) | nfs-kernel-server (Debian, GPL-2.0), NFSv4.1/4.2 only over TCP 2049, statd masked, rpcbind TCP 111 only (Proxmox online check); exports rendered by the helper (`nfs.json` → `/etc/exports.d/nas-revival.exports`) | installed 2026-11-26 ([install-nfs.sh](../deploy/debian/install-nfs.sh)), kernel with `CONFIG_NFSD=m` in slot A; off until enabled in the UI |
| Not installed | avahi, acl, quota | – |

Measured baseline is in [PERFORMANCE_BUDGET](PERFORMANCE_BUDGET.md). Short version: 51 MiB used
without page cache, web backend 9.4 MiB RSS idle (41 MiB peak after login, argon2), idle CPU ~99 %.

**Keep, don't rewrite.** The Go + React stack ([ADR 0002](adr/0002-web-ui-stack-debian.md)) meets
the plan's principles: static frontend rendered in the browser, one backend process, REST API,
native Linux tools, no runtime on the NAS besides one binary. Every module below extends it.

## 2. Process model

```mermaid
flowchart LR
  B[Browser: static React UI] -- HTTPS + cookie + CSRF header --> W
  subgraph NAS
    W[nas-revival web<br/>user nas-revival, sandboxed] -- Unix socket, JSON op --> H[nas-revival helper<br/>root, op allowlist]
    W -- read /proc /sys, smartctl -n standby --> K[(kernel)]
    H -- fixed argv --> T[systemctl, networkctl, smbpasswd,<br/>hdparm, smartctl, useradd, nft, apt-get]
    H -- atomic write --> C[(managed config files)]
    J[journald] <-- audit records --- H
    S[systemd timers] --> H
  end
```

- **Exactly two long-running NAS Revival processes:** the unprivileged web process and the root
  helper. No per-module daemons, no per-metric scripts. Optional services (Samba, NFS, Avahi…) are
  ordinary Debian packages started only when enabled.
- **Web process** (`nas-revival`): auth, sessions, authorization, input validation (first layer),
  the shared monitoring collector, read-only data from `/proc`, `/sys`, `smartctl --nocheck=standby`.
  Sandbox: `ProtectSystem=strict`, `NoNewPrivileges`, only `CAP_SYS_RAWIO` + group `disk` for SMART.
- **Helper** (`nas-revival helper`): the only place that changes the system. Accepts one JSON
  request per connection from uid `nas-revival` only (`SO_PEERCRED`), validates every field again,
  runs **fixed executables with argument arrays** (never a shell), writes only registered files
  atomically, audits every op. See [SECURITY_MODEL](SECURITY_MODEL.md).
- **Scheduled work** runs as systemd timers that call `nas-revival task run <id>` (one-shot, exits).
  No in-process scheduler; nothing runs when nothing is due.

## 3. Module map

Modules are Go packages inside the one binary (web side under `internal/<module>`, privileged ops
under `internal/helper/<module>`), plus one UI app per module.

| Module | Web side (read) | Helper ops (write) | Source of truth |
|---|---|---|---|
| System Core | info, capabilities, health | reboot, shutdown | `/proc`, `/etc/os-release`, profile |
| Config Manager | history, drift | snapshot, rollback, export, import | managed files (below) |
| HAL | capabilities, sensors | fan profile, LED role | device profile + sysfs |
| Monitoring | snapshot, ring buffer | – | `/proc`, `/sys` |
| Storage | disks, partitions, mounts, SMART | mount/unmount, fstab entry, SMART test, disk power | `lsblk`, `mountinfo`, `/etc/fstab`, `smartctl` |
| Users & Groups | list, membership | useradd/usermod/userdel, groups, passwords | `/etc/passwd`, `/etc/group`, Samba tdb |
| Shared Folders | list, permissions | create/delete share, ACL | `/etc/nas-revival/shares.json` → generated `smb.conf` include + `/etc/exports.d` |
| File Services | status, sessions | enable/disable, settings | `smb.conf`, `exports.d`, `sshd_config.d` |
| Service Manager | states (allowlist) | start/stop/restart/enable | systemd |
| Network | interfaces, routes | apply/confirm/rollback (ADR 0003) | `/etc/systemd/network/20-wired.network` |
| Scheduler | tasks, history | create/enable timers | `/etc/systemd/system/nas-revival-task-*.timer` |
| Backup | jobs, history | run rsync job, config backup | `/etc/nas-revival/backup.json` |
| Logs & Notifications | journal query, notifications | – | journald, `/var/lib/nas-revival/notifications.json` |
| Security | sessions, advisor findings | firewall apply/confirm | nftables `/etc/nftables.d/nas-revival.nft` |
| Updates | available updates | apt update/upgrade, self-update, kernel slot write | apt, release manifest |

## 4. Configuration Manager

**Principle: Linux files stay the source of truth.** NAS Revival keeps its own state only where no
Linux file exists (share definitions, disk power profiles, backup jobs, UI settings), as small JSON
documents in `/etc/nas-revival/` with a `schemaVersion`.

- **Managed file registry** (compiled into the helper): every file the helper may write, its owner
  module, validator and apply command. Example: `smb.conf.d/nas-revival.conf` → `testparm -s` →
  `systemctl reload smbd`. Anything not in the registry is read-only for NAS Revival.
- **Generated fragments instead of editing whole files** where the daemon supports includes
  (`smb.conf` include, `/etc/exports.d/`, `sshd_config.d/`, `nftables.d/`, `systemd/*.d/`), so the
  owner's manual edits elsewhere survive.
- **Transaction** = validate full proposed state → snapshot current files → atomic write
  (tmp + fsync + rename + fsync dir, `internal/atomicfile`) → validate with the daemon's checker →
  apply → health check → commit or rollback to snapshot. One transaction at a time (helper mutex).
- **History:** snapshots in `/var/lib/nas-revival-helper/config/<n>/` (tar of changed files +
  manifest with sha256, module, user, time), **bounded to 20** entries / 2 MiB.
- **Drift detection:** the manifest stores the sha256 of each managed file after our last write; a
  differing hash on read = "changed outside the web UI". The UI shows a diff summary and never
  overwrites silently (requires "take over" confirmation).
- **Export/import:** a versioned archive (JSON manifest + files), size and depth limits, allowlisted
  paths only. Secrets (password hashes, TLS key, API tokens) are excluded unless the export is
  encrypted with a user passphrase. Import = the same transaction path with migrations
  `vN → vN+1` per document.

## 5. Hardware Abstraction Layer

Profiles are trusted data compiled into the binary (`devices/<id>/profile.json`), selected by
`/proc/device-tree/compatible` (NSA310: `zyxel,nsa310a`) or by DMI on x86. See
[HARDWARE_COMPATIBILITY](HARDWARE_COMPATIBILITY.md).

| Profile | Selection | Capabilities |
|---|---|---|
| `zyxel-nsa310` | DT `zyxel,nsa310a` | 1 SATA + eSATA, 2 USB, LM85 (3 temps, fan1, pwm1 auto), 12 LEDs, COPY/RESET/POWER buttons, PCF8563 RTC; WoL/RTC wake unverified |
| `generic-arm-nas` | any DT without profile | discovered hwmon/LED read-only, no actuator writes |
| `generic-linux-nas` | x86/other | same as generic-arm, DMI model name |

`GET /system/capabilities` returns the profile and evidence state per feature
(`verified | unverified | unsupported`); the UI hides unsupported features and disables unverified
writes. Selectors are by driver name and label, never by hwmon index. Fan safety stays outside the
web process (LM85 hardware auto mode, `nsa310-fan` sets it at boot; on failure the fan stays at 100 %).

## 6. Monitoring

- One **collector goroutine** in the web process produces a snapshot; all clients read the same
  snapshot (no per-request `/proc` parsing). It runs at 5 s while at least one session polled within
  the last 30 s, and drops to 60 s (only health-relevant metrics) when nobody is watching.
- **Ring buffer in RAM**: 1 h at 10 s (360 samples × ~16 metrics, < 100 KiB). Long-term history:
  5-min aggregates for 7 days in RAM (~2 000 samples), flushed to `/var/lib/nas-revival/history.bin`
  at most once per hour and on shutdown (one small sequential write; skipped while the data disk sleeps
  if the state dir lives on it).
- Expensive sources run only on demand: process list (only while the Resource Monitor is open,
  ≥ 5 s), folder sizes, activity diagnostics, `df` on sleeping disks (never; cached value shown).
- SMART: `smartctl --nocheck=standby` with a 10 min cache (implemented); temperatures of a sleeping
  disk are shown as "sleeping", never woken.

## 7. Storage and disk power

- Disk identity = `/dev/disk/by-id` (WWN or model+serial), never `sdX`. Partitions by PARTUUID/UUID.
- **The system disk is protected:** the disk holding `/`, swap or the state directory is flagged
  `system: true`; unmount/format/delete ops refuse it in the helper (not only in the UI).
- Disk power settings live in `/etc/nas-revival/disks.json` (per disk id: standby timeout, APM) and
  are applied by `nas-revival task disk-power` at boot and on udev add events (hdparm `-S`, `-B`).
- **Known constraint on the NSA310 today:** root, swap, journal and data are on the same physical
  disk; the idle system writes about 0.2 MiB per 30 s (measured), so the disk cannot stay asleep.
  The UI must say so. Decision ([ADR 0007](adr/0007-disk-power-and-system-writes.md)): reduce
  writes first (`noatime`, batched journal), measure, root stays on the disk for now.

## 8. Users, permissions and authentication

- Linux users and groups are the identity. NAS users are uid ≥ 1000 in group `nas-users`; system
  accounts (uid < 1000, `root`, `nas-revival`) and critical groups are read-only in the UI.
- Web roles map to Linux groups: `nas-admin` (administrator), `nas-viewer` (read-only admin), any
  other NAS user = standard user (own password, later File Station).
- Passwords: one change updates the Linux shadow hash (`chpasswd` via stdin), the Samba hash
  (`smbpasswd -s`) and the web login hash (argon2id in `/etc/nas-revival/credentials.json`, keyed by
  Linux username). The web cannot verify yescrypt from `/etc/shadow` without cgo/PAM, so it keeps
  its own hash of the same password – one identity, not parallel virtual users ([ADR 0004](adr/0004-multi-user-auth-linux-users.md)).
- argon2id costs 16 MiB per verification: logins are serialized (semaphore of 1) and rate-limited
  before hashing.

## 9. Decisions to record

| ADR | Topic | Status |
|---|---|---|
| 0001 | Base distribution | Debian 13 chosen 2026-11-25; ADR text still the original evaluation (rewrite pending) |
| 0002 | Web stack on Debian | accepted |
| 0003 | Privileged helper (network/DNS/NTP) | accepted, in development |
| 0004 | Multi-user web auth tied to Linux users, roles via groups | accepted 2026-11-25 ([ADR](adr/0004-multi-user-auth-linux-users.md)) |
| 0005 | Config Manager: registry, fragments, snapshots, drift | proposed (§4) |
| 0006 | Scheduling via systemd timers + `nas-revival task` | proposed (§2) |
| 0007 | Disk power and system-disk write reduction | accepted 2026-11-25 ([ADR](adr/0007-disk-power-and-system-writes.md)) |

## 10. Build and deployment

- Workstation builds everything: `scripts/webui/build.sh` (Vite build → embedded → `GOARM=5`
  static binary, ~8 MiB). Node and Go run only on the workstation.
- Deployment today: copy binary, systemd units from `deploy/debian`, restart. Target (M5): signed
  release tarball with manifest, A/B binary directories (`/usr/local/lib/nas-revival/{a,b}`) and a
  symlink switch; see [RECOVERY](RECOVERY.md) and Update Manager in [ROADMAP](ROADMAP.md).
