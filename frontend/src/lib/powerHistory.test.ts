import { describe, expect, it } from 'vitest';
import { perDay, recentChanges, timeline } from './powerHistory';

const h = 3600e3;
const now = 100 * h;

describe('power history timeline', () => {
  it('is untracked without events', () => {
    expect(timeline([], now)).toEqual([{ kind: 'untracked', from: now - 24 * h, to: now, percent: 100 }]);
  });
  it('splits the window by state and merges standby with sleeping', () => {
    const s = timeline([
      { t: now - 30 * h, state: 'active' }, { t: now - 12 * h, state: 'standby' },
      { t: now - 9 * h, state: 'sleeping' }, { t: now - 6 * h, state: 'active' },
    ], now);
    expect(s.map((x) => [x.kind, Math.round(x.percent)])).toEqual([['active', 50], ['asleep', 25], ['active', 25]]);
  });
  it('marks the time before the first event as untracked', () => {
    const s = timeline([{ t: now - 6 * h, state: 'standby' }], now);
    expect(s.map((x) => [x.kind, x.percent])).toEqual([['untracked', 75], ['asleep', 25]]);
  });
  it('lists the newest changes first', () => {
    expect(recentChanges([{ t: 1, state: 'active' }, { t: 2, state: 'standby' }, { t: 3, state: 'active' }], 2).map((e) => e.t)).toEqual([3, 2]);
  });
  it('formats daily rates', () => {
    expect(perDay(4.84)).toBe('4,8');
    expect(perDay(288.4)).toBe('288');
  });
});
