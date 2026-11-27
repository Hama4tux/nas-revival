# ADR 0012: Notification centre

- Status: accepted (2026-11-27, W4.5 phase 1)
- Scope: NOTIF-01 (in-UI notification centre, read state, dedup); the failure notifications of BKP-11
- Context: the health evaluation (MON-01, `system.Evaluate`) shows what is wrong *now*. A backup that failed at
  05:00 and succeeded at 05:30, or a disk that ran hot overnight, leaves no trace in the UI. The hardware watcher
  (HW-01) only writes journal lines. ARCHITECTURE planned `/var/lib/nas-revival/notifications.json`.

## Decision

- **Source = health problem transitions**, nothing new to detect. The shared collector already evaluates health
  every 5 s (open browser) or 60 s (idle); after each successful collection the centre compares the problems with
  its open entries: a new problem opens an entry, a vanished one is resolved. Every condition that health knows
  (failed or stale backup, failed service, SMART, temperatures, full volumes, fan, disk wear) is a notification
  without extra code, and a new problem code becomes one automatically.
- **Key = problem code + subject.** A warning that turns critical changes code (`tempWarn` → `tempCrit`), so it
  resolves the warning entry and opens a new one; a severity change under the same code marks it unread again.
- **Dedup (matrix: same event within 1 h = one entry):** a problem that comes back within 1 h after it was
  resolved reopens its entry (`count` + 1, read state kept unless it got worse) instead of a new one.
- **Unknown is not resolved:** `collectFailed` is not a notification, a failed collection does not update the
  centre, and SMART-based problems of a disk in standby (or with no SMART report yet) stay open – otherwise every
  disk sleep would resolve and every wake reopen them.
- **Read state per user** (`readBy` list in the entry), so one administrator reading does not hide news from
  another. The read-only administrator may mark read: it changes only their own view.
- **Storage:** one JSON file in the state directory of the web service (0600, atomic write), written only when
  something changed – no periodic writes that would keep the system disk awake. At most 200 entries; resolved
  entries are dropped after 30 days, then the oldest resolved first; open ones are never dropped. A broken file is
  logged and the centre starts empty (notifications are not worth refusing to start). New entries also go to the
  journal (`notification code=… subject=…`).
- **UI:** a bell in the header (viewer and admin) with the unread count, polled every 30 s while the page is
  visible; the panel lists the newest 50 with the dashboard texts, since/resolved time and repeats.

## Not in phase 1

- Delivery outside the browser (e-mail, webhook, Home Assistant) – NOTIF-02.
- Events that are not health problems: task runs other than backups, logins, config changes (LOG-10 audit).
- Per-notification deletion; retention handles cleanup.
