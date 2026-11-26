import { describe, expect, it } from 'vitest';
import { backupBadge, formatAge } from './backups';

describe('backupBadge', () => {
  it('maps the last result', () => {
    expect(backupBadge({ lastResult: 'ok' })).toBe('ok');
    expect(backupBadge({ lastResult: 'failed' })).toBe('failed');
    expect(backupBadge({ lastResult: 'skipped' })).toBe('warning');
    expect(backupBadge({ lastResult: 'stopped' })).toBe('warning');
    expect(backupBadge({ lastResult: '' })).toBe('never');
  });
});

describe('formatAge', () => {
  const now = Date.parse('2026-11-26T12:00:00Z');
  it('formats minutes, hours and days', () => {
    expect(formatAge('2026-11-26T11:59:30Z', now)).toBe('právě teď');
    expect(formatAge('2026-11-26T11:15:00Z', now)).toBe('před 45 min');
    expect(formatAge('2026-11-26T11:00:00Z', now)).toBe('před 1 hodinou');
    expect(formatAge('2026-11-26T07:00:00Z', now)).toBe('před 5 hodinami');
    expect(formatAge('2026-11-25T11:00:00Z', now)).toBe('před 1 dnem');
    expect(formatAge('2026-11-19T12:00:00Z', now)).toBe('před 7 dny');
  });
  it('is empty without a valid time and never negative', () => {
    expect(formatAge(null, now)).toBe('');
    expect(formatAge('x', now)).toBe('');
    expect(formatAge('2026-11-26T12:05:00Z', now)).toBe('právě teď');
  });
});
