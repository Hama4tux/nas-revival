import { describe, expect, it } from 'vitest';
import type { HardwarePoint, HardwareThresholds } from '../api/types';
import { historySince, maxSeries, mergeHardwareHistory, tempLevel, thresholdError } from './hardware';

const pt = (t: number, temps: Record<string, number>): HardwarePoint => ({ t, temps, fanRpm: null, pwmPercent: null });
const th: HardwareThresholds = { boardWarnC: 55, boardCritC: 65, diskWarnC: 50, diskCritC: 55 };

describe('hardware history', () => {
  it('replaces the running minute and asks from the last closed one', () => {
    const a = [pt(0, { x: 40 }), pt(60, { x: 41 }), pt(120, { x: 42 })];
    expect(historySince(a)).toBe(60);
    const m = mergeHardwareHistory(a, [pt(120, { x: 45 }), pt(180, { x: 43 })]);
    expect(m.map((p) => p.t)).toEqual([0, 60, 120, 180]);
    expect(m[2]!.temps.x).toBe(45);
    expect(historySince([pt(0, {})])).toBe(0);
    expect(mergeHardwareHistory(a, [])).toBe(a);
  });

  it('takes the hottest sensor of each point and skips gaps', () => {
    expect(maxSeries([pt(0, { a: 40, b: 44 }), pt(60, {}), pt(120, { a: 41 })], ['a', 'b'])).toEqual([44, 41]);
  });
});

describe('thresholds', () => {
  it('levels board and disk sensors by their own limits; a sleeping disk is ok', () => {
    expect(tempLevel({ id: 'b', label: '', kind: 'board', c: 55 }, th)).toBe('warn');
    expect(tempLevel({ id: 'b', label: '', kind: 'board', c: 54.9 }, th)).toBe('ok');
    expect(tempLevel({ id: 'd', label: '', kind: 'disk', c: 55 }, th)).toBe('crit');
    expect(tempLevel({ id: 'd', label: '', kind: 'disk', c: null }, th)).toBe('ok');
  });

  it('mirrors the NAS validation', () => {
    expect(thresholdError(th)).toBeNull();
    expect(thresholdError({ ...th, boardWarnC: 39 })).toBe('boardWarnC');
    expect(thresholdError({ ...th, boardCritC: 55 })).toBe('boardCritC');
    expect(thresholdError({ ...th, diskWarnC: 50.5 })).toBe('diskWarnC');
    expect(thresholdError({ ...th, diskCritC: 71 })).toBe('diskCritC');
  });
});
