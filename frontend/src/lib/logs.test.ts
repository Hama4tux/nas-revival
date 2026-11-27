import { describe, expect, it } from 'vitest';
import type { LogEntry } from '../api/types';
import { auditActionLabel, buildLogQuery, defaultFilters, exportFilename, exportText, logTone, mergeOlder, oldestTime, presetSince, priorityName, textError } from './logs';

const now = new Date('2026-11-27T12:00:00Z');
const e = (time: string, message: string, extra: Partial<LogEntry> = {}): LogEntry => ({ time, priority: 6, ident: 'sshd', message, ...extra });

describe('presetSince', () => {
  it('maps presets to RFC 3339 and "all" to null', () => {
    expect(presetSince('hour', now)).toBe('2026-11-27T11:00:00.000Z');
    expect(presetSince('day', now)).toBe('2026-11-26T12:00:00.000Z');
    expect(presetSince('week', now)).toBe('2026-11-20T12:00:00.000Z');
    expect(presetSince('all', now)).toBeNull();
  });
});

describe('textError', () => {
  it('accepts 100 characters, rejects 101 and control characters', () => {
    expect(textError('a'.repeat(100))).toBeNull();
    expect(textError('a'.repeat(101))).toBe('long');
    expect(textError('a\nb')).toBe('control');
    expect(textError('  ')).toBeNull();
  });
});

describe('buildLogQuery', () => {
  it('builds the default query', () => {
    const q = new URLSearchParams(buildLogQuery(defaultFilters, { now }));
    expect(Object.fromEntries(q)).toEqual({ category: 'all', priority: '6', since: '2026-11-26T12:00:00.000Z', limit: '200' });
  });
  it('sends unit only for category all and omits empty values', () => {
    expect(new URLSearchParams(buildLogQuery({ ...defaultFilters, unit: 'ssh.service', preset: 'all' }, { now })).get('unit')).toBe('ssh.service');
    expect(new URLSearchParams(buildLogQuery({ ...defaultFilters, category: 'kernel', unit: 'ssh.service' }, { now })).has('unit')).toBe(false);
    expect(new URLSearchParams(buildLogQuery({ ...defaultFilters, preset: 'all' }, { now })).has('since')).toBe(false);
  });
  it('adds until, trimmed text and clamps limit', () => {
    const q = new URLSearchParams(buildLogQuery({ ...defaultFilters, text: ' fail & more ' }, { now, until: '2026-11-27T11:00:00.5Z', limit: 5000 }));
    expect(q.get('text')).toBe('fail & more');
    expect(q.get('until')).toBe('2026-11-27T11:00:00.5Z');
    expect(q.get('limit')).toBe('1000');
  });
  it('audit has no priority and rejects too long text', () => {
    expect(new URLSearchParams(buildLogQuery(defaultFilters, { now, category: 'audit' })).has('priority')).toBe(false);
    expect(() => buildLogQuery({ ...defaultFilters, text: 'x'.repeat(101) }, { now })).toThrow(RangeError);
  });
});

describe('mergeOlder', () => {
  it('drops boundary duplicates and keeps order', () => {
    const a = [e('t3', 'c'), e('t2', 'b')];
    const m = mergeOlder(a, [e('t2', 'b'), e('t2', 'b2'), e('t1', 'a')]);
    expect(m.map((x) => x.message)).toEqual(['c', 'b', 'b2', 'a']);
    expect(oldestTime(m)).toBe('t1');
    expect(oldestTime([])).toBeNull();
  });
});

describe('export', () => {
  it('formats one line per entry', () => {
    const t = exportText([e('2026-11-27T10:00:00Z', 'Accepted\nkey', { pid: 42, priority: 3 }), e('2026-11-27T09:00:00Z', 'x', { ident: '' })]);
    expect(t).toBe('2026-11-27T10:00:00Z err sshd[42]: Accepted key\n2026-11-27T09:00:00Z info -: x\n');
    expect(exportText([])).toBe('');
  });
  it('names the file by local date and time', () => {
    expect(exportFilename(new Date(2026, 10, 7, 9, 5))).toBe('nas-revival-log-20261107-0905.txt');
  });
});

describe('labels and tones', () => {
  it('maps priority names and tones', () => {
    expect(priorityName(0)).toBe('emerg');
    expect(priorityName(7)).toBe('debug');
    expect(priorityName(9)).toBe('9');
    expect(logTone(3)).toBe('danger');
    expect(logTone(4)).toBe('warning');
    expect(logTone(6)).toBe('neutral');
  });
  it('shows known actions in Czech and unknown raw', () => {
    expect(auditActionLabel('login', { login: 'Přihlášení' })).toBe('Přihlášení');
    expect(auditActionLabel('new.thing', { login: 'x' })).toBe('new.thing');
    expect(auditActionLabel('constructor', {})).toBe('constructor');
  });
});
