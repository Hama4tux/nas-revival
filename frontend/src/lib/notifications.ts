import type { NotificationItem } from '../api/types';
import { cs } from '../i18n/cs';

/** Same Czech sentence as on the dashboard health panel. */
export function notificationText(n: Pick<NotificationItem, 'code' | 'subject'>): string {
  const f = cs.overview.reasons[n.code];
  return f ? f(n.subject) : n.code;
}

/** "27. 11. 10:05", or just the time for today. */
export function shortTime(iso: string, now = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const time = d.toLocaleTimeString('cs-CZ', { hour: '2-digit', minute: '2-digit' });
  return d.toDateString() === now.toDateString()
    ? time
    : `${d.toLocaleDateString('cs-CZ', { day: 'numeric', month: 'numeric' })} ${time}`;
}

/** Status line under the text: since when / resolved when, and repeats. */
export function notificationStatus(n: NotificationItem, now = new Date()): string {
  const t = cs.notify;
  const parts = [n.resolvedAt ? t.resolved(shortTime(n.resolvedAt, now)) : `${t.active} ${t.since(shortTime(n.firstAt, now))}`];
  if (n.count > 1) parts.push(t.repeated(n.count));
  return parts.join(' · ');
}
