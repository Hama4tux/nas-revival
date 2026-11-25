import type { HealthProblem, Overview } from '../api/types';

export type HealthLevel = 'ok' | 'warning' | 'problem' | 'unknown';
export type HealthCode = HealthProblem['code'] | 'stale';
export interface HealthReason { level: 'warning' | 'problem'; code: HealthCode; arg?: string }
export interface Health { level: HealthLevel; reasons: HealthReason[]; checkedAt: string }

const levelOf = { ok: 'ok', warn: 'warning', crit: 'problem', unknown: 'unknown' } as const;

/**
 * Map the health evaluated on the NAS to the hero panel. Only staleness is
 * judged here, because only the browser knows its own connection.
 */
export function deriveHealth(o: Overview, stale: boolean): Health {
  const reasons: HealthReason[] = o.health.problems.map((p) => ({
    level: p.severity === 'crit' ? 'problem' : 'warning', code: p.code, arg: p.subject,
  }));
  let level: HealthLevel = levelOf[o.health.status] ?? 'unknown';
  if (stale) {
    reasons.push({ level: 'warning', code: 'stale' });
    if (level === 'ok') level = 'warning';
  }
  return { level, reasons, checkedAt: o.health.checkedAt };
}
