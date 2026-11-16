# NAS Revival

Give unsupported NAS hardware a useful, maintainable second life with a small Linux system and a modern local administration interface.

**Status: Stage 1 userspace recipe and manual chroot controller prepared — 2026-11-16. No rootfs build or NAS deployment has been completed; no firmware, backend or frontend is implemented.** The first target is the original **ZyXEL NSA310 A203**; precise board-profile qualification remains incomplete. An OpenWrt image named NSA310b is not evidence that it is safe for this unit.

The [Stage 1 guide](docs/research/stage1-userspace.md) prepares only a shell, basic diagnostics and key-only Dropbear SSH on port 2222 alongside the existing firmware. It proposes maintained Buildroot/uClibc-ng with legacy kernel headers; runtime compatibility with the vendor Linux 2.6.31.8 is unverified. All NAS installation/start operations remain manual and were not executed.

## Goals

- SMB, NFS and SFTP/SSH; users, groups, shares and explicit permissions.
- Disk/filesystem management and SMART where supported.
- CPU, memory, network and storage status; temperatures, fan, LEDs and buttons where verified.
- Logs, service control, networking, reboot, safe shutdown, configuration backup/restore and recoverable updates.
- Responsive, accessible administration with clear health states and confirmations for critical operations.
- Embedded-first resource use, reproducible builds, device profiles and a versioned API independent of the UI.
- Local operation without required cloud services, telemetry, universal passwords or automatic Internet exposure.

This is a distribution integration project, not a rewrite of the kernel or a patch to the vendor web interface. We will reuse maintained upstream drivers and services. Additional devices need their own evidence and recovery qualification.

## First technical decision

Investigate **OpenWrt first**, with **Buildroot as the fallback** and Debian ARMEL as a time-limited experiment. OpenWrt 25.12.6 publishes a Kirkwood NSA310b profile; Linux has distinct NSA310 device descriptions. Debian 13 is the last ARMEL release. These are upstream facts, not successful tests on our unit. See the [ADR](docs/adr/0001-base-distribution.md) and [dated source review](docs/research/upstream-support.md).

The proposed OpenWrt backend uses uhttpd, rpcd/ubus and small ucode modules with narrowly scoped privileged operations. React/TypeScript/Vite is an optional workstation build stack; only static assets would run on the NAS. No Node.js or containers are required on the target. Neither choice is a measured implementation yet.

## Read next

| Document | Purpose |
| --- | --- |
| [Architecture](docs/architecture/overview.md) | Boundaries, service model, updates and resource targets |
| [Technology selection](docs/architecture/technology-selection.md) | Base system and backend tradeoffs |
| [Hardware abstraction](docs/architecture/hardware-abstraction.md) | Capability API and Linux interfaces |
| [NSA310 evidence](docs/hardware/zyxel-nsa310.md) | Reported, source-verified and unknown properties |
| [Threat model](docs/security/threat-model.md) | Authentication, privileges, storage and supply chain |
| [Recovery plan](docs/recovery/recovery-plan.md) | Future NAND backup, UART and temporary boot gates |
| [First experiment](docs/research/phase-1-experiment.md) | Workstation-only investigation; no device access |
| [Roadmap](docs/roadmap.md) | Milestones and acceptance criteria |
| [Open items](OPEN.md) | Decisions, blockers and unverified work |
| [Bootstrap validation](docs/research/bootstrap-validation.md) | Checks performed and their limitations |

## Repository layout

```text
docs/          architecture, hardware, research, security, recovery, ADR, roadmap
devices/       evidence and future per-board profiles; currently not deployable
firmware/      integration policy; no images or build configurations yet
backend/       backend scope and implementation gate
frontend/      static UI scope and implementation gate
scripts/       future reproducible build/check entry points
tests/         validation strategy; no hardware test results yet
tools/         workstation tooling policy
```

The requested layout is retained. Later OpenWrt packages/overlays belong under `firmware/openwrt/`, or a Buildroot `br2-external` tree under `firmware/buildroot/`, once ADR-0001 is accepted. Do not fork or vendor an entire upstream distribution now.

## Safety and contributing

The current Stage 1 task permits local userspace configuration, scripts and workstation tests only. No NAS requests, automatic deployment, network scans, flashing, NAND writes, boot environment changes, disk initialization or host software installation. No new kernel, kexec or boot experiments. Follow [AGENTS.md](AGENTS.md).

The pre-existing `nas_proxy.py` in the parent workspace is outside this repository and was left untouched. It is a legacy access aid with weakened TLS validation, not a production component or a security pattern to reuse. No local addresses, credentials or device dumps belong in the public repository.

Original project material is [MIT licensed](LICENSE). Kernel, OpenWrt, Buildroot and bundled applications keep their own licenses; MIT does not relicense them. Binary distribution will require corresponding source, notices and a license/SBOM review.
