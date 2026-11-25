import type { StorageDisk } from '../api/types';

export type DiskWarning = 'sharedWithData' | 'unstableId' | 'readOnly';

/** Warnings shown on a disk card, most important first. */
export function diskWarnings(d: StorageDisk): DiskWarning[] {
  const w: DiskWarning[] = [];
  if (d.sharedWithData) w.push('sharedWithData');
  if (!d.idStable) w.push('unstableId');
  if (d.readOnly) w.push('readOnly');
  return w;
}

/** Display name: vendor + model, falling back to the kernel name. */
export function diskTitle(d: StorageDisk): string {
  const parts = [d.vendor, d.model].filter((s): s is string => !!s);
  return parts.length ? parts.join(' ') : d.name;
}

/** Space actually allocated to partitions; the rest is unpartitioned. */
export function unpartitionedBytes(d: StorageDisk): number {
  if (d.partitions.length === 1 && d.partitions[0]?.name === d.name) return 0; // filesystem on the whole disk
  const used = d.partitions.reduce((a, p) => a + p.sizeBytes, 0);
  const free = d.sizeBytes - used;
  // Alignment gaps and GPT backup headers leave a few MiB; do not report those.
  return free > 16 * 1024 * 1024 ? free : 0;
}
