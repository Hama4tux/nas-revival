import { describe, expect, it } from 'vitest';
import { daysSince, plural, sortPackages, upgradePercent } from './updates';

const p = (name: string, security = false) => ({ name, to: '1', origin: '', security, size: 0 });

describe('updates', () => {
  it('sorts security updates first, then by name', () => {
    expect(sortPackages([p('zlib'), p('tzdata'), p('openssl', true), p('libc6', true)]).map((x) => x.name))
      .toEqual(['libc6', 'openssl', 'tzdata', 'zlib']);
  });
  it('caps the progress of a running upgrade', () => {
    const run = { startedAt: '', result: 'running' as const, total: 4, done: 1 };
    expect(upgradePercent(run)).toBe(25);
    expect(upgradePercent({ ...run, done: 6 })).toBe(99);
    expect(upgradePercent({ ...run, total: 0 })).toBe(0);
    expect(upgradePercent({ ...run, result: 'ok' })).toBe(100);
  });
  it('counts days since the check', () => {
    const now = Date.parse('2026-11-27T12:00:00Z');
    expect(daysSince(undefined, now)).toBeNull();
    expect(daysSince('bad', now)).toBeNull();
    expect(daysSince('2026-11-20T11:00:00Z', now)).toBe(7);
    expect(daysSince('2026-11-27T13:00:00Z', now)).toBe(0);
  });
  it('uses Czech plurals', () => {
    expect([1, 2, 5, 0].map((n) => plural(n, 'balíček', 'balíčky', 'balíčků'))).toEqual(['balíček', 'balíčky', 'balíčků', 'balíčků']);
  });
});
