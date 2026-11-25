import { describe, expect, it } from 'vitest';
import type { DiskPower } from '../api/types';
import { apmKind, changed, effective, needsConfirm, parseApm, standbyLabel } from './power';

const view = (p: Partial<DiskPower> = {}): DiskPower => ({
  diskId: 'wwn-0x1', systemDisk: true, state: 'active', checkedAt: '2026-11-25T20:00:00Z', apmSupported: true, apmLevel: 254,
  settings: { profile: 'balanced', standbyMinutes: 0, apm: 254 }, stored: true, standbyChoices: [0, 10, 20, 30, 60],
  profiles: [{ profile: 'performance', standbyMinutes: 0, apm: 254 }, { profile: 'balanced', standbyMinutes: 0, apm: 254 },
    { profile: 'eco', standbyMinutes: 20, apm: 128 }], ...p,
});

describe('power form', () => {
  it('parses the APM field', () => {
    expect(parseApm('')).toBeNull();
    expect(parseApm(' 200 ')).toBe(200);
    expect(parseApm('0')).toBe('invalid');
    expect(parseApm('256')).toBe('invalid');
    expect(parseApm('12a')).toBe('invalid');
  });
  it('takes fixed profile values from the server and custom ones from the form', () => {
    expect(effective(view(), { profile: 'eco', standbyMinutes: 300, apm: '' })).toEqual({ profile: 'eco', standbyMinutes: 20, apm: 128 });
    expect(effective(view(), { profile: 'custom', standbyMinutes: 30, apm: '' })).toEqual({ profile: 'custom', standbyMinutes: 30, apm: null });
    expect(effective(view(), { profile: 'custom', standbyMinutes: 30, apm: 'x' })).toBeNull();
  });
  it('asks before spinning down the system disk only', () => {
    const eco = effective(view(), { profile: 'eco', standbyMinutes: 0, apm: '' });
    expect(needsConfirm(view(), eco)).toBe(true);
    expect(needsConfirm(view({ systemDisk: false }), eco)).toBe(false);
    expect(needsConfirm(view(), view().profiles[0] ?? null)).toBe(false);
  });
  it('treats the unsaved default as a change', () => {
    const same = { profile: 'balanced' as const, standbyMinutes: 0, apm: 254 };
    expect(changed(view(), same)).toBe(false);
    expect(changed(view({ stored: false }), same)).toBe(true);
  });
  it('labels timeouts and APM levels', () => {
    expect(standbyLabel(0)).toBe('nikdy');
    expect(standbyLabel(20)).toBe('20 min');
    expect(standbyLabel(120)).toBe('2 h');
    expect([apmKind(254), apmKind(128), apmKind(127), apmKind(255), apmKind(null)]).toEqual(['max', 'park', 'spindown', 'off', null]);
  });
});
