import { describe, expect, it } from 'vitest';
import { formatBytes, formatPercent, formatUptime, formatTemp, ratioPercent } from './format';
import { isStale, pushSample } from './stale';

const n = (s: string) => s.replace(/ | /g, ' ');

describe('formatBytes', () => {
  it('formats binary units', () => {
    expect(n(formatBytes(0))).toBe('0 B');
    expect(n(formatBytes(1536))).toBe('1,50 KiB');
    expect(n(formatBytes(262144000))).toBe('250 MiB');
    expect(n(formatBytes(1000204886016))).toBe('932 GiB');
  });
  it('shows dash for null', () => {
    expect(formatBytes(null)).toBe('—');
  });
});

describe('formatPercent', () => {
  it('uses decimal comma', () => expect(n(formatPercent(3.5))).toBe('3,5 %'));
  it('null is a dash, not 0', () => expect(formatPercent(null)).toBe('—'));
});

describe('formatUptime', () => {
  it('handles minutes', () => expect(n(formatUptime(300))).toBe('5 min'));
  it('handles hours', () => expect(n(formatUptime(12345))).toBe('3 h 25 min'));
  it('pluralizes days in Czech', () => {
    expect(n(formatUptime(86400 + 3600))).toBe('1 den 1 h');
    expect(n(formatUptime(3 * 86400))).toBe('3 dny 0 h');
    expect(n(formatUptime(5 * 86400 + 7200))).toBe('5 dní 2 h');
  });
  it('null is a dash', () => expect(formatUptime(null)).toBe('—'));
});

describe('misc', () => {
  it('formatTemp', () => expect(n(formatTemp(41000))).toBe('41,0 °C'));
  it('ratioPercent clamps', () => {
    expect(ratioPercent(5, 0)).toBe(0);
    expect(ratioPercent(200, 100)).toBe(100);
  });
});

describe('stale detection', () => {
  it('is stale without any success', () => expect(isStale(null, 1000)).toBe(true));
  it('fresh within 15 s', () => expect(isStale(1000, 16000)).toBe(false));
  it('stale after 15 s', () => expect(isStale(1000, 16001)).toBe(true));
});

describe('pushSample', () => {
  it('caps length and skips null', () => {
    let h: number[] = [];
    for (let i = 0; i < 70; i++) h = pushSample(h, i);
    expect(h).toHaveLength(60);
    expect(h[0]).toBe(10);
    expect(pushSample(h, null)).toBe(h);
  });
});
