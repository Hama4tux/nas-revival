import { describe, expect, it } from 'vitest';
import type { ConfigDiffLine, ConfigHistory } from '../api/types';
import { compactDiff, diffStats, driftedFiles, filesToRestore } from './config';

const l = (op: ' ' | '-' | '+', text: string): ConfigDiffLine => ({ op, text });

describe('config history helpers', () => {
  it('counts added and removed lines', () => {
    expect(diffStats([l(' ', 'a'), l('-', 'b'), l('+', 'c'), l('+', 'd')])).toEqual({ added: 2, removed: 1 });
    expect(diffStats(null)).toEqual({ added: 0, removed: 0 });
  });

  it('keeps context around changes and folds long unchanged runs', () => {
    const lines = [l(' ', '1'), l(' ', '2'), l(' ', '3'), l(' ', '4'), l('-', 'x'), l('+', 'y'), l(' ', '5'), l(' ', '6'), l(' ', '7')];
    expect(compactDiff(lines, 1)).toEqual([null, l(' ', '4'), l('-', 'x'), l('+', 'y'), l(' ', '5'), null]);
    expect(compactDiff([l(' ', 'a')], 2)).toEqual([null]);
  });

  it('lists only differing files and drifted files', () => {
    expect(filesToRestore([
      { id: 'network', path: '/n', same: true, present: true, currentPresent: true, lines: null, tooLarge: false },
      { id: 'time', path: '/t', same: false, present: false, currentPresent: true, lines: [], tooLarge: false },
    ]).map((f) => f.id)).toEqual(['time']);
    const h: ConfigHistory = { entries: [], maxEntries: 20, maxBytes: 1, usedBytes: 0,
      drift: [{ id: 'network', path: '/n', state: 'pending' }, { id: 'time', path: '/t', state: 'modified' }] };
    expect(driftedFiles(h).map((d) => d.id)).toEqual(['time']);
  });
});
