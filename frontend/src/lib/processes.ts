import type { ProcessInfo } from '../api/types';

export type ProcessSort = 'cpu' | 'memory' | 'name' | 'pid';

/** Case-insensitive match on name, command line, user or PID. */
export function filterProcesses(list: ProcessInfo[], query: string): ProcessInfo[] {
  const q = query.trim().toLowerCase();
  if (!q) return list;
  return list.filter((p) => String(p.pid) === q || p.name.toLowerCase().includes(q)
    || p.cmd.toLowerCase().includes(q) || p.user.toLowerCase().includes(q));
}

/** Biggest first for CPU and memory, A–Z / ascending for name and PID; ties by PID. */
export function sortProcesses(list: ProcessInfo[], by: ProcessSort): ProcessInfo[] {
  const cmp: Record<ProcessSort, (a: ProcessInfo, b: ProcessInfo) => number> = {
    cpu: (a, b) => b.cpuPercent - a.cpuPercent || b.rssBytes - a.rssBytes,
    memory: (a, b) => b.rssBytes - a.rssBytes,
    name: (a, b) => a.name.localeCompare(b.name, 'cs'),
    pid: () => 0,
  };
  return [...list].sort((a, b) => cmp[by](a, b) || a.pid - b.pid);
}

/** Kernel threads are hidden unless asked for: dozens of rows nobody can act on. */
export const userProcesses = (list: ProcessInfo[], withKernel: boolean): ProcessInfo[] =>
  withKernel ? list : list.filter((p) => p.protected !== 'kernel');

export const killPath = (pid: number) => `/monitor/processes/${pid}/kill`;
