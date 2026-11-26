import { describe, expect, it } from 'vitest';
import type { MonitorHistory, MonitorSample } from '../api/types';
import { cpuBusy, mergeHistory, monitorPath, names, peak, seriesOf } from './monitor';

const sample = (t: number, disks: string[] = ['sda']): MonitorSample => ({
  t, cpu: { user: 10, system: 5, iowait: 2.5, irq: 0.5, steal: 0 },
  memory: { usedBytes: 1, cacheBytes: 2, availableBytes: 3, swapUsedBytes: 0, swapInBps: 0, swapOutBps: 0 },
  psi: null, disks: disks.map((name) => ({ name, readBps: t, writeBps: 0, readIops: 0, writeIops: 0, busyPercent: 0, awaitMs: 0 })), net: [],
});
const hist = (ts: number[], total = 100): MonitorHistory => ({
  intervalSeconds: 10, psiAvailable: false, memTotalBytes: total, swapTotalBytes: 0, netTotals: [], samples: ts.map((t) => sample(t)),
});

describe('monitor history', () => {
  it('asks only for newer samples', () => {
    expect(monitorPath(null)).toBe('/monitor');
    expect(monitorPath(hist([]))).toBe('/monitor');
    expect(monitorPath(hist([10, 20]))).toBe('/monitor?since=20');
  });

  it('appends, deduplicates and trims', () => {
    const m = mergeHistory(hist([10, 20, 30]), hist([30, 40, 50], 200), 4);
    expect(m.samples.map((s) => s.t)).toEqual([20, 30, 40, 50]);
    expect(m.memTotalBytes).toBe(200);
    expect(mergeHistory(hist([10]), hist([])).samples.map((s) => s.t)).toEqual([10]);
  });

  it('starts over after a server restart', () => {
    expect(mergeHistory(hist([100, 110]), hist([5, 15])).samples.map((s) => s.t)).toEqual([5, 15]);
  });

  it('derives series per disk', () => {
    const s = [sample(1, ['sda']), sample(2, ['sda', 'sdb']), sample(3, ['sdb'])];
    expect(names(s, 'disks')).toEqual(['sda', 'sdb']);
    expect(seriesOf(s, 'disks', 'sdb', (d) => d.readBps)).toEqual([0, 2, 3]);
    expect(peak([1, 5, 2])).toBe(5);
    expect(cpuBusy(s[0]!.cpu)).toBe(18);
  });
});
