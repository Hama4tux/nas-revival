# Performance budget

Two kinds of numbers live here and are never mixed: **targets** (design budgets) and
**measurements** (taken on the NAS with date, version and method). A target is not a result.

Method: `scripts/measure/snapshot.sh` over `ssh nsa310` (read-only: `/proc`, `/sys`, `ps`), or the
commands noted per row. "Used" = MemTotal − MemAvailable (excludes reclaimable cache).

## Targets

| Item | Target | Notes |
|---|---|---|
| Whole base system idle, used RAM | ≤ 100–120 MiB | with all enabled services, without page cache |
| Web backend (`nas-revival`) | ≤ 20 MiB RSS idle | peak during login allowed ≤ 45 MiB (argon2 16 MiB) |
| Helper | ≤ 8 MiB RSS idle | — |
| Monitoring + scheduler | ≤ 10 MiB | collector inside web process; timers cost 0 between runs |
| Idle CPU (no browser) | ≥ 98 % idle | measured with `vmstat 10 6` |
| CPU with dashboard open | ≤ 5 % average | 5 s polling |
| Swap | no sustained swap-in/out | `vmstat si/so` = 0 at idle |
| Disk writes caused by NAS Revival idle | 0 periodic writes; history flush ≤ 1/hour | — |
| UI initial transfer | ≤ 250 KiB gzip JS+CSS | — |
| Overview API response | ≤ 300 ms on NAS, ≤ 8 KiB gzip | — |
| Processes started per minute at idle by NAS Revival | 0 when nobody watches; ≤ 2/min when dashboard open | systemctl/smartctl are cached |
| Samba enabled, idle | ≤ 30 MiB extra (estimate, verify in M2) | — |
| SMB throughput impact of open dashboard | ≤ 5 % | measured in M2 |
| Polling defaults | CPU/RAM/net 5–10 s · temps 30–60 s · FS 60–300 s · SMART 10 min cache · processes only while open | slower when idle |

## Measurements

### 2026-11-25, baseline (M0)

NAS Revival web binary as deployed 2026-11-25 (read-only UI, helper not deployed); Debian 13.8,
kernel 6.12.119; uptime 2 h 53 min; no browser attached; services: ssh, networkd, resolved,
timesyncd, journald (persistent), udevd, logind, dbus, cron, smartmontools, nas-revival,
nsa310-button, 2 gettys.

| Metric | Value | Method |
|---|---|---|
| MemTotal | 246 136 kB | /proc/meminfo |
| Used (Total − Available) | 51 MiB | /proc/meminfo |
| MemAvailable | 189 MiB | /proc/meminfo |
| Page cache | 140 MiB | Cached |
| Swap used | 6.9 MiB | /proc/swaps |
| `nas-revival` RSS idle / PSS | 9.4 MiB / 9.4 MiB | /proc/PID/status, smaps_rollup |
| `nas-revival` peak RSS (VmHWM, after logins) | 40.8 MiB | /proc/PID/status |
| Largest other RSS | systemd 9.1 MiB, journald 8.7 MiB, networkd 6.2 MiB, resolved 5.9 MiB | ps |
| Processes | 72 | ps -e |
| CPU idle | 99–100 % (2 % us in first sample) | vmstat 1 3 |
| Load average | 0.03 / 0.04 / 0.01 | /proc/loadavg |
| Disk writes sda idle | 472 sectors / 30 s ≈ 7.9 KiB/s | /proc/diskstats |
| Listening sockets | 22/tcp ssh, 8443/tcp nas-revival, 5355 LLMNR (resolved), 53 localhost | ss -ltnup |
| UI bundle | 103 KiB JS + 5.8 KiB CSS gzip | gzip -9 of frontend/dist (workstation build) |
| Binary size | 7.8 MiB (armv5, static) | output/webui |

Second sample 2026-11-25 18:42 UTC with `snapshot.sh nsa310 10` (only the measuring SSH session
attached): used 45.2 MiB, MemAvailable 195.1 MiB, `nas-revival` 9.3 MiB RSS, CPU idle 99.6 %,
sda 20 KiB written in 10 s. The first table included two extra interactive SSH sessions (~15 MiB).

Reading: the base system already sits at about half of the 100–120 MiB target, so Samba (the biggest
expected addition) fits. The weak point is not RAM but **disk activity**: something writes ~8 KiB/s
to the only disk at idle (journal, ext4 commits, other timers) – to be attributed in M1-11.
LLMNR on 5355 is open to the LAN; decide in Security Advisor (likely disable).

## Gate procedure per module

Before a module is marked *Tested*: run `snapshot.sh` before and after enabling it, at idle and under
its typical load, and add a dated table here. Record RSS/PSS of new processes, delta of Used,
CPU idle, disk writes per minute, process count, and number of HTTP requests per minute from an open
UI. Regressions above target are tracked as open items (kept outside the public repo), not hidden.
