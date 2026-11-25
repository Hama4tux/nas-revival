import type { DiskPower, PowerProfile, PowerSettings } from '../api/types';

export interface PowerForm { profile: PowerProfile; standbyMinutes: number; apm: string }

export function formOf(v: DiskPower): PowerForm {
  return { profile: v.settings.profile, standbyMinutes: v.settings.standbyMinutes, apm: v.settings.apm == null ? '' : String(v.settings.apm) };
}

/** APM text field: empty = leave the drive's own setting; otherwise 1–255. */
export function parseApm(s: string): number | null | 'invalid' {
  const v = s.trim();
  if (v === '') return null;
  if (!/^\d{1,3}$/.test(v)) return 'invalid';
  const n = Number(v);
  return n >= 1 && n <= 255 ? n : 'invalid';
}

/** The settings the form would store (fixed profiles carry their own values). */
export function effective(v: DiskPower, f: PowerForm): PowerSettings | null {
  if (f.profile !== 'custom') return v.profiles.find((p) => p.profile === f.profile) ?? null;
  const apm = parseApm(f.apm);
  return apm === 'invalid' ? null : { profile: 'custom', standbyMinutes: f.standbyMinutes, apm };
}

export function changed(v: DiskPower, s: PowerSettings | null): boolean {
  if (!s) return false;
  const c = v.settings;
  return !v.stored || s.profile !== c.profile || s.standbyMinutes !== c.standbyMinutes || s.apm !== c.apm;
}

/** ADR 0007: spinning down the system disk needs an explicit confirmation. */
export function needsConfirm(v: DiskPower, s: PowerSettings | null): boolean {
  return v.systemDisk && !!s && s.standbyMinutes > 0;
}

export function standbyLabel(min: number): string {
  if (min === 0) return 'nikdy';
  return min < 60 ? `${min} min` : `${min / 60} h`;
}

/** What an APM level means for the drive: 1–127 may spin down by itself, 128–253 may park heads. */
export function apmKind(level: number | null): 'off' | 'spindown' | 'park' | 'max' | null {
  if (level == null) return null;
  if (level === 255) return 'off';
  if (level <= 127) return 'spindown';
  return level === 254 ? 'max' : 'park';
}
