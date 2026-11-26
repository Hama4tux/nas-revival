import type { BackupStatus } from '../api/types';

export type BackupBadge = 'ok' | 'failed' | 'warning' | 'never';

/** Badge kind of the last result; skipped and stopped runs are only a warning. */
export function backupBadge(b: Pick<BackupStatus, 'lastResult'>): BackupBadge {
  switch (b.lastResult) {
    case 'ok': return 'ok';
    case 'failed': return 'failed';
    case 'skipped': case 'stopped': return 'warning';
    default: return 'never';
  }
}

/** Czech relative age ("před 3 hodinami") of an ISO time; empty for an unparsable one. */
export function formatAge(iso: string | null, nowMs: number): string {
  const at = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(at)) return '';
  const min = Math.max(0, Math.floor((nowMs - at) / 60000));
  if (min < 1) return 'právě teď';
  if (min < 60) return `před ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return h === 1 ? 'před 1 hodinou' : `před ${h} hodinami`;
  const d = Math.floor(h / 24);
  return d === 1 ? 'před 1 dnem' : `před ${d} dny`;
}
