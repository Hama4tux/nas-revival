# ADR 0002: Web administration stack on Debian

- Status: accepted by the owner, 2026-11-25
- Context: the NAS runs Debian 13 armel (kernel 6.12, 256 MiB RAM, ARMv5TE 1.2 GHz)
  instead of OpenWrt, so the uhttpd/rpcd/ucode backend proposed in
  [technology selection](../architecture/technology-selection.md) does not apply.

## Decision

- **Backend:** one statically linked Go binary (`GOARCH=arm GOARM=5`, `CGO_ENABLED=0`),
  cross-compiled on the workstation. It serves HTTPS (self-signed certificate generated on
  the device), the versioned [API v1](../api/v1.md) and the embedded static UI. Standard
  library only, plus `golang.org/x/crypto` (argon2id) and `golang.org/x/term` (password
  prompt), both BSD-3-Clause.
- **Frontend:** React + TypeScript + Vite + Tailwind CSS with shadcn-style components and
  lucide icons, built on the workstation. The NAS only serves the built files, so the
  framework costs disk space (about 95 KiB gzip), not RAM. This matches the owner's other
  projects.
- **Privileges:** the service runs as user `nas-revival` with a systemd sandbox; the
  read-only stage needs only group `disk` and `CAP_SYS_RAWIO` for SMART. Write operations
  (Samba, NFS, users) will go through a separate, narrowly scoped privileged helper with
  typed operations, never shell strings from requests.
- **Authentication:** one administrator, argon2id hash in `/etc/nas-revival/admin.json`
  (mode 0600), in-memory sessions with `HttpOnly`/`Secure`/`SameSite=Strict` cookies,
  custom-header + Origin CSRF check, 5 failed logins per 5 min per client.

## Alternatives

- Python stdlib server: no workstation toolchain, but ~40 MB of runtime on the NAS, higher
  RSS and slow start on ARMv5.
- Cockpit: mature, but a per-session bridge is heavy for 256 MiB and armel availability was
  unverified; NAS-specific hardware views would still need custom plugins.
- Node.js on the NAS: excluded by the embedded-first rule.

## Consequences

- Go SDK is installed per user (`~/sdk`, official tarball with checked SHA-256); Node via nvm.
- Resource use must be measured on the NAS; on an amd64 workstation the process used about
  43 MiB RSS right after a login, mostly argon2's 16 MiB buffer and Go GC headroom, so the
  unit sets `GOMEMLIMIT=40MiB`, `GOGC=50` and `MemoryMax=96M`.
- Debian 13 is the last armel release; the binary does not depend on Debian libraries and
  would carry over to another base system on the same hardware.
