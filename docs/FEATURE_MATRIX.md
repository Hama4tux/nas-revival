# Feature matrix

Status as of 2026-11-25 (audit of the repository and the running NAS). One row = one feature.
Rows are split into **already implemented** (§1) and **planned** (§2). Update the state in the same
commit as the change.

**States:** Not started · Planned · In development · Implemented · Tested · Blocked · Unsupported by hardware.
*Implemented* = code exists and works; *Tested* = automated tests pass **and** it was verified on the NAS.
*Planned* = designed in [ARCHITECTURE](ARCHITECTURE.md) with a milestone in [ROADMAP](ROADMAP.md).

**Resource cost (Res):** L = negligible (< 1 MiB RAM, no idle CPU, no periodic writes) ·
M = noticeable while used (subprocess, parsing, short spikes) · H = sustained cost or optional daemon
(must be off by default or on demand). Numbers in [PERFORMANCE_BUDGET](PERFORMANCE_BUDGET.md).

**HW:** hardware requirement; `—` = none beyond the base board. **Deps:** other modules (IDs).

## 1. Already implemented

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| BOOT-01 | Platform | Unattended boot slot A → slot B → stock firmware | P0 | Tested | NAND, stock U-Boot | own kernel 6.12 | L | bad kernel needs UART | cold start boots Debian; invalid slot A falls through to B (verified 2026-11-25) | — |
| BOOT-02 | Platform | Kernel write to NAND slot from Linux | P0 | Tested | NAND | mtd-utils | L | wrong slot/offset bricks boot | readback sha256 + 0 ECC errors (verified, manual only) | — |
| CORE-01 | System Core | Host info: hostname, OS, kernel, uptime, board model | P0 | Tested | — | /proc, os-release | L | none | values match `uname`, `/etc/os-release` | — |
| CORE-02 | System Core | CPU model, cores, load 1/5/15, usage % | P0 | Tested | — | /proc | L | none | usage within ±5 % of `vmstat` | — |
| CORE-03 | System Core | RAM, MemAvailable, swap | P0 | Tested | — | /proc/meminfo | L | none | matches `free -b` | — |
| HAL-01 | HAL | Temperatures and fan RPM from hwmon (LM85) by driver name | P0 | Tested | LM85 | hwmon | L | none | missing sensor = null, not 0; only wired fan shown | — |
| HAL-02 | HAL | Fan automatic mode via LM85 zones (`nsa310-fan`) | P0 | Tested | LM85 | systemd unit | L | overheating if misconfigured | on failure fan stays 100 %; fan never stops | — |
| HAL-03 | HAL | LED triggers at boot (`nsa310-leds`) | P1 | Implemented | LEDs | led class | L | none | SYS green after boot; HDD/USB/LAN activity | — |
| HAL-04 | HAL | COPY button → USB stick backup to `/srv/data/usb-zalohy` | P1 | Implemented | COPY button, USB | evdev, rsync | M | backup of untrusted media | read-only mount, rescue stick skipped, LED feedback; real FAT32 stick test open | STOR-USB |
| NET-01 | Network | Interfaces: state, speed, addresses, RX/TX bytes | P0 | Tested | — | /sys/class/net | L | none | matches `ip -s link` | — |
| STOR-01 | Storage | Disk list (model, size, rotational, removable) | P0 | Tested | — | /sys/block | L | serial disclosure | no `sdX`-only identity in new code (see STOR-10) | — |
| STOR-02 | Storage | Filesystem usage per mount | P0 | Tested | — | statfs | L | none | matches `df -B1` | — |
| SMART-01 | SMART | SMART health summary, cached 10 min, `--nocheck=standby` | P0 | Tested | SATA SMART | smartmontools | L | none | does not wake a sleeping disk | — |
| SVC-01 | Services | Fixed allowlist of service states (systemctl show) | P0 | Tested | — | systemd | L | info disclosure only | unknown units = `not installed`; oneshot "done" | — |
| UI-01 | UI | Dashboard: health summary, resources, storage, network chart, services, events | P0 | Implemented | — | React static | L | XSS via data (React escapes) | loads < 3 s on LAN; mobile 390 px checked with mock only | CORE-*, STOR-* |
| UI-02 | UI | Polling every 5 s only while page visible | P0 | Tested | — | Page Visibility API | L | none | no requests when tab hidden | — |
| UI-03 | UI | Dark/light theme | P1 | Implemented | — | — | L | none | toggle persists per browser | — |
| UI-04 | UI | Launcher and sidebar navigation | P0 | Implemented | — | — | L | none | unready apps disabled | — |
| SEC-01 | Security | HTTPS with self-signed certificate generated on device | P0 | Tested | — | Go crypto/tls | L | MITM on first use | TLS ≥ 1.2, key 0600 | — |
| SEC-02 | Security | Admin login argon2id, sessions (30 min idle / 12 h), logout | P0 | Tested | — | x/crypto | M (16 MiB per login) | brute force, session theft | HttpOnly/Secure/SameSite=Strict; 401 without session | — |
| SEC-03 | Security | Login rate limit 5 / 5 min per client | P0 | Tested | — | — | L | DoS by lockout | 429 + Retry-After | SEC-02 |
| SEC-04 | Security | CSRF header + security headers (CSP, nosniff, frame deny) | P0 | Tested | — | — | L | cross-site writes | non-GET without header = 403 | SEC-02 |
| SEC-05 | Privilege | Web runs unprivileged in systemd sandbox | P0 | Tested | — | systemd | L | sandbox escape | runs as `nas-revival`, only CAP_SYS_RAWIO | — |
| PRIV-01 | Privilege | Root helper on Unix socket, peer uid check, typed ops (ADR 0003) | P0 | Tested | — | — | L | root code reachable from web | only uid nas-revival accepted; unknown op rejected; fuzzed validators | SEC-05 |
| NET-02 | Network | IP mode / static address / gateway / DNS with 180 s confirm-or-rollback | P1 | Implemented | — | systemd-networkd | L | admin lockout | unconfirmed change reverts, also after reboot | PRIV-01 |
| NET-03 | Network | NTP servers | P1 | Implemented | — | timesyncd | L | wrong time breaks TLS | restart timesyncd, status shown | PRIV-01 |
| SEC-06 | Security | TLS certificate upload / regenerate without restart | P1 | Implemented | — | — | L | key leak | key never returned by API | SEC-01 |

## 2. Planned

### System Core and Configuration (M1)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| CORE-10 | System Core | NAS Revival version, Debian version, arch, boot time, last update time | P0 | Planned | — | dpkg log, build info | L | none | values match `dpkg`, `/proc/stat btime` | — |
| CORE-11 | System Core | Hardware capability detection endpoint | P0 | Planned | — | profile | L | none | UI hides unsupported features | HAL-10 |
| CORE-12 | System Core | Reboot and shutdown with confirmation | P0 | Planned | — | systemctl | L | DoS by authorized user | typed confirmation; audit entry; `sync` before | PRIV-01 |
| CORE-13 | System Core | Preflight before power ops: running SMART test, backup, apt, copy | P0 | Planned | — | flock files | L | data loss | blocked with reason list; "force" needs second confirmation | CORE-12, TASK-* |
| CORE-14 | System Core | Shutdown progress / reason page (UI keeps polling until gone) | P1 | Planned | — | — | L | none | UI shows "shutting down" and "offline" states | CORE-12 |
| CFG-01 | Config | Managed file registry in helper | P0 | Planned | — | — | L | arbitrary file write | write to unregistered path impossible (test) | PRIV-01 |
| CFG-02 | Config | Atomic write + snapshot + rollback transaction | P0 | Planned | — | atomicfile | L | half-applied config | killed helper mid-write leaves old or new file, never partial | CFG-01 |
| CFG-03 | Config | Bounded history (20 snapshots / 2 MiB) | P0 | Planned | — | — | L | disk fill | 21st snapshot evicts oldest | CFG-02 |
| CFG-04 | Config | Drift detection (sha256 manifest) | P1 | Planned | — | — | L | silent overwrite of manual edits | manual edit shown as drift; save requires takeover | CFG-02 |
| CFG-05 | Config | Export / import archive with schema migration | P1 | Planned | — | — | M | malicious archive, secret leak | size/depth limits; secrets only in encrypted export | CFG-02 |
| HAL-10 | HAL | Device profiles: NSA310, generic ARM, generic Linux | P0 | Planned | — | DT/DMI | L | wrong profile → wrong actuator | unknown board = read-only generic profile | — |

### Storage (M1)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| STOR-10 | Storage | Stable disk identity (by-id/WWN), vendor, serial, firmware, interface, type HDD/SSD/SSHD/flash | P0 | Planned | — | lsblk -J, smartctl -i | L | serial shown to admins only | identity survives `sda`↔`sdb` swap (USB re-plug test) | STOR-01 |
| STOR-11 | Storage | Partitions, UUID/PARTUUID, FS type, mountpoints, bind/duplicate mounts | P0 | Planned | — | /proc/self/mountinfo | L | none | bind mount of /srv/data flagged, not double counted | STOR-10 |
| STOR-12 | Storage | System disk detection and protection | P0 | Planned | — | — | L | system wipe | helper refuses unmount/format of disk with `/`, swap or state dir | STOR-11, PRIV-01 |
| STOR-13 | Storage | Disk → partition → share dependency tree | P1 | Planned | — | — | L | none | unmount lists dependent shares and blocks | STOR-11, SHARE-01 |
| STOR-14 | Storage | Mount / unmount / read-only mount / automount (fstab `nofail`) | P1 | Planned | — | mount, fstab | L | data loss, boot hang | fstab entries by UUID with `nofail`; busy unmount reports holders | STOR-12, CFG-02 |
| STOR-15 | Storage | Create partition / format (ext4 default) | P2 | Not started | — | parted, mkfs | M | data destruction | typed disk id + capacity confirmation; never system disk; read-only overview first | STOR-12 |
| STOR-16 | Storage | exFAT / NTFS (ntfs3) support | P2 | Blocked | — | kernel modules | L | FS corruption on NTFS | needs new kernel with `exfat`, `ntfs3` | UPD-04 |
| SMART-10 | SMART | Attribute table with vendor-aware highlights (5, 187, 188, 197, 198, 193, 9, 4, 12, 194) | P0 | Planned | SMART | smartmontools | L | none | raw values parsed per vendor (Seagate 48-bit packed) | SMART-01 |
| SMART-11 | SMART | Self-test short/long: start, progress %, last result | P0 | Planned | SMART | smartctl -t/-c | M (disk busy) | DoS by repeated tests | one test per disk; progress shown; result in history | PRIV-01 |
| SMART-12 | SMART | Scheduled tests via timers | P1 | Planned | SMART | systemd timers | L | conflicts with backup | not concurrent with backup (lock) | TASK-01 |
| SMART-13 | SMART | History of results and temperature, deterioration alerts | P1 | Planned | SMART | — | L | none | increase of 5/197/198 raises warning notification | LOG-10 |
| PWR-01 | Disk power | Current power state (active/idle/standby) without waking | P0 | Planned | ATA | hdparm -C | L | none | query never spins the disk up (test with standby disk) | PRIV-01 |
| PWR-02 | Disk power | Standby timeout, APM level, profiles (performance/balanced/eco/custom) | P0 | Planned | ATA APM | hdparm -S/-B | L | wear from frequent spin-ups | settings persist after reboot and re-plug; unsupported APM hidden | CFG-02 |
| PWR-03 | Disk power | Manual standby / wake | P1 | Planned | ATA | hdparm -y | L | I/O stall | refused while I/O-heavy task holds lock | PWR-01 |
| PWR-04 | Disk power | Start/stop and load-cycle counters, frequent wake warning | P1 | Planned | SMART | — | L | none | warns when load cycles grow > N/day (attr 193 currently 35 003) | SMART-10 |
| PWR-05 | Disk power | Warning when system/swap/logs share the data disk | P0 | Planned | — | — | L | none | shown on NSA310 today | STOR-12 |
| PWR-06 | Disk power | Reduce system writes (journal mode, noatime, commit) | P1 | Planned | — | journald, fstab | L | lost logs on crash | idle writes measured before/after; owner decision | CFG-02 |
| DIAG-01 | Diagnostics | Current disk activity and per-process I/O (on demand) | P1 | Planned | — | /proc/diskstats, /proc/PID/io | M | info disclosure (process names) | 10 s sampling only while open | PRIV-01 |
| DIAG-02 | Diagnostics | Standby/active transition history, "why not asleep" | P2 | Planned | ATA | hdparm -C sampling | L | none | unknown reason shown as unknown, never guessed | PWR-01 |
| STOR-USB-01 | USB storage | Detect USB disks, info, capacity | P1 | Planned | USB | udev, lsblk | L | malicious FS | listed with stable id | STOR-10 |
| STOR-USB-02 | USB storage | Mount, safe eject, automount, share USB | P1 | Planned | USB | udisks-free (mount, hdparm -y, sysfs delete) | L | data loss on pull | eject flushes, unmounts and powers down port device | STOR-14 |
| STOR-USB-03 | USB storage | Backup destination and transfer state | P2 | Planned | USB | rsync | M | — | used by BKP-10 | BKP-10 |
| RAID-01 | Extended storage | mdadm RAID status, rebuild, replacement | P3 | Unsupported by hardware | ≥ 2 bays | mdadm | M | data loss | only on multi-bay profiles | HAL-10 |

### Users, shares, file services, services (M2)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| USR-01 | Users | List, create, edit, disable, delete Linux NAS users | P0 | Not started | — | shadow utils | L | privilege escalation | uid < 1000 and root immutable; delete keeps home unless confirmed | PRIV-01, CFG-02 |
| USR-02 | Users | Password change syncs shadow, Samba and web hash | P0 | Not started | — | chpasswd, smbpasswd | M | password in logs/argv | password only via stdin; never logged | USR-01 |
| USR-03 | Users | Groups, membership, protected system groups | P0 | Not started | — | groupadd | L | adding user to `sudo`/`disk` | only `nas-*` and user-created groups editable | USR-01 |
| USR-04 | Users | Web roles admin / read-only admin / user (ADR 0004) | P0 | Not started | — | — | L | role bypass | every endpoint has role check; test matrix per role | USR-03, SEC-02 |
| USR-05 | Users | Per-service access (SMB, SFTP, NFS-by-host) | P1 | Not started | — | — | L | unintended access | user without SMB right cannot log into SMB | USR-03 |
| USR-06 | Users | Effective permission view per user/share | P1 | Not started | — | getfacl | L | none | matches actual access test | SHARE-02 |
| USR-07 | Users | Home directories (optional) | P2 | Not started | — | — | L | — | `homes` share off by default | SHARE-01 |
| USR-08 | Users | Disk quotas | P3 | Not started | — | quota tools | M | — | ext4 quota feature needed | STOR-14 |
| SHARE-01 | Shares | Create/edit share on a data volume (name, description, path, protocols) | P0 | Not started | — | — | L | share on unmounted path writes to root FS | refuse path whose mount is not the data FS | STOR-13 |
| SHARE-02 | Shares | User/group RW/RO/none permissions → POSIX + ACL | P0 | Not started | — | acl | L | permission conflicts | conflicts listed; ACL only if FS supports | USR-03 |
| SHARE-03 | Shares | Hidden share, read-only share | P1 | Not started | — | samba | L | — | `browseable = no`, `read only = yes` | SMB-01 |
| SHARE-04 | Shares | SMB recycle bin with automatic cleanup | P1 | Not started | — | vfs_recycle, timer | L | disk fill | cleanup older than N days via timer | TASK-01 |
| SHARE-05 | Shares | Delete share: config only vs. config + data | P0 | Not started | — | — | L | data loss | data deletion requires typing share name | SHARE-01 |
| SHARE-06 | Shares | Share size on demand | P2 | Not started | — | du | M (disk scan) | — | cancellable, never automatic | — |
| SMB-01 | SMB | Enable/disable, workgroup, description, SMB2–3 only, SMB1 off | P0 | Not started | — | samba (~20–30 MiB RSS est.) | H (daemon when on) | SMB1, guest access | `testparm` before apply; min protocol SMB2_10 | SVC-10, SHARE-01 |
| SMB-02 | SMB | Active sessions and open files | P1 | Not started | — | smbstatus --json | M | info disclosure | admin only | SMB-01 |
| SMB-03 | SMB | macOS options (fruit), Time Machine | P2 | Not started | — | vfs_fruit | L | — | Time Machine later | SMB-01 |
| NFS-01 | NFS | Exports: path, clients (IP/subnet), ro/rw, root_squash | P1 | Not started | — | nfs-kernel-server | M | export to world | no `*` clients; root_squash default; `exportfs -ra` validated | SHARE-01 |
| SSH-01 | SSH/SFTP | Enable, port, password auth, root login restriction, allowed groups | P1 | Not started | — | openssh | L | lockout of admin | `sshd -t` before reload; current session kept | CFG-02 |
| SSH-02 | SSH/SFTP | Public keys per user, SFTP-only, chroot | P2 | Not started | — | openssh | L | chroot escape | chroot dir root-owned | USR-01 |
| FTP-01 | FTP | Legacy FTP/FTPS (off by default) | P3 | Not started | — | vsftpd | M | cleartext credentials | anonymous off; Security Advisor warns | SVC-10 |
| DISC-01 | Discovery | mDNS (Avahi or resolved mDNS) | P2 | Not started | — | systemd-resolved mDNS preferred | L | info disclosure | NAS visible in macOS/Linux | NET-10 |
| DISC-02 | Discovery | WS-Discovery for Windows | P2 | Not started | — | wsdd2 | L | — | NAS in Windows Network | SMB-01 |
| SVC-10 | Services | Service manager: state, PID, RAM, CPU, ports, last error | P0 | Planned | — | systemctl show, /proc | L | info disclosure | data for allowlisted units only | SVC-01 |
| SVC-11 | Services | Start/stop/restart/reload/enable/disable for allowlisted units | P0 | Planned | — | systemctl | L | stopping critical service | ssh/networkd/nas-revival/helper/fan protected | PRIV-01 |
| SVC-12 | Services | Unit log tail | P1 | Planned | — | journalctl -u (fixed args) | M | secrets in logs | bounded to 200 lines | LOG-01 |
| NET-10 | Network | Hostname, domain, routes view, MTU, duplex, MAC | P1 | Not started | — | networkctl, ip -j | L | — | matches `ip -j` | NET-02 |
| NET-11 | Network | Diagnostics: ping, DNS lookup, gateway, internet, listening ports, connections | P1 | Not started | — | ping, ss | M | used as scanner | fixed targets only gateway/user host, count ≤ 4, rate-limited | PRIV-01 |
| NET-12 | Network | IPv6 | P2 | Not started | — | — | L | — | — | NET-02 |
| NET-13 | Network | VLAN, static routes, bonding | P3 | Not started | 1 NIC | — | L | — | only on demand | NET-02 |

### Monitoring, scheduler, hardware (M3)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| MON-01 | Monitoring | Shared collector snapshot for all clients | P0 | Planned | — | — | L | — | 5 clients = same CPU as 1 (measured) | — |
| MON-02 | Monitoring | RAM ring buffer 1 h, 5-min aggregates 7 days, hourly flush | P1 | Planned | — | — | L | — | RSS growth < 1 MiB over 7 days | MON-01 |
| MON-03 | Monitoring | CPU user/system/idle/iowait, swap in/out, memory pressure (PSI) | P1 | Planned | — | /proc/stat, /proc/vmstat, /proc/pressure | L | — | PSI shown if kernel exposes it | MON-01 |
| MON-04 | Monitoring | Disk throughput, IOPS, busy %, latency | P1 | Planned | — | /proc/diskstats | L | — | does not wake disk (diskstats only) | MON-01 |
| MON-05 | Monitoring | Network errors, totals, history | P1 | Planned | — | /sys/class/net stats | L | — | — | MON-01 |
| MON-06 | Monitoring | Process list, sort by CPU/RSS, kill (admin, protected PIDs) | P1 | Planned | — | /proc | M (while open) | killing critical process | PID 1, sshd, helper, journald protected | PRIV-01 |
| TASK-01 | Scheduler | Task list (name, type, enabled, interval, last/next run, result, duration, history) | P1 | Planned | — | systemd timers | L | — | matches `systemctl list-timers` | PRIV-01 |
| TASK-02 | Scheduler | Built-in task types (SMART, backup, cleanup, restart, power) | P1 | Planned | — | — | L | arbitrary commands | no free-form command type | TASK-01 |
| TASK-03 | Scheduler | Run now / stop, heavy-task lock | P1 | Planned | — | flock | L | — | SMART long and backup never overlap | TASK-01 |
| TASK-04 | Scheduler | Custom scripts for trusted admin | P3 | Not started | — | — | L | code execution | separate ADR; off by default | TASK-01 |
| HW-01 | Hardware | Temperature history, thresholds, critical events | P1 | Planned | LM85 | — | L | — | event at threshold, deduplicated | MON-02, LOG-10 |
| HW-02 | Hardware | Fan profiles quiet/balanced/performance mapped to LM85 zone limits | P1 | Planned | LM85 PWM auto | sysfs | L | overheating | only verified ranges; disk temp watchdog → 100 % | HAL-10 |
| HW-03 | Hardware | Custom fan curve | P2 | Planned | LM85 | — | L | overheating | min PWM floor enforced in helper | HW-02 |
| HW-04 | Hardware | LED settings, night mode, critical error indication | P2 | Planned | LEDs | led class | L | — | night mode keeps red fault LED | HAL-10 |
| HW-05 | Hardware | Scheduled shutdown / reboot | P1 | Planned | — | timers | L | — | preflight applies | CORE-13, TASK-01 |
| HW-06 | Hardware | Scheduled power-on (RTC alarm) | P2 | Blocked | PCF8563 alarm wired to power? | rtc | L | — | blocked until wake path verified on board | HAL-10 |
| HW-07 | Hardware | Wake-on-LAN | P2 | Blocked | RTL8111D WoL + PSU standby | ethtool | L | — | blocked until verified on board | HAL-10 |
| HW-08 | Hardware | Power-loss behavior (auto power-on) | P2 | Unsupported by hardware | — | — | L | — | NAS stays off after power loss (observed 2026-11-18) | — |
| HW-09 | Hardware | UPS (NUT) | P3 | Not started | USB UPS | nut | M | — | shutdown at configurable battery level | SVC-10 |

### Data management (M4)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| FS-01 | File Station | Browse shares, tree, paged list | P2 | Not started | — | — | L | path traversal, symlink escape | `openat2(RESOLVE_BENEATH)` per share root; tests with `..` and symlinks | USR-04, SHARE-01 |
| FS-02 | File Station | Create folder, rename, copy, move, delete, multi-select | P2 | Not started | — | — | M | data loss | operations as the logged-in user's uid (helper setfsuid) | FS-01 |
| FS-03 | File Station | Streamed upload/download | P2 | Not started | — | — | M | memory exhaustion | 2 GiB upload with RSS growth < 4 MiB | FS-01 |
| FS-04 | File Station | Operation progress, cancel, max 2 concurrent ops | P2 | Not started | — | — | M | — | queue bounded | FS-02 |
| FS-05 | File Station | Name search (on demand), permissions edit, ZIP optional | P3 | Not started | — | — | H | — | no indexing, no thumbnails | FS-01 |
| BKP-01 | Backup | Config backup/export, history, validation, restore | P1 | Planned | — | — | L | secrets in export | restore goes through CFG transaction | CFG-05 |
| BKP-10 | Backup | rsync jobs: USB, other NAS, SSH server; incremental, excludes, bwlimit, dry run | P1 | Not started | — | rsync, ssh | M | deleting target data | no `--delete` unless mirror mode confirmed separately | TASK-01 |
| BKP-11 | Backup | Progress, history, success check, restore of selected files | P1 | Not started | — | — | M | — | last result on dashboard | BKP-10 |
| BKP-12 | Backup | Proxmox vzdump target via NFS | P1 | Not started | — | nfs | M | — | NFS export only to Proxmox host, root_squash | NFS-01 |
| DR-01 | Disaster recovery | Export HW profile, package list, system configs; integrity check | P1 | Not started | — | dpkg | L | — | documented in RECOVERY | BKP-01 |
| NOTIF-01 | Notifications | In-UI notification center, read state, dedup | P1 | Not started | — | — | L | — | same event within 1 h = one entry | LOG-10 |
| NOTIF-02 | Notifications | SMTP, webhook, Home Assistant channels | P2 | Not started | — | Go net/smtp | L | credential storage | secrets 0600, test button | NOTIF-01 |

### Production hardening (M5)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| SEC-10 | Security | Session list, forced logout | P1 | Not started | — | — | L | — | revoked session gets 401 immediately | SEC-02 |
| SEC-11 | Security | TOTP | P3 | Not started | — | — | L | — | RFC 6238 | SEC-02 |
| SEC-12 | Firewall | nftables rules: allowed networks per service, anti-lockout, confirm-or-rollback | P1 | Not started | — | nftables (installed) | L | admin lockout | current client net always allowed; rollback after 180 s | PRIV-01 |
| SEC-13 | Security Advisor | Checks: SMB1, FTP, root SSH, open shares, cert expiry, security updates, unusual ports | P1 | Not started | — | — | L | — | each finding has fix link | SMB-01, UPD-01 |
| LOG-01 | Log center | journald query by category/severity/time/text, export | P1 | Not started | — | journalctl -o json (fixed args) | M | log injection, secrets | max 1 000 lines per request | — |
| LOG-02 | Log center | Retention and size limits (journald SystemMaxUse) | P1 | Not started | — | journald | L | disk fill | journal ≤ 64 MiB | CFG-02 |
| LOG-10 | Audit | Audit log: who, when, object, change, result | P0 | Planned | — | journald structured fields | L | tampering | every helper op produces one audit record | PRIV-01 |
| UPD-01 | Updates | Debian updates: check, list, security marked, apply, reboot needed | P1 | Not started | — | apt-get (fixed args) | H (apt RAM spike) | broken system | free space and RAM checked; no major upgrade | PRIV-01 |
| UPD-02 | Updates | NAS Revival self-update: signed manifest, A/B binary, rollback | P1 | Not started | — | ed25519 (Go stdlib) | L | malicious update | unsigned/foreign-arch artifact rejected before writes | CFG-02 |
| UPD-03 | Updates | Config backup before update, history | P1 | Not started | — | — | L | — | — | BKP-01 |
| UPD-04 | Updates | Kernel update to NAND slots (A→B copy, write A, verify, boot-once) | P1 | Not started | NAND | mtd-utils | M | unbootable device | never both slots; refuses unless booted from A (`nsa_slot=A`) | BOOT-02 |

### Platform expansion (M6)

| ID | Module | Feature | P | State | HW | SW deps | Res | Security risk | Acceptance criteria | Deps |
|---|---|---|---|---|---|---|---|---|---|---|
| API-10 | Integration API | API tokens with scopes, expiry, revocation, rate limit, audit | P2 | Not started | — | — | L | token leak | default token read-only | SEC-02 |
| API-11 | Integration API | Read-only status endpoint for HA/monitoring that never wakes disks | P2 | Not started | — | — | L | — | served from collector snapshot | MON-01 |
| PKG-01 | Package Center | Package manifests (arch, RAM, ports, services), install/remove | P3 | Not started | — | apt | M | supply chain | packages too heavy for 256 MiB not offered | UPD-01 |
| I18N-01 | UI | English translation | P1 | Not started | — | — | L | — | language switch; no hard-coded Czech in components | UI-01 |
| DEV-01 | Platform | Second device profile | P3 | Not started | other device | — | — | — | API reuse without board-specific code in modules | HAL-10 |
