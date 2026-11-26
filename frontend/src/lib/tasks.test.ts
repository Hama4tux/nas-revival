import { describe, expect, it } from 'vitest';
import type { TaskDef } from '../api/types';
import { formatDuration, listError, newTask, nextRunLabel, normalizeTask, pollInterval, scheduleLabel, taskOk, validateTask } from './tasks';

const smart = (p: Partial<TaskDef> = {}): TaskDef => ({
  id: '', name: 'Test', type: 'smart', enabled: true, schedule: { kind: 'daily', time: '03:30' },
  smart: { diskId: 'ata-EXAMPLE_SERIAL', test: 'long' }, ...p,
});

describe('scheduleLabel', () => {
  it('describes each kind', () => {
    expect(scheduleLabel({ kind: 'daily', time: '03:30' })).toBe('Denně v 03:30');
    expect(scheduleLabel({ kind: 'weekly', time: '03:30', weekdays: ['thu', 'mon'] })).toBe('Po, Čt v 03:30');
    expect(scheduleLabel({ kind: 'monthly', time: '02:00', day: 1 })).toBe('Každý měsíc 1. v 02:00');
  });
});

describe('validateTask', () => {
  it('accepts valid tasks', () => {
    expect(taskOk(smart())).toBe(true);
    expect(taskOk({ ...smart(), type: 'recycle-cleanup', smart: undefined })).toBe(true);
  });
  it('checks the name', () => {
    expect(validateTask(smart({ name: '  ' })).name).toBeTruthy();
    expect(validateTask(smart({ name: 'x'.repeat(41) })).name).toBeTruthy();
    expect(validateTask(smart({ name: 'x'.repeat(40) })).name).toBeUndefined();
    expect(validateTask(smart({ name: 'a\u0007b' })).name).toBeTruthy();
  });
  it('checks time, weekdays and day', () => {
    expect(validateTask(smart({ schedule: { kind: 'daily', time: '24:00' } })).time).toBeTruthy();
    expect(validateTask(smart({ schedule: { kind: 'daily', time: '3:30' } })).time).toBeTruthy();
    expect(validateTask(smart({ schedule: { kind: 'weekly', time: '01:00', weekdays: [] } })).weekdays).toBeTruthy();
    expect(validateTask(smart({ schedule: { kind: 'weekly', time: '01:00', weekdays: ['mon', 'mon'] } })).weekdays).toBeTruthy();
    expect(validateTask(smart({ schedule: { kind: 'monthly', time: '01:00', day: 29 } })).day).toBeTruthy();
    expect(validateTask(smart({ schedule: { kind: 'monthly', time: '01:00', day: 28 } })).day).toBeUndefined();
  });
  it('checks SMART params', () => {
    expect(validateTask(smart({ smart: { diskId: '', test: 'short' } })).diskId).toBeTruthy();
    expect(validateTask(smart({ smart: { diskId: 'a b', test: 'short' } })).diskId).toBeTruthy();
    expect(validateTask(smart({ smart: undefined })).diskId).toBeTruthy();
  });
});

describe('listError', () => {
  it('limits count and duplicate ids', () => {
    expect(listError(Array.from({ length: 33 }, () => smart()))).toBeTruthy();
    expect(listError([smart({ id: 'a' }), smart({ id: 'a' })])).toBeTruthy();
    expect(listError([smart({ id: 'a' }), smart({ id: 'b' }), smart()])).toBeUndefined();
  });
});

describe('normalizeTask', () => {
  it('drops foreign fields and trims', () => {
    const n = normalizeTask({ ...smart({ name: ' A ' }), type: 'recycle-cleanup', schedule: { kind: 'daily', time: '01:00', day: 3, weekdays: ['mon'] } });
    expect(n).toEqual({ id: '', name: 'A', type: 'recycle-cleanup', enabled: true, schedule: { kind: 'daily', time: '01:00' } });
  });
  it('sorts weekdays', () => {
    expect(normalizeTask(smart({ schedule: { kind: 'weekly', time: '01:00', weekdays: ['sun', 'mon'] } })).schedule.weekdays).toEqual(['mon', 'sun']);
  });
  it('newTask is valid once a disk is chosen', () => {
    expect(taskOk({ ...newTask('d1'), name: 'X' })).toBe(true);
  });
});

describe('formatting', () => {
  it('formats durations', () => {
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:42Z')).toBe('42 s');
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:05:03Z')).toBe('5 min 3 s');
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T02:10:00Z')).toBe('2 h 10 min');
    expect(formatDuration('2026-01-01T01:00:00Z', '2026-01-01T00:00:00Z')).toBe('–');
  });
  it('formats next run', () => {
    const now = Date.parse('2026-01-01T00:00:00Z');
    expect(nextRunLabel(null, now)).toBe('–');
    expect(nextRunLabel('2025-12-31T00:00:00Z', now)).toBe('za chvíli');
    expect(nextRunLabel('2026-01-01T00:05:00Z', now)).toMatch(/^za 5 min/);
    expect(nextRunLabel('2026-01-01T03:00:00Z', now)).toMatch(/^za 3 h/);
  });
  it('polls faster while running', () => {
    expect(pollInterval([{ running: false }])).toBe(60_000);
    expect(pollInterval([{ running: false }, { running: true }])).toBe(10_000);
  });
});
