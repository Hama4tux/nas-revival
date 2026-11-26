import { describe, expect, it } from 'vitest';
import type { Overview } from '../api/types';
import { deriveEvents } from './events';

const base = (services: Overview['services'], uptime: number | null = 3600): Overview => ({
  generatedAt: '2026-11-25T16:00:00Z',
  host: { hostname: 'nas', os: 'Debian', kernel: '6.12', uptimeSeconds: uptime, boardModel: 'X' },
  cpu: { model: '', cores: 1, load1: null, load5: null, load15: null, usagePercent: null },
  memory: { totalBytes: 1, availableBytes: 1, swapTotalBytes: 0, swapFreeBytes: 0 },
  sensors: { status: 'ok', temperatures: [], fans: [] },
  network: [], storage: { disks: [], filesystems: [] }, services, backups: [],
  health: { status: 'ok', checkedAt: '2026-11-25T16:00:00Z', smartCheckedAt: null, problems: [] },
});

describe('deriveEvents', () => {
  it('orders service changes and boot newest first', () => {
    const ev = deriveEvents(base([
      { name: 'a', label: 'SSH', state: 'active', since: '2026-11-25T15:10:00Z' },
      { name: 'b', label: 'Samba', state: 'failed', since: '2026-11-25T15:50:00Z' },
      { name: 'c', label: 'LED', state: 'done', since: null },
      { name: 'd', label: 'NFS', state: 'inactive' },
    ]));
    expect(ev.map((e) => [e.kind, e.label])).toEqual([['failed', 'Samba'], ['active', 'SSH'], ['boot', 'nas']]);
    expect(ev[2]?.at).toBe(Date.parse('2026-11-25T15:00:00Z'));
  });

  it('puts boot after services started at the same moment and respects the limit', () => {
    const at = '2026-11-25T15:00:00Z';
    const ev = deriveEvents(base([
      { name: 'a', label: 'A', state: 'active', since: at },
      { name: 'b', label: 'B', state: 'active', since: at },
    ]), 2);
    expect(ev.map((e) => e.label)).toEqual(['A', 'B']);
  });

  it('skips boot when uptime is unknown', () => {
    expect(deriveEvents(base([], null))).toEqual([]);
  });
});
