# UI navigation

Visual direction: inspired by Synology DSM (launcher, coloured app icons, desktop-like dashboard),
but simpler. Existing UI ([frontend/](../frontend/README.md)): dark default + light theme, sidebar on
desktop, tile launcher on mobile, Czech texts in `src/i18n/cs.ts`, apps registered in
`src/components/nav.ts` with `ready` flag (unready = disabled tile).

## Information architecture

The web plan lists 15 applications. To keep the menu short on a single-bay NAS, they are grouped
into **10 apps**; the rest are sections inside them. Each app has *Basic* tabs and a collapsed
*Advanced* section.

| # | App (`nav` key) | Contains (plan modules) | Milestone | Today |
|---|---|---|---|---|
| 1 | Overview (`overview`) | Dashboard | – | ready |
| 2 | Storage (`storage`) | Storage Manager, SMART, Disk power, Activity diagnostics, USB storage | M1 | disks tab ready (W1.3), SMART detail and self-tests per disk (W1.4) |
| 3 | Shared folders (`shares`) | Shared Folder Management, recycle bin | M2 | tile exists, not ready |
| 4 | Users & groups (`users`) | User & Group Management, roles | M2 | tile exists, not ready |
| 5 | Services (`services`) | Service Manager + File Services (SMB, NFS, SSH/SFTP, FTP, discovery) as tabs | M2 | tile exists, not ready |
| 6 | Network (`network`) | Network Manager, diagnostics | in development | ready (settings) |
| 7 | Monitor (`monitor`) | Resource Monitor, process manager | M3 | – |
| 8 | Files (`files`) | File Station | M4 | – |
| 9 | Backup (`backup`) | Backup & Restore, config export, task scheduler tab | M3–M4 | – |
| 10 | Control panel (`system`) | System info, power, Hardware & Power (fan, LEDs, schedule), Security Center, Update Manager, Log Center tab, Package Center (M6) | M1+ | ready (system page) |

`logs` stays a separate tile (Log center) because it is used often from notifications; Task
Scheduler sits in Control panel → Tasks and is linked from Backup and Storage (SMART schedule).

## Dashboard (Overview)

Fixed widget order, compact, no technical detail beyond one line per item:

1. **System health** bar: ok/warning/critical/unknown, problem list (click → app), `checked at`.
2. **Resources:** CPU %, load, RAM used/available, swap, uptime.
3. **Storage:** per disk – capacity bar, SMART state, power state (active/standby/unknown), temp.
4. **Network:** address, link speed, live up/down chart.
5. **Services:** main services with coloured dot; count of unexpectedly stopped.
6. **Hardware:** temperatures, fan RPM (only wired fan).
7. **Events:** last 5 events (boot, service changes, later notifications).
8. **Backup / updates** (M4/M5): last backup result, pending updates.
9. **Quick actions:** restart, shut down (with preflight), open notifications.

All widgets read one `GET /overview` response every 5 s while the tab is visible; nothing polls on
its own. When hidden, polling stops (implemented).

## Common UI rules

- Status vocabulary and colours are identical everywhere: OK (green), Warning (amber), Critical
  (red), Unknown (grey), Unsupported (hidden or grey with reason), Sleeping (blue, disks).
- Unsupported hardware features are hidden; unverified ones are shown disabled with "not verified
  on this device".
- Destructive actions: red button → dialog listing effects from `…/preview` → type the object name
  → confirm. No double-confirm for non-destructive actions.
- Pending changes (network, firewall): full-width banner with countdown and Confirm button
  (`PendingBanner` exists).
- Errors: one sentence what happened + what to do; machine code in small text for support.
- Long operations: inline progress from `GET /jobs/{id}`, cancel when supported.
- Languages: Czech (default) and English (I18N-01); all strings in `i18n/<lang>.ts`, none in components.
- Responsive: phone (≥ 360 px, tiles + stacked cards), tablet, desktop (sidebar); touch targets ≥ 40 px.
- No decorative animation; transitions ≤ 150 ms only for open/close.
- No external fonts/CDNs; icons from bundled lucide.

## Routes

Hash routes (static hosting, no server routing): `#/` overview, `#/storage`, `#/storage/disk/<id>`,
`#/shares`, `#/users`, `#/services/<tab>`, `#/network`, `#/monitor`, `#/files/<share>/<path>`,
`#/backup`, `#/system/<tab>` (info, power, hardware, security, updates, tasks), `#/logs`.
