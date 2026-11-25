# NAS Revival

Give unsupported NAS hardware a useful, maintainable second life with a small Linux system and a modern local administration interface.

**Status 2026-11-25: Debian 13 armel runs on the ZyXEL NSA310** with our own 6.12 LTS kernel in two NAND slots (stock firmware kept as last fallback), the system on the internal disk, automatic fan control, LEDs and the COPY button. A read-only web administration (Go backend + static React UI) is deployed; write operations through a privileged helper are in development. Plan: [ROADMAP](docs/ROADMAP.md), current state: [FEATURE_MATRIX](docs/FEATURE_MATRIX.md).

The earlier OpenWrt qualification and the shelved Stage 1 chroot ([guide](docs/research/stage1-userspace.md)) are kept as research history.

## Goals

- SMB, NFS and SFTP/SSH; users, groups, shares and explicit permissions.
- Disk/filesystem management and SMART where supported.
- CPU, memory, network and storage status; temperatures, fan, LEDs and buttons where verified.
- Logs, service control, networking, reboot, safe shutdown, configuration backup/restore and recoverable updates.
- Responsive, accessible administration with clear health states and confirmations for critical operations.
- Embedded-first resource use, reproducible builds, device profiles and a versioned API independent of the UI.
- Local operation without required cloud services, telemetry, universal passwords or automatic Internet exposure.

This is a distribution integration project, not a rewrite of the kernel or a patch to the vendor web interface. We will reuse maintained upstream drivers and services. Additional devices need their own evidence and recovery qualification.

## Technical base

The owner chose **Debian 13 armel** over OpenWrt on 2026-11-25 (boot details in [debian/boot.md](docs/debian/boot.md)); [ADR 0001](docs/adr/0001-base-distribution.md) still describes the OpenWrt proposal and is to be rewritten. The web stack is [ADR 0002](docs/adr/0002-web-ui-stack-debian.md): one static Go binary for ARMv5 serving the API and a React/TypeScript/Vite UI built on the workstation; no Node.js, containers or database server on the NAS. Debian 13 is the last armel release, so the kernel is maintained by this project.

## Read next

| Document | Purpose |
| --- | --- |
| [Architecture](docs/ARCHITECTURE.md) | Running system, process model, modules, config manager, HAL |
| [Roadmap](docs/ROADMAP.md) | Milestones M0–M6 and the M1 backlog |
| [Web roadmap](docs/WEB_ROADMAP.md) | Web UI and API steps W0–W6 with definition of done |
| [Feature matrix](docs/FEATURE_MATRIX.md) | Every feature with priority, state and acceptance criteria |
| [Hardware compatibility](docs/HARDWARE_COMPATIBILITY.md) | Verified capabilities per device profile |
| [Performance budget](docs/PERFORMANCE_BUDGET.md) | Targets vs. dated measurements on the NAS |
| [Security model](docs/SECURITY_MODEL.md) | Trust boundaries, privileged helper, auth, web security |
| [API design](docs/API_DESIGN.md) | API conventions and planned resources |
| [UI navigation](docs/UI_NAVIGATION.md) | Apps, dashboard widgets, UI rules |
| [Recovery](docs/RECOVERY.md) | Failure scenarios and how to get the NAS back |
| [Test plan](docs/TEST_PLAN.md) | Test levels, module gates, scenario catalogue |
| [Architecture (OpenWrt era)](docs/architecture/overview.md) | Historical design, superseded by ARCHITECTURE.md |
| [Technology selection](docs/architecture/technology-selection.md) | Base system and backend tradeoffs |
| [Hardware abstraction](docs/architecture/hardware-abstraction.md) | Capability API and Linux interfaces |
| [NSA310 evidence](docs/hardware/zyxel-nsa310.md) | Reported, source-verified and unknown properties |
| [Threat model](docs/security/threat-model.md) | Authentication, privileges, storage and supply chain |
| [Recovery plan (OpenWrt era)](docs/recovery/recovery-plan.md) | Historical NAND backup and UART gates |
| [First experiment](docs/research/phase-1-experiment.md) | Workstation-only investigation; no device access |
| [Web UI stack (ADR 0002)](docs/adr/0002-web-ui-stack-debian.md) | Go backend, React UI, privileges and auth on Debian |
| [API v1](docs/api/v1.md) | Administration API contract |
| [Open items](OPEN.md) | Decisions, blockers and unverified work |
| [Bootstrap validation](docs/research/bootstrap-validation.md) | Checks performed and their limitations |
| [OpenWrt qualification](docs/research/openwrt-qualification.md) | Read-only hardware probe and what it decides |
| [OpenWrt simulation](docs/research/openwrt-simulation.md) | Verified build, layered QEMU simulation, NAND map findings |

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

The current phase permits public research, documentation and preparation of read-only diagnostics and a temporary-boot plan. Device commands only from an owner-approved read-only list. No automatic deployment, network scans, flashing, NAND writes, boot environment changes, disk initialization or host software installation. No kexec or boot experiments without separate approval and UART access. Follow [AGENTS.md](AGENTS.md).

The pre-existing `nas_proxy.py` in the parent workspace is outside this repository and was left untouched. It is a legacy access aid with weakened TLS validation, not a production component or a security pattern to reuse. No local addresses, credentials or device dumps belong in the public repository.

Original project material is [MIT licensed](LICENSE). Kernel, OpenWrt, Buildroot and bundled applications keep their own licenses; MIT does not relicense them. Binary distribution will require corresponding source, notices and a license/SBOM review.
