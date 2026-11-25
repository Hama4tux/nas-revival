# Roadmap

Authoritative plan since 2026-11-25 (replaces the OpenWrt-era `roadmap.md`; that history is in git
and in [research/](research/)). Open questions and blockers live only in [OPEN.md](../OPEN.md).
Feature states: [FEATURE_MATRIX](FEATURE_MATRIX.md). Web UI steps: [WEB_ROADMAP](WEB_ROADMAP.md). Gates: [TEST_PLAN](TEST_PLAN.md) and
[PERFORMANCE_BUDGET](PERFORMANCE_BUDGET.md).

**Goal:** a stable, safe, fast and really usable NAS system for hardware with 240 MiB RAM – not the
largest feature count. Every milestone ends with measurements on the NAS, not workstation numbers.

## Where we are

Done before this plan (Debian phase, 2026-11-25): own kernel 6.12 in two NAND slots with stock
fallback, Debian 13 armel on the internal disk, LM85 fan automatic mode, LEDs, COPY button backup,
read-only web UI (overview, SMART) as one Go binary + static React; root helper with network/DNS/NTP/TLS settings ([ADR 0003](adr/0003-privileged-helper-settings.md)) – taken over and deployed 2026-11-25.

| Milestone | Content | State |
|---|---|---|
| M0 Architecture & audit | inventory, measurements, these documents | **done 2026-11-25** |
| M1 Core & storage | System Core, Config Manager, HAL profiles, privilege separation, dashboard, storage, SMART, disk power | next |
| M2 NAS core services | users, groups, shares, service manager, Samba, SSH/SFTP, basic network | planned |
| M3 Monitoring & hardware | resource monitor, scheduler, temperatures, fan profiles, LEDs, power schedule, activity diagnostics | planned |
| M4 Data management | File Station, USB storage, backup & restore, notifications | planned |
| M5 Production hardening | Security Center, firewall, audit, update manager (Debian, self, kernel), recovery workflow, long-run stability | planned |
| M6 Platform expansion | package center, integration API, Home Assistant, second device | planned |

Order deviation from the web plan, on purpose: **network/DNS/NTP settings (M2 item) are already in
development** and will be finished first, because they are the first user of the helper and the
config transaction that all of M1 builds on.

## M1 backlog – Core & Storage

Rules: one item = one or a few commits, each with a test that fails before and passes after
(CLAUDE.md rule 4). Helper ops follow ADR 0003 (typed JSON, fixed argv, registry). Every item ends
with the gate in the last column; measurements with `scripts/measure/snapshot.sh`.

| # | Item | Matrix IDs | Scope | Acceptance gate |
|---|---|---|---|---|
| M1-00 | Finish and deploy helper (network/DNS/NTP/TLS) | PRIV-01, NET-02/03, SEC-06 | **deployed 2026-11-25**; helper-level rollback verified | remaining: UI flow with live login, rollback after reboot during pending change |
| M1-01 | Measurement script and baseline | — | `scripts/measure/snapshot.sh` (read-only) | **done in M0**; baseline in PERFORMANCE_BUDGET |
| M1-02 | Helper op registry + audit records | CFG-01, LOG-10 | `internal/helper`: op table with role, validator, audit fields; journald audit with `NAS_REVIVAL_AUDIT=1` | unknown op → error; each op = one audit record; fuzz tests for validators |
| M1-03 | Config transaction, snapshots, drift | CFG-02, CFG-03, CFG-04 | generalize `atomicfile` + network backup into `internal/config` | kill -9 during apply leaves consistent files; drift shown after manual edit |
| M1-04 | Device profiles + capabilities API | HAL-10, CORE-11 | **done 2026-11-25** (profiles in `internal/system/profile.go`); `GET /system/capabilities` | NSA310 detected by DT; unknown DT → generic read-only; UI hides unsupported |
| M1-05 | System info | CORE-10 | **done 2026-11-25**; `GET /system/info` (version, Debian, arch, boot time, last update) | values match commands on NAS |
| M1-06 | Reboot / shutdown with preflight | CORE-12, CORE-13, CORE-14 | **implemented 2026-11-25 (W1.6), live reboot test pending**; helper ops `power.reboot`, `power.shutdown`; lock files `/run/nas-revival/locks/*` | blocked during SMART test/backup with reason; confirmation token; NAS comes back (reboot test with owner consent) |
| M1-07 | Shared collector + health | MON-01, UI-01 | move overview to one snapshot goroutine; health = ok/warn/crit/unknown with problem list and `checkedAt` | 3 browsers ≈ CPU of 1; idle collector at 60 s when nobody watches |
| M1-08 | Storage inventory (read-only) | STOR-10, STOR-11, STOR-12, PWR-05 | **done 2026-11-25** (sysfs + udev database instead of `lsblk`, `internal/storage`); `mountinfo`, by-id; system disk flag; new Storage app | identity survives USB re-plug; `/srv/data` and bind mounts correct; system disk warning shown |
| M1-09 ✅ 2026-11-25 | SMART detail and self-tests | SMART-10, SMART-11 | attributes table, vendor raw decoding, `smart.test` op, progress, history from the drive's own self-test log (bounded to 21 by the drive) | short test started from UI, progress visible, result in history; no wake of standby disk |
| M1-10 | Disk power management | PWR-01..PWR-04 | install `hdparm` (owner consent), `disks.json`, boot/udev apply task, profiles | setting survives reboot; `hdparm -C` query does not wake; APM hidden if unsupported |
| M1-11 | System write audit for spin-down | PWR-06, DIAG-01 | on-demand per-process write sampler; document what writes to sda at idle | report lists top writers; owner decides ADR 0007 |
| M1-12 | M1 performance gate | — | run TEST_PLAN M1 scenarios | budgets met or deviations recorded in PERFORMANCE_BUDGET |

Dependencies: M1-02 → M1-03 → (M1-06, M1-10); M1-04 → M1-08 → M1-09 → M1-10; M1-07 independent.

## M2–M6 summary

Detailed backlogs are written when the previous milestone closes; feature rows already exist in the
matrix.

- **M2:** ADR 0004 (users/roles), USR-01..05, SHARE-01..05, SVC-10..12, SMB-01..02, SSH-01,
  NET-10..11, Proxmox backup target (NFS-01 + BKP-12) because the owner needs it. Gate: Windows,
  macOS and Linux clients read/write with correct per-user permissions; SMB throughput measured with
  and without the dashboard open.
- **M3:** MON-02..06, TASK-01..03, HW-01..05, DIAG-02. Gate: 7-day run without RSS growth; disk
  sleeps when idle (if ADR 0007 allows).
- **M4:** FS-01..04, STOR-USB-01..03, BKP-01, BKP-10..11, DR-01, NOTIF-01. Gate: 2 GiB upload with
  flat RSS; backup to USB with unplug during transfer.
- **M5:** SEC-10, SEC-12, SEC-13, LOG-01..02, UPD-01..04, I18N-01, recovery drills from
  [RECOVERY](RECOVERY.md). Gate: full [TEST_PLAN](TEST_PLAN.md) failure scenarios.
- **M6:** API-10..11, PKG-01, DEV-01, Home Assistant integration.

## Boundaries

- No NAND or U-Boot environment write, reboot of the NAS, formatting, or package install without the
  owner's explicit consent for that step ([AGENTS.md](../AGENTS.md)).
- No Docker, database server, Node.js or extra daemons on the NAS. Optional services off by default.
- Kernel and Debian major upgrades are never automatic.
