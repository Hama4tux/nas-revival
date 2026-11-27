# ADR 0013: Security Center – sessions and advisor

- Status: accepted (2026-11-27, W5.1 phase 1)
- Scope: SEC-10 (session list, forced logout), SEC-13 (Security Advisor); sessions surviving a service restart
- Context: sessions lived only in memory, so every deployment logged everybody out, and an administrator could
  not see or end other sessions (only indirectly by changing a password). The matrix asks for a revoked session to
  get 401 immediately and for each advisor finding to carry a fix link.

## Decision

- **Session store keeps token hashes only.** Each session gets a random public ID (unrelated to the cookie); the
  UI and API work with that ID, so listing sessions never reveals anything usable as a cookie.
- **Metadata:** client address (no proxy in front, `X-Forwarded-For` is never trusted) and the User-Agent cut to
  160 characters without control characters.
- **Persistence:** `/var/lib/nas-revival/sessions.json` (0600, atomic write) with token SHA-256, ID, user,
  metadata, creation and last activity. Written on login, logout, revocation and on exit – never on ordinary
  requests, so an idle NAS keeps its disk asleep. After a crash the stored last activity may be older than the real
  one, which can only end a session earlier. Expired or future-dated records are dropped on load; a broken file is
  logged and the service starts with no sessions. The file is written outside the session lock with a generation
  counter, so a slow or sleeping disk does not block requests and an old snapshot never overwrites a newer one.
- **Revocation** removes the session from memory first, so the next request with that cookie is 401 even if the
  file write fails. Endpoints: list, revoke one (own included: the cookie is cleared), revoke all others. Sessions
  are administrator-only (they show addresses and browsers of other users); each revocation is logged with the
  acting user.
- **Advisor = pure function over current configuration** (`internal/security.Evaluate`), inputs read through the
  existing helper operations (SSH, SMB, NFS, users) plus the TLS manager and the login limiter. A backend that
  fails turns only its checks into `unknown`. Findings carry an ID, status (crit/warn/info/ok/unknown), parameters
  and a fix route; texts live in the frontend so W5.5 can translate them. Readable by the read-only administrator.
- **Checks of phase 1:** root password login over SSH, password authentication with SFTP users, SMB minimum
  protocol 2.1, NFS clients outside private networks, certificate expiry (warn < 30 days) and self-signed
  certificate, only the recovery administrator / recovery administrator enabled, clients blocked by the login
  limiter.

## Consequences

- Deployments no longer log users out (sessions within their idle and absolute limits continue).
- Not yet checked: security updates (needs UPD-01), unexpected listening ports, firewall (W5.2), open shares
  beyond the user/group model. These are added when their data source exists.
