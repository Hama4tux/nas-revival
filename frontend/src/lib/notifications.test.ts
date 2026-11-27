import { describe, expect, it } from 'vitest';
import type { NotificationItem } from '../api/types';
import { notificationStatus, notificationText, shortTime } from './notifications';

const item: NotificationItem = {
  id: 'a', severity: 'crit', code: 'backupFailed', subject: 'web', firstAt: '2026-11-27T09:05:00',
  updatedAt: '2026-11-27T09:05:00', resolvedAt: null, count: 1, read: false,
};
const now = new Date('2026-11-27T12:00:00');

describe('notifications', () => {
  it('uses the dashboard texts', () => {
    expect(notificationText(item)).toBe('Záloha selhala: web');
  });
  it('shows only the time for today, the date otherwise', () => {
    expect(shortTime('2026-11-27T09:05:00', now)).toBe('09:05');
    expect(shortTime('2026-11-25T09:05:00', now)).toMatch(/^25\. ?11\. 09:05$/);
    expect(shortTime('x', now)).toBe('');
  });
  it('describes open, resolved and repeated entries', () => {
    expect(notificationStatus(item, now)).toBe('Trvá od 09:05');
    expect(notificationStatus({ ...item, resolvedAt: '2026-11-27T10:00:00', count: 3 }, now)).toBe('Vyřešeno 10:00 · objevilo se 3×');
  });
});
