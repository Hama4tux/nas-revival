import type { HardwarePoint, HardwareSensor, HardwareThresholds } from '../api/types';

/** 24 h of minute points. */
export const HISTORY_SIZE = 24 * 60;

/** Replace the running minute (and anything newer) by the fresh points; keep 24 h. */
export function mergeHardwareHistory(prev: HardwarePoint[], next: HardwarePoint[]): HardwarePoint[] {
  if (next.length === 0) return prev;
  const first = next[0]!.t;
  return [...prev.filter((p) => p.t < first), ...next].slice(-HISTORY_SIZE);
}

/** `since` for the next poll: the last closed minute, so the running one comes again. */
export function historySince(points: HardwarePoint[]): number {
  return points.length >= 2 ? points[points.length - 2]!.t : 0;
}

/** Highest value per point over the given sensors; points without any of them are skipped. */
export function maxSeries(points: HardwarePoint[], ids: string[]): number[] {
  const out: number[] = [];
  for (const p of points) {
    const vals = ids.map((id) => p.temps[id]).filter((v): v is number => v !== undefined);
    if (vals.length) out.push(Math.max(...vals));
  }
  return out;
}

export type TempLevel = 'ok' | 'warn' | 'crit';

export function tempLevel(s: HardwareSensor, t: HardwareThresholds): TempLevel {
  if (s.c === null) return 'ok';
  const [warn, crit] = s.kind === 'disk' ? [t.diskWarnC, t.diskCritC] : [t.boardWarnC, t.boardCritC];
  return s.c >= crit ? 'crit' : s.c >= warn ? 'warn' : 'ok';
}

/** Same limits as the NAS (hardware.Validate); returns the first invalid field. */
export function thresholdError(t: HardwareThresholds): keyof HardwareThresholds | null {
  const ok = (v: number) => Number.isInteger(v);
  if (!ok(t.boardWarnC) || t.boardWarnC < 40 || t.boardWarnC > 80) return 'boardWarnC';
  if (!ok(t.boardCritC) || t.boardCritC <= t.boardWarnC || t.boardCritC > 90) return 'boardCritC';
  if (!ok(t.diskWarnC) || t.diskWarnC < 35 || t.diskWarnC > 65) return 'diskWarnC';
  if (!ok(t.diskCritC) || t.diskCritC <= t.diskWarnC || t.diskCritC > 70) return 'diskCritC';
  return null;
}

/** PWM 0–255 as a rounded percentage. */
export const pwmPercent = (pwm: number) => Math.round((pwm * 100) / 255);
