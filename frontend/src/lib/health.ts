import type { Overview, Smart } from '../api/types';
import { ratioPercent } from './format';

export type HealthLevel = 'ok' | 'warning' | 'problem';
export type HealthCode =
  | 'sensorsFault' | 'sensorsUnknown' | 'fanStopped' | 'smartFailed'
  | 'fsCritical' | 'fsWarn' | 'serviceFailed' | 'stale';
export interface HealthReason { level: 'warning' | 'problem'; code: HealthCode; arg?: string }
export interface Health { level: HealthLevel; reasons: HealthReason[] }

export const FS_WARN = 85;
export const FS_CRIT = 95;

/** Derive overall system health from the overview, SMART summary and staleness flag. */
export function deriveHealth(o: Overview, smart: Smart | null, stale: boolean): Health {
  const reasons: HealthReason[] = [];
  const add = (level: HealthReason['level'], code: HealthCode, arg?: string) => reasons.push({ level, code, arg });

  if (o.sensors.status === 'fault') add('problem', 'sensorsFault');
  else if (o.sensors.status !== 'ok') add('warning', 'sensorsUnknown');
  for (const f of o.sensors.fans) if (f.rpm === 0) add('warning', 'fanStopped', f.label);

  for (const d of smart?.disks ?? []) if (!d.standby && d.health === 'failed') add('problem', 'smartFailed', d.name);

  for (const f of o.storage.filesystems) {
    const p = ratioPercent(f.usedBytes, f.totalBytes);
    if (p > FS_CRIT) add('problem', 'fsCritical', f.mount);
    else if (p > FS_WARN) add('warning', 'fsWarn', f.mount);
  }

  for (const s of o.services) if (s.state === 'failed') add('problem', 'serviceFailed', s.label);
  if (stale) add('warning', 'stale');

  reasons.sort((a, b) => (a.level === b.level ? 0 : a.level === 'problem' ? -1 : 1));
  const level: HealthLevel = reasons.some((r) => r.level === 'problem') ? 'problem' : reasons.length ? 'warning' : 'ok';
  return { level, reasons };
}
