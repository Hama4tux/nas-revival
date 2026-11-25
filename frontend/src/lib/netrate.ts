import { formatNumber } from './format';

export interface NetSample { t: number; rx: number | null; tx: number | null }

/** Bytes per second between two counter readings; null when unknown or the counter was reset. */
export function bytesRate(prev: number | null, cur: number | null, dtMs: number): number | null {
  if (prev == null || cur == null || !(dtMs > 0) || cur < prev) return null;
  return (cur - prev) / (dtMs / 1000);
}

export function formatRate(bps: number | null | undefined): string {
  if (bps == null || !Number.isFinite(bps) || bps < 0) return '—';
  const kb = bps / 1024;
  if (kb >= 1000) return `${formatNumber(kb / 1024, 1)} MB/s`;
  return `${formatNumber(kb, kb < 10 ? 1 : 0)} KB/s`;
}
