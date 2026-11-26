import { describe, expect, it } from 'vitest';
import type { ProcessInfo } from '../api/types';
import { filterProcesses, killPath, sortProcesses, userProcesses } from './processes';

const p = (pid: number, name: string, cpu: number, rss: number, extra: Partial<ProcessInfo> = {}): ProcessInfo => ({
  pid, ppid: 1, name, cmd: name, user: 'root', state: 'S', threads: 1, rssBytes: rss, cpuPercent: cpu, start: pid * 10, startedAt: 0, ...extra,
});
const list = [p(10, 'smbd', 1, 30), p(20, 'rsync', 25, 10, { user: 'nas-backup', cmd: 'rsync -a /srv/data' }),
  p(2, 'kthreadd', 0, 0, { protected: 'kernel' }), p(30, 'Apache', 1, 50)];

describe('processes', () => {
  it('sorts by cpu, memory, name and pid', () => {
    expect(sortProcesses(list, 'cpu').map((x) => x.pid)).toEqual([20, 30, 10, 2]);
    expect(sortProcesses(list, 'memory').map((x) => x.pid)).toEqual([30, 10, 20, 2]);
    expect(sortProcesses(list, 'name').map((x) => x.name)).toEqual(['Apache', 'kthreadd', 'rsync', 'smbd']);
    expect(sortProcesses(list, 'pid').map((x) => x.pid)).toEqual([2, 10, 20, 30]);
  });
  it('filters by name, command, user and exact pid', () => {
    expect(filterProcesses(list, ' RSYNC ').map((x) => x.pid)).toEqual([20]);
    expect(filterProcesses(list, '/srv').map((x) => x.pid)).toEqual([20]);
    expect(filterProcesses(list, 'nas-backup').map((x) => x.pid)).toEqual([20]);
    expect(filterProcesses(list, '2').map((x) => x.pid)).toEqual([2]);
    expect(filterProcesses(list, '')).toHaveLength(4);
  });
  it('hides kernel threads unless asked', () => {
    expect(userProcesses(list, false).map((x) => x.pid)).toEqual([10, 20, 30]);
    expect(userProcesses(list, true)).toHaveLength(4);
  });
  it('builds the kill path', () => expect(killPath(42)).toBe('/monitor/processes/42/kill'));
});
