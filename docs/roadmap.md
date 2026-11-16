# Roadmap

Milestones are gated by evidence rather than calendar promises. M0 is complete; the current bounded task adds offline discovery evidence toward M2. No boot experiment or implementation stage is authorized by this document.

## Stage 1 side experiment: existing-kernel chroot

The owner authorized preparation of only a minimal userspace beside the stock firmware: shell, basic diagnostics and key-only Dropbear on 2222. [Build recipe and manual control](research/stage1-userspace.md) are prepared and workstation safety tests pass. A full build, actual ELF/dependency/size audit and vendor-kernel/SSH/stop tests remain pending. No NAS deployment is authorized in this turn; Stage 2 is excluded. This experiment needs neither a new kernel nor any bootloader/UART/USB boot change and does not advance M3.

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
