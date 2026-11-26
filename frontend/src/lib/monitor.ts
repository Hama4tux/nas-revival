import type { MonitorCpu, MonitorHistory, MonitorSample } from '../api/types';

/** One hour at the server's 10 s interval. */
export const HISTORY_SAMPLES = 360;

/** Query for the next poll: only samples newer than the last one we have. */
export const monitorPath = (h: MonitorHistory | null): string => {
  const last = h?.samples[h.samples.length - 1];
  return last ? `/monitor?since=${last.t}` : '/monitor';
};

/**
 * Append a poll result. Samples are deduplicated by time and the oldest are
 * dropped beyond `max`; totals and capabilities come from the newer response.
 * When the server restarted (its newest sample is older than ours), start over.
 */
export function mergeHistory(prev: MonitorHistory | null, next: MonitorHistory, max = HISTORY_SAMPLES): MonitorHistory {
  if (!prev) return { ...next, samples: next.samples.slice(-max) };
  const lastT = prev.samples[prev.samples.length - 1]?.t ?? -Infinity;
  const nextLast = next.samples[next.samples.length - 1]?.t;
  if (nextLast != null && nextLast < lastT) return { ...next, samples: next.samples.slice(-max) };
  const added = next.samples.filter((s) => s.t > lastT);
  return { ...next, samples: [...prev.samples, ...added].slice(-max) };
}

/** CPU time not idle, in percent. */
export const cpuBusy = (c: MonitorCpu): number => Math.round((c.user + c.system + c.iowait + c.irq + c.steal) * 10) / 10;

/** Names of disks or interfaces seen in any sample, in first-seen order. */
export function names(samples: MonitorSample[], key: 'disks' | 'net'): string[] {
  const out: string[] = [];
  for (const s of samples) for (const x of s[key]) if (!out.includes(x.name)) out.push(x.name);
  return out;
}

/** Values of one disk/interface over time; a sample without it counts as 0. */
export function seriesOf<K extends 'disks' | 'net'>(
  samples: MonitorSample[], key: K, name: string, pick: (x: MonitorSample[K][number]) => number,
): number[] {
  return samples.map((s) => {
    const x = (s[key] as MonitorSample[K][number][]).find((d) => d.name === name);
    return x ? pick(x) : 0;
  });
}

export const peak = (values: number[]): number => values.reduce((a, v) => (v > a ? v : a), 0);
export const sum = (values: number[]): number => values.reduce((a, v) => a + v, 0);
