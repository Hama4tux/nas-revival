export const STALE_AFTER_MS = 15_000;

/** Data is stale when nothing was ever fetched or the last success is older than the limit. */
export function isStale(lastSuccessMs: number | null, nowMs: number, limitMs = STALE_AFTER_MS): boolean {
  return lastSuccessMs == null || nowMs - lastSuccessMs > limitMs;
}

/** Append a sample keeping at most `max` entries; nulls are ignored. */
export function pushSample(history: readonly number[], v: number | null, max = 60): number[] {
  if (v == null || !Number.isFinite(v)) return history as number[];
  const next = [...history, v];
  return next.length > max ? next.slice(next.length - max) : next;
}
