import type { UpdatePackage, UpdateRun } from '../api/types';

/** Security updates first, then by name. */
export const sortPackages = (p: UpdatePackage[]): UpdatePackage[] =>
  [...p].sort((a, b) => Number(b.security) - Number(a.security) || a.name.localeCompare(b.name));

/** Percent of the packages set up; a running upgrade never shows 100 % (dpkg triggers run after the last package). */
export function upgradePercent(u: UpdateRun): number {
  if (u.result !== 'running') return 100;
  if (u.total <= 0) return 0;
  return Math.min(99, Math.round((u.done / u.total) * 100));
}

/** Days since the last successful check; null when never checked. */
export function daysSince(iso: string | undefined, now: number): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : Math.max(0, Math.floor((now - t) / 86_400_000));
}

/** Czech plural: 1 balíček, 2–4 balíčky, 5+ balíčků. */
export const plural = (n: number, one: string, few: string, many: string) => (n === 1 ? one : n >= 2 && n <= 4 ? few : many);
