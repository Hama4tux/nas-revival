# ADR 0015: Log Center on the systemd journal

- Status: accepted (2026-11-27, W5.3)
- Scope: LOG-01 (journal query by category/severity/time/text, export), LOG-02 (retention and size limits),
  audit view of administrative actions
- Context: the only log store on the NAS is the systemd journal (persistent, capped by
  `50-nas-revival-journald.conf`, ADR 0007). The Services app already reads the last lines of one catalog unit
  through the helper (`journal.unit`, SVC-12). Administrative actions are logged by `slog` as text lines on the
  service's stderr (`msg="user created" account=… by=…`), so they cannot be filtered reliably.

## Decision

- **No own log store.** The Log Center queries the journal; no database, no log files, no extra daemon.
- **Query through the helper** (`journal.query`, read-only): the web service has no journal access. The helper
  runs `journalctl --output=json --no-pager --quiet --reverse --lines=N` with arguments built from validated
  fields only (argument array, no shell):
  - `category` from a fixed list, each mapped to fixed journal matches: `all` (none), `kernel` (`--dmesg`),
    `auth` (`SYSLOG_FACILITY=4 SYSLOG_FACILITY=10`: sshd, PAM, sudo, login), `nas` (the units
    `nas-revival.service` and `nas-revival-helper.service`), `services` (the units of the Services catalog),
    `audit` (see below); optional `unit` = one unit of the Services catalog instead of a category;
  - `priority` 0–7 → `--priority=0..N` (this severity and worse);
  - `since` / `until` (RFC 3339) → `--since=@<unix>` / `--until=@<unix.micro>`; paging to older entries passes
    the oldest loaded time as `until`;
  - `text` (≤ 100 characters, no control characters) → `--grep=<pattern> --case-sensitive=false`, where every
    non-alphanumeric character is escaped with `\` so the text is always a literal, never a regular expression;
  - `limit` 1–1000 (default 200).
  Output fields: time, priority, identifier, unit, PID, message (≤ 1000 characters, valid UTF-8) and the audit
  fields. Empty result is not an error; a journalctl failure is.
- **Audit records as structured journal fields.** A small package `internal/audit` writes one entry per
  administrative action over the journal's native socket (`/run/systemd/journal/socket`, datagram, the
  documented native protocol – no new dependency) with `MESSAGE` (readable sentence), `PRIORITY` (6, or 4 for
  failed logins and wrong passwords), `NAS_REVIVAL_AUDIT=1`, `AUDIT_ACTION` (fixed identifier such as
  `user.create`), `AUDIT_USER` (who), `AUDIT_CLIENT` (client address, where known), `AUDIT_TARGET` (object, e.g.
  account or unit) and `AUDIT_RESULT` (`ok`/`fail`). It replaces the existing `slog` lines of those actions; when
  the socket is missing (development machine) it falls back to `slog`. The `audit` category matches
  `NAS_REVIVAL_AUDIT=1` **together with the trusted field** `_SYSTEMD_UNIT=nas-revival.service` (or the helper
  unit), so another local process cannot forge entries in the audit view. Values never contain passwords,
  tokens or file contents.
- **Retention** (`journal.retention.get` / `.set`): the helper rewrites the drop-in
  `/etc/systemd/journald.conf.d/50-nas-revival.conf` (keeps `SyncIntervalSec=15min`) with `SystemMaxUse` from
  {32M, 64M, 128M, 256M} and `MaxRetentionSec` from {0 = size only, 1week, 1month, 3month, 1year}, then
  restarts `systemd-journald` and reports `journalctl --disk-usage`. The default stays 64 MiB / size only. The
  file is not in the config history (it has no secrets but is a plain system file; export/import may add it
  later).
- **API:** `GET /api/v1/logs?category=&unit=&priority=&since=&until=&text=&limit=` (administrator and
  read-only administrator) → `{entries, more}`; `GET /api/v1/logs/retention` (same roles) → current limits,
  disk usage; `PUT /api/v1/logs/retention` (administrator). A retention change is itself an audit record.
- **Export** is done by the browser from the loaded entries (text file, max 1000 lines) – no extra endpoint.

## Consequences

- Logs survive as long as journald keeps them; the audit trail is bounded by the same retention (documented in
  the UI). A tamper-proof or remote audit log is out of scope (M6: remote syslog).
- Kernel and auth logs can contain user names and client addresses; they are shown only to administrators.
- Text search runs in journalctl on the NAS; a broad query over a full journal takes a few seconds, so the
  request has a 20 s timeout and the UI shows progress.
