import { describe, expect, it } from 'vitest';
import type { ActivityDisk, StorageDisk } from '../api/types';
import { activityTitle, busyFirst, isWarning, perMinute, relevantPartitions, sortHints } from './activity';

const disk = (name: string, writeOps: number, extra: Partial<ActivityDisk> = {}): ActivityDisk => ({
  id: `id-${name}`, name, system: false, readOps: 0, readBytes: 0, writeOps, writeBytes: writeOps * 4096,
  partitions: [], hints: [], ...extra,
});

describe('activity', () => {
  it('extrapolates per minute', () => {
    expect(perMinute(16384, 20)).toBe(49152);
    expect(perMinute(100, 0)).toBe(0);
  });
  it('puts busy disks first', () => {
    expect(busyFirst([disk('sdb', 0), disk('sdc', 3), disk('sda', 1)]).map((d) => d.name)).toEqual(['sda', 'sdc', 'sdb']);
  });
  it('orders hints by severity', () => {
    const h = sortHints([{ code: 'journald' }, { code: 'idle' }, { code: 'no_standby' }, { code: 'fs_journal', value: 'sda1' }]);
    expect(h.map((x) => x.code)).toEqual(['no_standby', 'journald', 'fs_journal', 'idle']);
    expect(isWarning(h[0]!)).toBe(true);
    expect(isWarning(h[1]!)).toBe(false);
  });
  it('titles from storage or kernel name', () => {
    const s = [{ id: 'id-sda', name: 'sda', model: 'ST1000', vendor: null, bus: 'ata', sizeBytes: 1e12 }] as unknown as StorageDisk[];
    expect(activityTitle(disk('sda', 0), s)).toContain('ST1000');
    expect(activityTitle(disk('sdz', 0), s)).toBe('sdz');
    expect(activityTitle(disk('sda', 0), null)).toBe('sda');
  });
  it('hides idle unmounted partitions', () => {
    const p = (name: string, writeOps: number, mount = '', swap = false) =>
      ({ name, parent: 'sda', mount, fsType: '', swap, journalCommits: null, readOps: 0, readBytes: 0, writeOps, writeBytes: 0 });
    const d = disk('sda', 1, { partitions: [p('sda1', 1, '/'), p('sda2', 0, '', true), p('sda3', 0), p('sda4', 0, '/srv')] });
    expect(relevantPartitions(d).map((x) => x.name)).toEqual(['sda1', 'sda2', 'sda4']);
  });
});
