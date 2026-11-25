import type { ActivityDisk, ActivityHint, StorageDisk } from '../api/types';
import { diskTitle } from './storage';

export const DURATIONS = [15, 30, 45] as const;

/** Bytes per minute extrapolated from a window of `seconds`. */
export function perMinute(bytes: number, seconds: number): number {
  return seconds > 0 ? Math.round((bytes * 60) / seconds) : 0;
}

/** Disks that worked during the measurement first, then by name. */
export function busyFirst(disks: ActivityDisk[]): ActivityDisk[] {
  const busy = (d: ActivityDisk) => d.readOps + d.writeOps > 0;
  return [...disks].sort((a, b) => Number(busy(b)) - Number(busy(a)) || a.name.localeCompare(b.name));
}

/** Title from the storage report (model), falling back to the kernel name. */
export function activityTitle(d: ActivityDisk, storage: StorageDisk[] | null): string {
  const s = storage?.find((x) => x.id === d.id || x.name === d.name);
  return s ? diskTitle(s) : d.name;
}

/** Warnings first (things to act on), then plain findings. */
const severity: Record<string, number> = { no_standby: 0, writes_wake: 0, shared_data: 0, swap: 1, journald: 1, fs_journal: 2, unattributed: 2, idle: 3 };
export function sortHints(h: ActivityHint[]): ActivityHint[] {
  return [...h].sort((a, b) => (severity[a.code] ?? 2) - (severity[b.code] ?? 2));
}
export const isWarning = (h: ActivityHint) => (severity[h.code] ?? 2) === 0;

/** Partitions that did any I/O, or that are mounted/swap (to show where nothing happened). */
export function relevantPartitions(d: ActivityDisk) {
  return d.partitions.filter((p) => p.readOps + p.writeOps > 0 || p.mount !== '' || p.swap);
}
