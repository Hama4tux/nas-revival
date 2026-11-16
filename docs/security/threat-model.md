# Initial threat model

Design only; no implementation has undergone a security audit.

## Assets and boundaries

Protect file contents, ownership/ACLs, credentials, configuration, update trust and the ability to boot stock firmware. Treat browsers, LAN clients, removable media, uploaded configuration and update artifacts as untrusted. Separate browser/API, application/privileged operations, system/user storage and trusted release/device profile boundaries.

The owner controls physical access. We cannot promise secure boot or protection against a physically present attacker on an unverified old boot chain. A signed application update does not repair that limitation.

| Threat | Required control and test |
| --- | --- |
| LAN attacker or exposed admin service | Bind administration deliberately, firewall it, disable UPnP exposure/port forwarding, TLS 1.2+ policy subject to current TLS library validation; test listening sockets and unauthenticated requests |
| Unclaimed first boot | No shared password or remotely claimable open administrator; design a physical/local enrollment window or unique per-device bootstrap secret supplied securely; non-enrollment requests rejected |
| Stolen session / browser attack | HTTPS, bounded short-lived sessions, HttpOnly/Secure/SameSite cookies if cookie transport is selected, CSRF token and Origin validation; reject cross-origin writes, login CSRF and unauthenticated RPC |
| Brute force / memory exhaustion | Rate limits before expensive password verification; use maintained password hashing, calibrate cost/concurrency on 256 MiB; do not promise an arbitrary Argon2 memory cost fits |
| Command injection / privilege escalation | Typed inputs, fixed executables/argument arrays, restrictive rpcd ACL and process/socket permissions; no generic shell/file endpoint |
| Path traversal / symlink race | Resolve operations beneath trusted share roots, disallow arbitrary paths and device nodes, defend TOCTOU and symlink escapes; recheck authorization server-side |
| Share access bypass | Explicit users/groups and modes/ACL mapping, no anonymous SMB by default, no SMB1; NFS export/client restrictions and root squashing; test cross-user access |
| Accidental data loss | Read-only inventory first; disk identity and proposed operation confirmed server-side; no auto-format, no filesystem repair or RAID assembly on discovery |
| Configuration restore attack | Versioned allowlist schema, size/depth limits, safe archive handling, no uploaded scripts or profile selectors, explicit handling of secrets and credential invalidation |
| Malicious/replayed update | Signed complete manifest, pinned public trust, hardware/config compatibility, key rotation and downgrade policy; test rejection before writes |
| Power loss / failed update | Qualified external recovery path, reserved storage space and transaction boundary; simulate interrupted staging and boot failure on disposable media first |
| Fan stops or sensor disappears | Independent qualified thermal policy and fault reporting; no thermal dependency on web uptime; stop testing if cooling is unknown |
| Sensitive debug/backup disclosure | Keep NAND, environment, certificates, MACs and serials outside public Git; sanitized logs, bounded retention, restricted backup storage |

## Legacy access aid

The pre-existing parent proxy uses obsolete TLS and disables certificate validation for the vendor web UI. It was inspected locally and not run or changed. It is outside the new repository and must not be shipped as part of NAS Revival. Future legacy access remains a conscious, separately approved lab activity; weakening system-wide TLS is prohibited.

## Release requirements

No default SSH root password, no telemetry, no required third-party login, no CDN runtime resources, no unrequested Internet exposure. Explicitly review rpcd's process privileges, password verification and the selected TLS implementation before calling the system secure. Review dependencies and supported versions at each release; artifact availability is not evidence of current security maintenance.

Security primitives should use maintained upstream implementations. This document specifies desired behavior and acceptance tests, not a claim that custom authentication or updates already exist.
