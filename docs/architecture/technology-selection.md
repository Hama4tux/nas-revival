# Technology selection

Status: proposed, 2026-11-16. Base comparison and decision gates are in [ADR-0001](../adr/0001-base-distribution.md).

## Backend evaluation

| Candidate | Fit | Main tradeoff | Decision |
| --- | --- | --- | --- |
| OpenWrt uhttpd + rpcd/ubus + ucode | Reuses distribution-native HTTP/TLS, RPC and configuration facilities; small scripts for application logic | OpenWrt coupling, ACL review and blocking-operation limits; actual footprint unmeasured | Preferred experiment if OpenWrt qualifies |
| C service / narrowly scoped helper | Cross compilation and direct Linux interfaces without an interpreter; useful on Buildroot too | Memory safety, parser/authentication risk and more protocol maintenance | Use only where existing tools cannot expose a necessary typed operation; avoid a custom HTTP/TLS implementation |
| Lua with existing server | Lightweight scripting alternative | Additional runtime/module choice; no clear benefit over OpenWrt's ucode integration | Reserve for a demonstrated dependency |
| Go or Rust native service | Potential single executable and stronger language tooling | Must qualify precise ARMv5 target, libc, atomic requirements, maintained toolchain, binary size and RSS; no benchmark here | Not selected without a demonstrable advantage and separate ADR |
| Node.js / Python web framework / containers | Familiar development ecosystem | Additional runtime, packaging and memory costs without an established requirement | Not part of target architecture |

uhttpd's release packaging includes TLS dependencies and ubus/ucode modules; rpcd offers a ucode plugin. This establishes an upstream integration path, not a secure custom API by itself. Sources: [uhttpd package](https://github.com/openwrt/openwrt/blob/v25.12.6/package/network/services/uhttpd/Makefile), [rpcd package](https://github.com/openwrt/openwrt/blob/v25.12.6/package/system/rpcd/Makefile), [ucode documentation](https://ucode.mein.io/).

Expose only NAS-specific operations through restrictive ACLs. Never grant the UI blanket rpcd `file`, `exec`, UCI or system-upgrade access. Reuse upstream session machinery behind an explicitly designed browser session/CSRF policy. Privileged operations use structured validated arguments and allowlisted executables, never request-derived shell strings.

Some system daemons need privileges, but the frontend server and custom modules must not gain broad root authority by default. The exact process/user/socket separation must be verified against the selected rpcd/uhttpd implementation; native OpenWrt defaults are not proof of least privilege.

Long-running storage/SMART operations need bounded jobs with IDs, timeout, single-writer locking and honest cancellation semantics; they must not block the HTTP loop. First backend prototype exposes read-only status only.

## NAS services

- Compare maintained Samba and available kernel SMB server packaging before choosing; assess security features, permissions and memory on ARMv5. SMB1 is excluded.
- NFS exports require deliberate client/network rules and root squashing; a LAN is not an authentication mechanism. Document whether the selected NFS mode provides encryption/authentication before enabling it.
- Dropbear/OpenSSH selection must include actual SFTP subsystem availability; an SSH daemon alone does not imply SFTP support. Public-key administration first, no passwordless root.
- SMART uses upstream smartmontools only after controller/disk compatibility is demonstrated; avoid polling that repeatedly wakes a sleeping disk.

Package availability, versions, licenses, CVEs and measured costs remain to be qualified in the selected release. These are design requirements, not claims that every service fits the default image.

## Frontend

React, TypeScript and Vite are permitted workstation build tools, not yet introduced dependencies. Compare a small React build with a simpler static implementation at prototype time. Serve only built HTML/CSS/JS, no SSR, runtime package manager, CDN fonts or external analytics.

Navigation: Overview, Storage, Shares, Users, Services, Network, Logs, System. Show useful snapshots and actionable states; missing sensors display unavailable rather than zero. Include loading/stale/error states, keyboard operation and responsive layouts. Destructive actions require reauthentication where appropriate, exact target identification and a server-side plan/confirm flow. No UI is implemented during bootstrap.
