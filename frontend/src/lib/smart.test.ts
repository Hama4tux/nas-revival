import { describe, expect, it } from 'vitest';
import type { SmartAttribute, SmartDetail } from '../api/types';
import { decodedDiffers, hoursAgo, orderAttributes, pollMs, testDonePercent, worstLevel } from './smart';

const attr = (id: number, p: Partial<SmartAttribute> = {}): SmartAttribute => ({
  id, name: `A${id}`, value: 100, worst: 100, thresh: 0, raw: '0', rawValue: 0, count: 0,
  prefailure: false, whenFailed: '', key: false, level: 'ok', ...p,
});
const detail = (p: Partial<SmartDetail> = {}): SmartDetail => ({
  checkedAt: '2026-11-25T20:00:00Z', standby: false, health: 'passed', model: 'M', family: null,
  temperatureCelsius: 40, powerOnHours: 6870, powerCycles: 10, selfTestsSupported: true, shortMinutes: 1,
  longMinutes: 206, selfTest: null, attributes: [], history: [], ...p,
});

describe('orderAttributes', () => {
  const list = [attr(7), attr(9, { key: true, level: 'info' }), attr(5, { key: true, level: 'info' }),
    attr(199, { key: true, level: 'warn' }), attr(3, { level: 'crit' })];
  it('puts problems first, then key attributes by ID, and hides the rest', () =>
    expect(orderAttributes(list, false).map((a) => a.id)).toEqual([3, 199, 5, 9]));
  it('shows everything on request', () =>
    expect(orderAttributes(list, true).map((a) => a.id)).toEqual([3, 199, 5, 9, 7]));
  it('worst level wins', () => {
    expect(worstLevel(list)).toBe('crit');
    expect(worstLevel([])).toBe('ok');
  });
});

describe('self-test helpers', () => {
  const running = detail({ selfTest: { value: 249, text: 'in progress', running: true, remainingPercent: 90, passed: null } });
  it('turns remaining into done percent', () => {
    expect(testDonePercent(running)).toBe(10);
    expect(testDonePercent(detail())).toBeNull();
  });
  it('polls only while a test runs', () => {
    expect(pollMs(running)).toBe(30_000);
    expect(pollMs(detail())).toBeNull();
    expect(pollMs(null)).toBeNull();
  });
  it('computes hours since a logged test', () => {
    expect(hoursAgo(detail(), 6866)).toBe(4);
    expect(hoursAgo(detail({ powerOnHours: null }), 1)).toBeNull();
    expect(hoursAgo(detail(), 9999)).toBeNull();
  });
  it('flags packed raw values', () => {
    expect(decodedDiffers(attr(7, { raw: '68992411861', count: 16 }))).toBe(true);
    expect(decodedDiffers(attr(194, { raw: '42 (0 6 0 0 0)', count: 42 }))).toBe(false);
  });
});
