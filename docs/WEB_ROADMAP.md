# Web administration roadmap

The web UI and its API, step by step. System-level milestones are in [ROADMAP](ROADMAP.md);
features and states in [FEATURE_MATRIX](FEATURE_MATRIX.md); screens and UI rules in
[UI_NAVIGATION](UI_NAVIGATION.md); API rules in [API_DESIGN](API_DESIGN.md).

Each step (W-n) is a small deliverable: backend endpoint(s) + helper op(s) + UI screen + tests +
deployment on the NAS + measurement. A step is done when every box is ticked and
`scripts/measure/snapshot.sh` shows no regression against [PERFORMANCE_BUDGET](PERFORMANCE_BUDGET.md).

**Definition of done for every step:** Go and frontend tests pass (one test fails before the change);
endpoint requires a session (and later a role); helper op validated + audited; Czech texts in
`i18n/cs.ts`; works on phone width 360 px and desktop; deployed and checked on the NAS with live
data; CHANGELOG, FEATURE_MATRIX state and this file updated in the same commit.

## Done

- [x] W0.1 Login, sessions, rate limit, CSRF, HTTPS (2026-11-25)
- [x] W0.2 Overview dashboard: health, resources, storage, SMART summary, network chart, services, events (2026-11-25)
- [x] W0.3 Launcher/sidebar shell, dark/light theme, polling only when visible (2026-11-25)
- [x] W0.4 Root helper + Settings: network (IP/DNS, 180 s confirm-or-rollback), NTP, TLS certificate (2026-11-25, deployed, helper rollback verified; UI flow in W1.0)

## W1 – Core & storage (system milestone M1)

| Step | Screen / UI | API | Helper ops | Matrix |
|---|---|---|---|---|
| W1.0 | Network/System settings live check: rollback in UI, confirm from new address | existing `/settings/*` | `network.*`, `time.*` | NET-02, NET-03, SEC-06 |
| W1.1 | – (infrastructure) | – | op registry with role + audit record per op; `job.status` | CFG-01, LOG-10 |
| W1.2 ✅ 2026-11-25 | Control panel → Info: versions, boot time, last update, board, capabilities | `GET /system/info`, `GET /system/capabilities` | – | CORE-10, CORE-11, HAL-10 |
| W1.3 ✅ 2026-11-25 | Storage app, Disks tab: stable id, model, serial (admin), FS, partitions, mounts, system disk badge, shared-disk warning | `GET /storage` | – | STOR-10..12, PWR-05 |
| W1.4 ✅ 2026-11-25 | Disk detail: SMART attributes with highlights, self-test start, progress %, history | `GET /storage/disks/{id}/smart`, `POST …/smart/test` | `smart.test` | SMART-10, SMART-11 |
| W1.5 ✅ 2026-11-25 | Disk detail → Power: state (active/standby), profile, standby timeout, APM, standby now | `GET/PUT /storage/disks/{id}/power`, `POST …/standby` | `disk.setPower`, `disk.standby`, boot task `disk-power` | PWR-01..03 (PWR-04 load-cycle growth warning moved to W3.6) |
| W1.6 ✅ 2026-11-25 | Quick actions: restart / shut down with preflight dialog and "going down" screen | `POST /system/{reboot,shutdown}/preview`, `POST /system/{reboot,shutdown}` | `power.reboot`, `power.shutdown` | CORE-12..14 |
| W1.7 ✅ 2026-11-25 | Overview: health with problem list + `checkedAt`, disk power state in storage widget | `GET /overview` (shared collector) | – | MON-01, UI-01 |
| W1.8 ✅ 2026-11-25 | Config history (Control panel → Configuration): snapshots, drift, rollback | `GET /config/history`, `POST /config/rollback/{n}` | `config.*` | CFG-02..04 |
| W1.9 ✅ 2026-11-26 | Storage → Activity (on demand): per-process disk writes, why the disk does not sleep | `GET /storage/activity` | `disk.activity` | DIAG-01, PWR-06 |

## W2 – NAS core services (M2)

| Step | Screen / UI | API | Helper ops | Matrix |
|---|---|---|---|---|
| W2.1 ✅ 2026-11-26 (deployed, verified via helper; logged-in browser check open) | Users & groups: list, create, edit, disable, delete, password, group membership | `/users`, `/groups` | `user.*`, `group.*` | USR-01..03 |
| W2.2 ✅ 2026-11-26 (tests + mock UI; NAS deployment tracked separately) | Roles admin / read-only / user, multi-account login, own password | session carries role | `auth.*` | USR-04 |
| W2.3 ✅ 2026-11-26 (deployed, e2e on NAS; SMB side — hidden, read-only, recycle, per-user enforcement — unverified until Samba is installed; NAS ext4 has no ACL support) | Shared folders: create on data volume, permissions matrix, read-only, hidden, recycle bin, delete (config vs. data) | `/shares` | `share.*` | SHARE-01..05 |
| W2.4 ✅ 2026-11-26 (deployed, e2e on NAS; browser checked with mock data on desktop only) | Services app: state, PID, RAM, CPU, ports, start/stop/restart/enable, unit log | `/services` | `service.*`, `journal.unit` | SVC-10..12 |
| W2.5 ✅ 2026-11-26 (deployed, e2e with a real SMB client from the workstation; page typechecked and unit-tested, not yet checked in a browser) | Services → SMB: enable, workgroup, protocol range, sessions, open files | `/services/smb` | `smb.*` | SMB-01, SMB-02 |
| W2.6 ✅ 2026-11-26 (deployed, e2e from Proxmox; page not yet checked in a browser) | Services → NFS: exports for Proxmox backups | `/services/nfs` | `nfs.*` | NFS-01, BKP-12 |
| W2.7 ✅ 2026-11-26 (deployed, e2e with a real SSH client on the NAS; page not yet checked in a browser; per-user keys → W2.7b) | Services → SSH/SFTP: port, password auth, root login (fixed: key only), SFTP-only groups in a chroot | `/services/ssh` | `ssh.*` | SSH-01, SSH-02 |
| W2.7b ✅ 2026-11-26 (deployed, e2e: key login with password off, refused outside SFTP groups, when disabled or removed; page not yet checked in a browser) | Services → SSH: public keys per NAS user (upload, list, remove) | `/services/ssh/keys` | `ssh.keys.*` | SSH-02 |
| W2.8 ✅ 2026-11-26 (deployed, e2e on the NAS; hostname change verified only by re-setting the current name; page checked in a browser only partly, mock) | Network: hostname, routes, diagnostics (ping, DNS, gateway, internet, ports) | `/network/*` | `net.diag` | NET-10, NET-11 |

## W3 – Monitoring & hardware (M3)

W3.1 ✅ 2026-11-26 (deployed, e2e on the NAS; page checked with mock data on desktop and 360 px, not yet logged in on the NAS; PSI hidden: kernel without CONFIG_PSI; 7-day aggregates of MON-02 open) Resource Monitor (CPU breakdown, RAM/PSI, disk and network I/O, 1 h history) ·
W3.2 ✅ 2026-11-26 (deployed, e2e on the NAS: list, protected processes refused, own sleep stopped; page checked with mock data on desktop and 360 px, not yet logged in on the NAS) Process list + protected kill · W3.3 ✅ 2026-11-26 (NAS: timer, run now, lock; phone width unchecked) Task scheduler (list, run now, history, SMART and cleanup
tasks) · W3.4 ✅ 2026-11-26 (deployed, e2e on the NAS: fan profile read back, night mode, fault LED; page checked with mock data on desktop, phone width and logged in on the NAS not yet; disk temperature watchdog open) Hardware: temperature history and thresholds, fan profiles, LEDs and night mode ·
W3.5 ✅ 2026-11-26 (deployed, e2e on the NAS without a real restart: non-persistent timer, run now refused, preflight codes; page checked with mock data on desktop and 360 px; a scheduled restart on the NAS not yet observed) Power schedule: task types `reboot` and `shutdown` on the W3.3 scheduler, preflight wait up to 1 h then skipped, no catch-up, not within 15 min of boot · W3.6 ✅ 2026-11-26 (deployed, history file and counters checked on the NAS; endpoint and health warning covered by tests; page checked with mock data on desktop, phone width and logged in on the NAS not yet; first growth rate after 20 h) Standby history and load-cycle growth warning (PWR-04).

## W4 – Data management (M4)

W4.1 File Station (phase 1 ✅ 2026-11-26, deployed, tests on the NAS by the owner; ADR 0009: browse, paged list, folder ops, streamed upload/download with progress, delete to the bin; copy jobs and cancel → FS-04) ·
W4.2 USB storage (phase 1 ✅ 2026-11-26, deployed, tests on the NAS by the owner; ADR 0010: list, mount ro/rw via systemd-mount, unmount, safe eject, share via /srv/usb; open: automount, backup target STOR-USB-03, exFAT/NTFS kernel) · W4.3 Backup jobs (phase 1 ✅ 2026-11-26 on the NAS: pull over SSH, key, host key, test, read-only share; phase 2a ✅ 2026-11-26 on the NAS: dashboard card and health problems, versions with sizes, browse, single-file download through a passed descriptor; phase 2b ✅ 2026-11-26 (deployed, e2e on the NAS restores one file per job into a throw-away share; folder restore and the dialog not yet in a browser on the NAS): restore of a folder or file into a share in the background with progress and cancel; failure notifications via W4.5; ADR 0008: task type `backup-pull` on the W3.3 scheduler core; rsync, dry run, history,
restore of files) · W4.4 Config export/import ✅ 2026-11-27 (deployed, e2e on the NAS: export without secrets, own export changes nothing, foreign files refused, NTP change imported and the original imported back; dialog checked only with tests and the mock build, not yet in a browser; ADR 0011; tasks, backup jobs and hardware settings not in the export yet) · W4.5 Notification centre ✅ 2026-11-27 (phase 1, ADR 0012: entries from health problem transitions – backups, services, SMART, temperatures, volumes, fan, disk wear; 1 h dedup, per-user read state, bell with unread count in the header; open: e-mail/webhook NOTIF-02, non-health events).

## W5 – Hardening (M5)

W5.1 Security Center (phase 1 ✅ 2026-11-27, ADR 0013: session list with forced logout, sessions survive restarts, advisor with SSH/SMB/NFS/certificate/account/login checks and fix links; open: security updates and unexpected ports in the advisor) · W5.2 Firewall with rollback (✅ 2026-11-27, ADR 0014: allowed sources per service, anti-lockout, 180 s confirm-or-rollback, boot unit; needs kernel 6.12.119-nr1) ·
W5.3 Log Center (✅ 2026-11-27, ADR 0015: journal query by category/severity/time/text with export, audit view of administrative actions from structured journal fields, retention size/age) · W5.4 Update Manager (Debian,
NAS Revival self-update, kernel slot update) · W5.5 English translation.

## W6 – Expansion (M6)

W6.1 API tokens + integration status endpoint · W6.2 Home Assistant · W6.3 Package Center.
