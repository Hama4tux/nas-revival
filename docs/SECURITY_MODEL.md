# Security model

Status 2026-11-25. Builds on the [threat model](security/threat-model.md) (assets, threats) and
[ADR 0002](adr/0002-web-ui-stack-debian.md) / [ADR 0003](adr/0003-privileged-helper-settings.md).
Nothing here has had an external security audit.

## Trust boundaries

```
LAN client ──HTTPS──▶ nas-revival (uid nas-revival, sandbox) ──Unix socket──▶ helper (root)
   untrusted              authn, authz, first validation            re-validation, fixed argv,
                                                                    file registry, audit
```

1. **Browser → web:** everything from the network is untrusted, including authenticated admins'
   input. Authorization is checked on every endpoint, server-side.
2. **Web → helper:** the helper treats the web process as potentially compromised. It accepts only
   peer uid `nas-revival`, a closed list of ops, and re-validates every argument. A compromised web
   process can at most perform allowlisted ops with valid arguments.
3. **Helper → system:** fixed executables by absolute path, `exec` with argument arrays, no shell,
   environment cleared, stdin for secrets, timeouts on every subprocess.

## Privilege separation rules

| Rule | Implementation |
|---|---|
| Web never root | systemd `User=nas-revival`, `NoNewPrivileges`, `ProtectSystem=strict`, only `CAP_SYS_RAWIO` + group `disk` (SMART) |
| No generic root API | helper op table; no `exec`, `write_file`, `read_file` or path-taking op |
| No user-supplied paths to system files | ops take IDs (share id, disk by-id, unit name from allowlist); helper maps IDs to paths |
| No sudo | the helper is the only privileged entry point; `nas-revival` has no sudoers entry |
| No shell strings | `exec.Command(abs, args...)`; arguments validated by regex/enum; `--` before positional args |
| Bounded requests | ≤ 16 KiB JSON, 5 s read deadline, per-op timeout, one config transaction at a time |
| Audit | every op → journald record (`SYSLOG_IDENTIFIER=nas-revival-audit`, fields `NR_USER`, `NR_OP`, `NR_OBJECT`, `NR_RESULT`); no secrets |
| Helper sandbox | `ProtectSystem=strict` + `ReadWritePaths` (`/etc` since W2.1: the shadow tools need lock/temp files next to `/etc/passwd`; `/srv` since W2.3 for share folders, which are only created under a mounted data volume, never from a request path), `RestrictSUIDSGID` (share folders therefore use no setgid bit), `ProtectHome`, `PrivateTmp` |

Helper op naming: `<module>.<verb>` (`network.apply`, `smart.test`, `disk.setPower`, `share.create`,
`user.setPassword`, `service.restart`, `power.reboot`). Each op declares: required role, validator,
files touched, commands run, whether it needs confirmation, whether it takes the heavy-task lock.

## Authentication

- **Recovery administrator:** argon2id (t=3, m=16 MiB, p=1) in `/etc/nas-revival/admin.json` (0600),
  set only over SSH (`nas-revival set-password`), always role admin (ADR 0004, amended in W2.2).
- **M2 (ADR 0004):** accounts = Linux users; web hash per Linux username in
  `/etc/nas-revival/credentials.json` (0600, written only by the helper, owned by `nas-revival`
  and read by the web login; ADR 0004). Since W2.2 these accounts log in; roles by Linux group:
  `nas-admin`, `nas-viewer`; others = standard user (own password only).
- Sessions: random 256-bit id, server-side map, idle 30 min, absolute 12 h, rotate on login,
  invalidate all sessions of a user on password change. Session list and forced logout in M5.
- Brute force: per-client limit 5 failures / 5 min (exists) + global concurrency 1 for argon2
  verification (protects RAM) + per-account backoff (M2).
- First boot: password set locally over SSH (`nas-revival set-password`), no default password.
  A first-run web enrollment, if ever added, must require physical presence (COPY button press).
- TOTP: optional, P3.

## Web security

| Control | State |
|---|---|
| HTTPS only, TLS ≥ 1.2, self-signed cert generated on device, upload own cert | implemented / in development |
| Cookies `HttpOnly`, `Secure`, `SameSite=Strict` | implemented |
| CSRF: required header `X-Requested-With: nas-revival` + Origin check on non-GET | implemented |
| CSP `default-src 'self'`, no inline script, no CDN, `frame-ancestors 'none'`, `nosniff` | implemented |
| Output escaping: React escapes text; no `dangerouslySetInnerHTML` | rule, check in review |
| Input validation: typed JSON decoding with `DisallowUnknownFields`, length limits | per endpoint |
| Errors without stack traces or internal paths | implemented |
| Role check on every endpoint (default by method: GET = read-only admin, other = admin; user-level endpoints listed explicitly) | implemented (W2.2) |
| API tokens with scopes | M6 |

## Data protection

- Never log or return passwords, password hashes, TLS private keys, API tokens.
- Passwords reach `chpasswd`/`smbpasswd` only via stdin, never argv (visible in `/proc`).
- Serial numbers and MACs are visible to administrators only (they identify the device).
- Config export excludes secrets unless encrypted with a passphrase (argon2id → AES-256-GCM, stdlib + x/crypto).
- Destructive ops (format, delete share data, delete user home, mirror backup with delete, kernel
  write) require a server-issued confirmation token bound to the object and expiring in 60 s, plus
  typing the object name in the UI.

## Network exposure

- Default listen `:8443` on LAN; no UPnP, no port forwarding, no cloud relay, no telemetry.
- Firewall (M5): nftables table `inet nas_revival` in `/etc/nftables.d/`, default allow LAN subnet
  only for admin/SMB/NFS/SSH; apply with confirm-or-rollback (180 s) like network changes; the
  current admin client's address is always kept allowed.
- Disable services not needed: LLMNR (resolved, 5355) is open today – Security Advisor finding.
- SMB: SMB1 off, `min protocol = SMB2_10`, no guest, `restrict anonymous = 2`.
- NFS: explicit client list only (IP addresses or networks no wider than /16, IPv6 /48; no `*`, host names or
  netgroups), `root_squash` default and `no_root_squash` never offered, no `insecure`, `sync`. Squashed
  clients write as `nobody` with the member-less group `nas-nfs`, so NFS write access is granted per share
  without granting it to SMB users. NFSv4.1/4.2 only (TCP 2049; rpcbind on TCP 111 only for the Proxmox online check, UDP on loopback). Exports
  reference shared folders by name; paths never come from the request. NFS off renders an empty exports file.
- SSH (W2.7): always on (the recovery path; the Services app cannot stop it either),
  `PermitRootLogin prohibit-password` fixed, `AllowGroups root` plus the groups granted SFTP. NAS accounts
  have no shell; SFTP groups get `ForceCommand internal-sftp`, `ChrootDirectory /srv` (root-owned, checked
  before enabling) and no TTY, TCP/agent/X11 forwarding or tunnels; file access follows the share ACLs.
  Port 22 or 1024–65535 (not 2049/8443, not used by another listener). The drop-in is checked with `sshd -t`
  and restored when rejected; reload keeps open sessions. Disabling root login waits until admin access moves to a
  named user with a shell (M2 follow-up).
- SSH user keys (W2.7b): admin only; only NAS accounts (never root or system accounts, whose keys stay
  outside the UI). A key is parsed and written back as `type base64 comment`: authorized_keys options
  (`command=`, `from=`, …), private keys, DSA, certificates and RSA below 2048 bits are refused, so an upload
  cannot widen what sshd allows. Files are root-owned 0644 (StrictModes), at most 10 keys per user; only
  members of SFTP groups get past `AllowGroups`. A disabled account (shadow expiry) is refused by PAM even
  with a key. Keys go with the account on delete and never pass to a new account of the same name. Not
  part of the config history (per-user data, like passwords); additions and removals are logged with
  the fingerprint.

## Security Advisor checks (M5)

SMB1 enabled · FTP enabled · root SSH with password · share writable by everyone · NFS export to
`*` · certificate expiring < 30 days · pending Debian security updates · listening ports not in the
expected list · admin password unchanged since install · web UI exposed on non-LAN interface.
Each finding: severity, explanation, one-click link to the setting.

## Supply chain

Go modules pinned in `go.sum` (only `golang.org/x/crypto`, `x/term`), npm via `package-lock.json`,
built on the workstation. Release artifacts (M5) are signed with ed25519; the public key is
embedded in the binary; downgrade below the installed version requires explicit confirmation.
