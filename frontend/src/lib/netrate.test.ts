import { describe, expect, it } from 'vitest';
import { bytesRate, formatRate } from './netrate';

const n = (s: string) => s.replace(/ | /g, ' ');

describe('bytesRate', () => {
  it('computes bytes per second', () => expect(bytesRate(1000, 11000, 5000)).toBe(2000));
  it('returns null without a previous sample', () => expect(bytesRate(null, 10, 5000)).toBeNull());
  it('returns null for missing current value', () => expect(bytesRate(10, null, 5000)).toBeNull());
  it('returns null on counter reset', () => expect(bytesRate(500, 100, 5000)).toBeNull());
  it('returns null for non-positive interval', () => expect(bytesRate(1, 2, 0)).toBeNull());
  it('is zero for an idle link', () => expect(bytesRate(7, 7, 5000)).toBe(0));
});

describe('formatRate', () => {
  it('formats KB/s and MB/s', () => {
    expect(n(formatRate(512))).toBe('0,5 KB/s');
    expect(n(formatRate(150 * 1024))).toBe('150 KB/s');
    expect(n(formatRate(5 * 1024 * 1024))).toBe('5,0 MB/s');
  });
  it('shows dash for unknown', () => expect(formatRate(null)).toBe('—'));
});
