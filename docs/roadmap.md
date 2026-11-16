# Roadmap

Milestones are gated by evidence rather than calendar promises. M0 is complete; the current bounded task adds offline discovery evidence toward M2. No boot experiment or implementation stage is authorized by this document.

## Role of Buildroot vs. OpenWrt

- **OpenWrt Kirkwood remains the preferred long-term base** ([ADR-0001](adr/0001-base-distribution.md), status proposed). It requires a new kernel and therefore a bootloader/NAND change, so it belongs to M3+ and its ADR gates.
- **Buildroot in Stage 1 is only a build tool** for a small chroot userspace that runs on the unchanged stock kernel 2.6.31.8. Prebuilt OpenWrt binaries cannot run on that kernel, and modern glibc requires ≥ 3.2, hence uClibc-ng with old headers. Stage 1 is not the final product and does not replace the OpenWrt decision.
- **Stage 1 shelved (2026-11-16):** the first full build failed on a uClibc-ng 1.0.59 vs 2.6.31.8 `linux/socket.h` conflict. The owner decided not to patch legacy headers: OpenWrt replaces kernel and userspace anyway, and existing root Telnet suffices for read-only evidence gathering.
- Buildroot stays the fallback base only if OpenWrt cannot support this board with small upstreamable changes.

## Owner risk decision (2026-11-16)

The owner considers the device, in its stock state with a 2009 kernel, to have no practical value and accepts the risk of experimenting with it. If an experiment fails, the plan is to buy a USB–TTL (UART) adapter and recover. Full raw dumps of all nine NAND partitions (mtd0–mtd8, with OOB and SHA256SUMS) are kept privately outside the repository. This sets the risk tolerance only; it does not replace per-step owner approval, and restoring from these dumps is not yet qualified (ECC/OOB/bad-block handling, see [OPEN.md](../OPEN.md)). The owner confirmed the existing `/dev/md0` volume is empty after a reformat and may be used freely for experiments; disk health (SMART) is still unverified.

## Current focus: OpenWrt qualification

1. Read-only evidence via existing root Telnet (owner-approved command list): SoC/clock, Ethernet PHY, fan/thermal controller, `CONFIG_KEXEC`, MTD and U-Boot environment consistency — compared against the OpenWrt NSA310b profile and Linux NSA310 device trees.
2. USB–TTL (UART) adapter and verified pinout before any boot experiment.
3. M3: OpenWrt initramfs booted from USB or RAM via U-Boot, without NAND writes, with a documented return to stock.
4. Only then NAND installation and recovery qualification (M7).

The [Stage 1 chroot recipe](research/stage1-userspace.md) is shelved and kept for reference only.

| Milestone | Deliverable | Acceptance / dependency |
| --- | --- | --- |
| M0 — bootstrap | Local repository, sourced analysis, architecture, threat/recovery design | Complete; no firmware or live tests |
| M1 — feasibility dossier | Source delta, package closure, API/capability fixtures and inventory form | Workstation-only; reproducible provenance; no active device profile |
| M2 — identity and backup | Exact board, bootloader/MTD inventory, private verified backups and recovery proposal | Partial: hardware observations/root Telnet and nine OOB backups recorded; offline U-Boot identified. Board variant, address discrepancy and restore qualification remain open |
| M3 — temporary boot | Minimal external boot with UART evidence and cooling | Separate authorization; no NAND/environment/user-disk writes; documented return to stock |
| M4 — read-only administration | Status API, HAL adapter, static responsive dashboard | Auth/ACL/CSRF tests, real resource measurements and missing-device behavior |
| M5 — NAS services | Users/groups, share permissions, SMB/NFS/SFTP and SMART | Disposable test disk; concurrent access/permission tests; security and memory budgets |
| M6 — safe administration | Service/network controls, safe poweroff, config export/import | Atomic configuration, network rollback, bounded logs and critical-action protections |
| M7 — release and recovery | Signed reproducible releases, qualified installation/update storage design | Power-loss/failure tests, key policy, licensing/SBOM, documented external recovery |
| M8 — second device | Additional separately qualified device profile | Prove API/application reuse without copying board-specific logic |

## Completed

- [x] M0: documentation bootstrap, conditional distribution decision and risk inventory.
- [x] Offline discovery: OOB structure/integrity, U-Boot identification, compiled command evidence and no-UART boot assessment; no device access or boot test.

The [U-Boot analysis](research/uboot-analysis.md) does not establish a safe one-shot boot path without UART. Preserve NAND boot; obtain qualified serial access before a separately approved experiment. Root Telnet and matching hashes do not complete M2 or unlock M3. kexec remains unverified and prohibited in this task.

## Implementation boundaries

M1 designs the first read-only API; M4 implements it. M2 must not “fix” NAND partitions or initialize disks. M3 cannot start because a download index contains an image. M5 uses disposable data and requires explicit consent before formatting test media. Persistent deployment is part of M7 after update/recovery behavior is qualified, not an accidental consequence of M3.

Open questions and immediate blockers are maintained only in [OPEN.md](../OPEN.md), not duplicated as unchecked todo lists here.
