import type { PowerEvent, PowerState } from '../api/types';

export type SegmentKind = 'active' | 'asleep' | 'untracked';
export interface Segment { kind: SegmentKind; from: number; to: number; percent: number }

const kindOf = (s: PowerState): SegmentKind =>
  s === 'standby' || s === 'sleeping' ? 'asleep' : s === 'active' ? 'active' : 'untracked';

/** Splits [now - windowMs, now] into runs of one state; time before the first event is untracked. */
export function timeline(events: PowerEvent[], now: number, windowMs = 24 * 3600e3): Segment[] {
  const start = now - windowMs;
  const out: Segment[] = [];
  const push = (kind: SegmentKind, from: number, to: number) => {
    const a = Math.max(from, start), b = Math.min(to, now);
    if (b <= a) return;
    const last = out[out.length - 1];
    if (last && last.kind === kind && last.to === a) { last.to = b; last.percent = ((b - last.from) / windowMs) * 100; return; }
    out.push({ kind, from: a, to: b, percent: ((b - a) / windowMs) * 100 });
  };
  const first = events[0];
  if (!first) return [{ kind: 'untracked', from: start, to: now, percent: 100 }];
  push('untracked', start, first.t);
  events.forEach((e, i) => push(kindOf(e.state), e.t, events[i + 1]?.t ?? now));
  return out;
}

/** The newest changes first, at most n. */
export function recentChanges(events: PowerEvent[], n: number): PowerEvent[] {
  return events.slice(-n).reverse();
}

export function perDay(v: number): string {
  return v.toLocaleString('cs-CZ', { maximumFractionDigits: v < 10 ? 1 : 0 });
}
