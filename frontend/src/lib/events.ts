import type { Overview, ServiceState } from '../api/types';

export type EventKind = 'boot' | ServiceState;
export interface SysEvent { at: number; kind: EventKind; label: string }

/**
 * Recent events derived from the overview: the last state change of each
 * allowlisted service plus the system start (generatedAt − uptime). Newest first.
 */
export function deriveEvents(o: Overview, limit = 5): SysEvent[] {
  const out: SysEvent[] = [];
  const gen = Date.parse(o.generatedAt);
  if (o.host.uptimeSeconds != null && Number.isFinite(gen)) {
    out.push({ at: gen - o.host.uptimeSeconds * 1000, kind: 'boot', label: o.host.hostname });
  }
  for (const s of o.services) {
    const at = s.since ? Date.parse(s.since) : NaN;
    if (Number.isFinite(at)) out.push({ at, kind: s.state, label: s.label });
  }
  // Services started during boot share its timestamp; keep boot first among equals.
  out.sort((a, b) => b.at - a.at || (a.kind === 'boot' ? 1 : b.kind === 'boot' ? -1 : 0));
  return out.slice(0, limit);
}
