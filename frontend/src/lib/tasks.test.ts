import { describe, expect, it } from 'vitest';
import type { BackupParams, TaskDef } from '../api/types';
import { MAX_POWER_TASKS, authorizedLine, formatDuration, isPowerTask, newBackup, listError, newTask, nextRunLabel, normalizeTask, pollInterval, scheduleLabel, taskOk, validateTask } from './tasks';

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

const bk = (p: Partial<BackupParams> = {}, t: Partial<TaskDef> = {}): TaskDef => ({
  id: '', name: 'Záloha', type: 'backup-pull', enabled: true, schedule: { kind: 'daily', time: '06:45' },
  backup: { host: '192.0.2.10', port: 22, user: 'root', paths: ['nas-export', 'hrcm'], keepDays: 35,
    fresh: { file: 'nas-export/last-ok', maxAgeHours: 26 }, bwlimitKiB: 0, excludes: ['*.tmp'], ...p }, ...t,
});

describe('backup-pull validation', () => {
  it('accepts a valid task', () => {
    expect(validateTask(bk())).toEqual({});
    expect(taskOk(bk({ host: 'backup.example.lan', fresh: undefined, excludes: [] }))).toBe(true);
    expect(taskOk(bk({ host: '2001:db8::10' }))).toBe(true);
    expect(taskOk(bk({ port: 0 }))).toBe(true);
  });
  it('rejects bad hosts', () => {
    for (const h of ['-oProxyCommand=x', '', 'a b', 'a;b', 'host_name', `${'a'.repeat(64)}.lan`, 'x'.repeat(254)]) expect(validateTask(bk({ host: h })).host, h).toBeTruthy();
  });
  it('checks port and user', () => {
    expect(validateTask(bk({ port: 65536 })).port).toBeTruthy();
    expect(validateTask(bk({ port: 1.5 })).port).toBeTruthy();
    expect(validateTask(bk({ port: 65535 })).port).toBeUndefined();
    expect(validateTask(bk({ user: 'Root' })).user).toBeTruthy();
    expect(validateTask(bk({ user: '-x' })).user).toBeTruthy();
    expect(validateTask(bk({ user: 'a'.repeat(33) })).user).toBeTruthy();
  });
  it('checks paths', () => {
    for (const p of ['../x', 'a/../b', './a', 'a//b', '/abs', 'a b', 'a/', '']) expect(validateTask(bk({ paths: [p], fresh: undefined })).paths, p).toBeTruthy();
    expect(validateTask(bk({ paths: [], fresh: undefined })).paths).toBeTruthy();
    expect(validateTask(bk({ paths: ['a', 'a'], fresh: undefined })).paths).toBeTruthy();
    expect(validateTask(bk({ paths: Array.from({ length: 9 }, (_, i) => `p${i}`), fresh: undefined })).paths).toBeTruthy();
    expect(validateTask(bk({ paths: [`${'a'.repeat(129)}`], fresh: undefined })).paths).toBeTruthy();
    expect(validateTask(bk({ paths: Array.from({ length: 8 }, (_, i) => `p${i}`), fresh: undefined })).paths).toBeUndefined();
  });
  it('checks keepDays and bandwidth', () => {
    expect(validateTask(bk({ keepDays: 0 })).keepDays).toBeTruthy();
    expect(validateTask(bk({ keepDays: 91 })).keepDays).toBeTruthy();
    expect(validateTask(bk({ keepDays: 1 })).keepDays).toBeUndefined();
    expect(validateTask(bk({ keepDays: 90 })).keepDays).toBeUndefined();
    expect(validateTask(bk({ bwlimitKiB: -1 })).bwlimit).toBeTruthy();
    expect(validateTask(bk({ bwlimitKiB: 1_000_001 })).bwlimit).toBeTruthy();
    expect(validateTask(bk({ bwlimitKiB: 1_000_000 })).bwlimit).toBeUndefined();
  });
  it('checks the freshness file', () => {
    expect(validateTask(bk({ fresh: { file: 'other/last-ok', maxAgeHours: 26 } })).freshFile).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-export', maxAgeHours: 26 } })).freshFile).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-export/../x', maxAgeHours: 26 } })).freshFile).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-exportx/a', maxAgeHours: 26 } })).freshFile).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-export/a', maxAgeHours: 0 } })).freshAge).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-export/a', maxAgeHours: 721 } })).freshAge).toBeTruthy();
    expect(validateTask(bk({ fresh: { file: 'nas-export/a', maxAgeHours: 720 } }))).toEqual({});
  });
  it('checks excludes', () => {
    expect(validateTask(bk({ excludes: ['-rf'] })).excludes).toBeTruthy();
    expect(validateTask(bk({ excludes: ['a b'] })).excludes).toBeTruthy();
    expect(validateTask(bk({ excludes: [''] })).excludes).toBeTruthy();
    expect(validateTask(bk({ excludes: ['x'.repeat(101)] })).excludes).toBeTruthy();
    expect(validateTask(bk({ excludes: Array.from({ length: 17 }, () => 'a') })).excludes).toBeTruthy();
    expect(validateTask(bk({ excludes: ['a/[b]?*.tmp'] })).excludes).toBeUndefined();
  });
  it('requires the params and limits the count', () => {
    expect(validateTask(bk({}, { backup: undefined })).host).toBeTruthy();
    expect(listError(Array.from({ length: 9 }, (_, i) => bk({}, { id: `b${i}` })))).toBeTruthy();
    expect(listError(Array.from({ length: 8 }, (_, i) => bk({}, { id: `b${i}` })))).toBeUndefined();
  });
  it('normalizes', () => {
    const n = normalizeTask(bk({ host: ' 192.0.2.10 ', port: 0, paths: [' a ', ''], excludes: [' '], bwlimitKiB: 0 }, { smart: { diskId: 'x', test: 'short' } }));
    expect(n.smart).toBeUndefined();
    expect(n.backup).toMatchObject({ host: '192.0.2.10', port: 22, paths: ['a'], excludes: [] });
    expect(normalizeTask({ ...bk(), type: 'recycle-cleanup' }).backup).toBeUndefined();
    expect(newBackup().paths).toEqual(['']);
  });
});

describe('power tasks', () => {
  const power = (type: 'reboot' | 'shutdown'): TaskDef => ({ ...smart({ name: 'Noční restart' }), type, smart: undefined });
  it('validates without parameters and drops leftovers', () => {
    expect(taskOk(power('reboot'))).toBe(true);
    expect(normalizeTask({ ...power('shutdown'), smart: { diskId: 'x', test: 'short' } }).smart).toBeUndefined();
  });
  it('recognises the power types', () => {
    expect(isPowerTask('reboot')).toBe(true);
    expect(isPowerTask('shutdown')).toBe(true);
    expect(isPowerTask('smart')).toBe(false);
  });
  it('limits their count', () => {
    const l = Array.from({ length: MAX_POWER_TASKS }, () => power('shutdown'));
    expect(listError(l)).toBeUndefined();
    expect(listError([...l, power('reboot')])).toBe('Úloh restartu a vypnutí může být nejvýše 4.');
  });
});

describe('authorizedLine', () => {
  const hint = 'from="192.0.2.5",restrict,command="rrsync -ro /var/backups" ssh-ed25519 AAAA nas-backup@nas';
  it('replaces the folder', () => {
    expect(authorizedLine(hint, '/srv/export')).toContain('rrsync -ro /srv/export"');
  });
  it('refuses unsafe folders', () => {
    expect(authorizedLine(hint, '/x" ; rm')).toBeNull();
    expect(authorizedLine(hint, 'rel')).toBeNull();
    expect(authorizedLine(hint, '/a/../b')).toBeNull();
  });
});
