# ADR 0007: Disk power management and system write reduction

- Status: accepted by the owner, 2026-11-25 (web roadmap W1.5, W1.9)
- Context: on the NSA310 root, swap, journal and data share the only internal disk. Measured idle
  write rate ~8 KiB/s ([PERFORMANCE_BUDGET](../PERFORMANCE_BUDGET.md)), so the disk never sleeps.
  The SSHD also parks its heads often (SMART 193 = 35 003 at 6 866 h).

## Decision

1. Install `hdparm`; NAS Revival manages APM and standby timeout per disk (by-id) from
   `/etc/nas-revival/disks.json`, applied at boot and on hotplug. Default profile **balanced**:
   APM 254 for disks that park aggressively (no head parking), no standby timeout on the system disk.
2. First reduce writes, then measure: `noatime` on root and data, journald `SyncIntervalSec=15min`
   and `SystemMaxUse=64M` (persistent journal kept for crash diagnosis), and attribute remaining
   writers with the on-demand activity tool (W1.9).
3. Spin-down of the system disk is offered only with an explicit warning that it will be woken by
   the system; moving the root to the USB stick is **not** done now and is re-evaluated after the
   measurement in step 2.
4. Monitoring never wakes a disk: `smartctl --nocheck=standby`, `hdparm -C`, cached filesystem stats.

## Consequences

- Up to 15 min of non-critical log lines can be lost on a crash (critical messages are synced at once).
- Real data-disk sleep needs a second disk or the root elsewhere; documented in the UI warning.
