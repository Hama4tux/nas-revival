import type { SmartAttribute, SmartDetail, SmartLevel } from '../api/types';

const rank: Record<SmartLevel, number> = { crit: 3, warn: 2, info: 1, ok: 0 };

/** The worst level among the attributes; `ok` when there are none. */
export function worstLevel(attrs: SmartAttribute[]): SmartLevel {
  return attrs.reduce<SmartLevel>((w, a) => (rank[a.level] > rank[w] ? a.level : w), 'ok');
}

/**
 * Problems first, then key attributes, then the rest by ID. Without `all`, only
 * key attributes and attributes with a problem are returned.
 */
export function orderAttributes(attrs: SmartAttribute[], all: boolean): SmartAttribute[] {
  const shown = all ? attrs : attrs.filter((a) => a.key || rank[a.level] >= rank.warn);
  return [...shown].sort((a, b) =>
    rank[b.level] - rank[a.level] || Number(b.key) - Number(a.key) || a.id - b.id);
}

/** Percent of a running self-test that is done, or null when none runs. */
export function testDonePercent(d: SmartDetail): number | null {
  const st = d.selfTest;
  if (!st?.running) return null;
  return 100 - (st.remainingPercent ?? 100);
}

/** How many power-on hours ago a self-test ran; null when unknown. */
export function hoursAgo(d: SmartDetail, lifetimeHours: number): number | null {
  if (d.powerOnHours == null || lifetimeHours > d.powerOnHours) return null;
  return d.powerOnHours - lifetimeHours;
}

/** Packed vendor raw values: show the decoded count only when it differs from the raw text. */
export function decodedDiffers(a: SmartAttribute): boolean {
  return String(a.count) !== a.raw.split(' ')[0];
}

/** While a test runs the UI refreshes progress; otherwise it does not poll. */
export const pollMs = (d: SmartDetail | null): number | null => (d?.selfTest?.running ? 30_000 : null);
