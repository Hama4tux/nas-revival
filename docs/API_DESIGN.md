# API design

The implemented contract is [api/v1.md](api/v1.md) (session, overview, SMART, settings). This
document defines the **rules and the planned resources** so new modules stay consistent. When an
endpoint is implemented, it moves into `api/v1.md`.

## Conventions (apply to all endpoints)

- Base path `/api/v1`, JSON UTF-8, camelCase fields. Missing data `null`, never `0` or `""`.
- Units in field names: `…Bytes`, `…Seconds`, `milliCelsius`, `…Percent`, `…Mbps`. Timestamps RFC 3339 UTC.
- Auth: session cookie (UI) or `Authorization: Bearer <token>` (M6). Every endpoint declares its
  minimum role: `viewer` (read-only admin), `admin`, `user` (own data only). Default deny.
- CSRF: non-GET needs `X-Requested-With: nas-revival` and same Origin (tokens exempt).
- Errors: `{"error": {"code": "snake_case", "message": "…", "details": {…}}}`; codes are stable
  and translated in the UI. HTTP status: 400 validation, 401, 403, 404, 409 conflict/locked,
  422 rejected by validator (e.g. `testparm`), 429, 503 helper unavailable.
- Objects are addressed by stable IDs, never by device paths: disks `ata-…`/`usb-…`/`wwn-…`
  (by-id name), partitions by `PARTUUID`, shares/users/groups by name, services by allowlist key.
- Lists are paged when unbounded: `?limit=&cursor=`; response `{"items": [], "nextCursor": null}`.
- Long operations return `202 {"jobId": "…"}`; `GET /jobs/{id}` → `{state: queued|running|done|failed|cancelled, progressPercent, message, result}`. Jobs are kept in RAM, max 32, oldest finished evicted.
- Destructive operations: `POST …/{op}/preview` returns `{confirmToken, effects: […]}`; the real
  call must carry `confirmToken` (60 s, single use, bound to user + object + op).
- Pending changes with rollback (network, firewall): `PUT` returns `{pendingUntil}`; confirm with
  `POST …/confirm` from the new address.
- Polling: one aggregate endpoint per screen (no per-widget endpoints). Responses carry
  `generatedAt`; the server serves cached collector snapshots, so many clients cost the same.
- Disk-safe reads: endpoints that could wake disks document it; status endpoints never do.

## Resources by milestone

| Area | Endpoint | Role | Helper op | Milestone |
|---|---|---|---|---|
| System | `GET /system/info` | viewer | – | M1 |
| | `GET /system/capabilities` | viewer | – | M1 |
| | `GET /system/health` | viewer | – | M1 (part of overview today) |
| | `POST /system/reboot/preview`, `POST /system/reboot` | admin | `power.reboot` | M1 |
| | `POST /system/shutdown/preview`, `POST /system/shutdown` | admin | `power.shutdown` | M1 |
| Config | `GET /config/history` (incl. drift), `GET /config/history/{n}` (diff vs. current) | admin | `config.list`, `config.get` | M1 (done W1.8) |
| | `POST /config/rollback/{snapshot}` | admin | `config.rollback` | M1 |
| | `GET /config/export`, `POST /config/import` | admin | `config.export/import` | M4 |
| Storage | `GET /storage` (disks, partitions, mounts, system flags, dependencies) | viewer | – | M1 |
| | `GET /storage/disks/{id}/smart` (attributes, self-test log, progress) | viewer | – | M1 |
| | `POST /storage/disks/{id}/smart/test` `{type: short\|long}` | admin | `smart.test` | M1 |
| | `GET/PUT /storage/disks/{id}/power` `{profile, standbyMinutes, apm}` | admin | `disk.setPower` | M1 |
| | `POST /storage/disks/{id}/standby`, `/wake` | admin | `disk.standby` | M1 |
| | `GET /storage/activity?seconds=` (on demand, blocks for the window) | admin | `disk.activity` | M1 (done W1.9) |
| | `GET /storage/activity` (on demand, ~10 s sample) | admin | `disk.activity` | M1 |
| | `POST /storage/volumes/{partuuid}/mount`, `/unmount`, `/eject` | admin | `volume.*` | M1–M4 |
| Users | `GET/POST /users`, `GET/PATCH/DELETE /users/{name}`, `PUT /users/{name}/password` | admin (self for password) | `user.*` | M2 |
| | `GET/POST /groups`, `PATCH/DELETE /groups/{name}` | admin | `group.*` | M2 |
| Shares | `GET/POST /shares`, `GET/PATCH/DELETE /shares/{name}?data=keep\|delete` | admin | `share.*` | M2 |
| | `GET /shares/{name}/permissions/effective?user=` | admin | – | M2 |
| File services | `GET/PUT /services/smb`, `/services/nfs`, `/services/ssh` | admin | `smb.apply`… | M2 |
| | `GET /services/smb/sessions` | admin | `smb.status` | M2 |
| Services | `GET /services` (allowlist, state, PID, RAM, CPU, ports) | viewer | – | M2 |
| | `POST /services/{key}/{start\|stop\|restart\|reload\|enable\|disable}` | admin | `service.*` | M2 |
| | `GET /services/{key}/logs?lines=` (≤ 200) | admin | `journal.unit` | M2 |
| Network | `GET/PUT /settings/network`, `POST …/confirm` | admin | `network.*` | in development |
| | `GET /network` (hostname, domains, interfaces, routes) | viewer | – | implemented |
| | `PUT /network/hostname` `{hostname}` | admin | `net.hostname` | implemented |
| | `POST /network/diagnostics` `{test: ping\|dns\|gateway\|internet\|ports\|connections, target}` (409 busy, 429 > 20/min) | admin | `net.diag` | implemented |
| Monitor | `GET /monitor` (snapshot + ring buffer), `GET /monitor/processes` | viewer | – | M3 |
| | `POST /monitor/processes/{pid}/kill` | admin | `process.kill` | M3 |
| Tasks | `GET/POST /tasks`, `PATCH/DELETE /tasks/{id}`, `POST /tasks/{id}/run`, `/stop` | admin | `task.*` | M3 |
| Hardware | `GET/PUT /hardware/fan`, `/hardware/leds`, `GET /hardware/sensors/history` | admin | `fan.setProfile`, `led.set` | M3 |
| Backup | `GET/POST /backups`, `POST /backups/{id}/run?dryRun=` , `GET /backups/{id}/history` | admin | `backup.*` | M4 |
| USB | `GET /storage/usb`, `POST /storage/usb/mount` `{diskId, partition, readOnly}`, `…/unmount`, `…/eject` `{diskId}` (409 busy) | viewer / admin | `usb.*` (ADR 0010) | implemented |
| Files | `GET /files` (shares with access), `GET /files/{share}/list?path=&offset=`, `GET …/download?path=`, `POST …/mkdir\|rename\|move\|delete`, `PUT …/upload?path=&name=&overwrite=1` (raw body, Content-Length, max 2 at once) | user | `fs.*` (ADR 0009) | implemented (copy jobs M4) |
| Logs | `GET /logs?category=&severity=&since=&q=&limit=` | admin | `journal.query` | M5 |
| Notifications | `GET /notifications`, `POST /notifications/{id}/read` | viewer | – | M4 |
| Security | `GET /security/sessions`, `DELETE /security/sessions/{id}`, `GET /security/advisor` | admin | – | M5 |
| | `GET/PUT /security/firewall`, `POST …/confirm` | admin | `firewall.*` | M5 |
| Updates | `GET /updates`, `POST /updates/check`, `POST /updates/apply` (job) | admin | `apt.*`, `self.update`, `kernel.write` | M5 |
| Integration | `GET /integration/status` (compact, never wakes disks) | token `read` | – | M6 |
| | `GET/POST/DELETE /tokens` | admin | – | M6 |
| Jobs | `GET /jobs/{id}`, `POST /jobs/{id}/cancel` | owner of job | – | M1 |

## Helper protocol

As ADR 0003: one request per connection `{"op", "args", "actor": {"user", "role"}, "confirmToken"?}`
→ `{"ok", "result"} | {"ok": false, "error": {"code", "message"}}`. The helper re-checks the role
for the op (the web process passes the actor, the helper trusts only the peer uid and validates the
role value against the op's minimum). Long ops return a job handle that the web polls via
`job.status`.

## Versioning

Additive changes (new fields, endpoints) stay in v1. Removing or changing meaning of a field needs
`/api/v2` with v1 kept until the UI no longer uses it. The UI and backend ship together in one
binary, so external clients (M6) are the reason for compatibility, not the UI.
