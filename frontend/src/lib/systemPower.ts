import type { SystemPowerAction } from '../api/types';

/** The typed confirmation: the NAS name, ignoring case and surrounding spaces. */
export function confirmMatches(input: string, hostname: string): boolean {
  const want = hostname.trim().toLowerCase();
  return want !== '' && input.trim().toLowerCase() === want;
}

/**
 * Phases of the "going down" screen (CORE-14):
 * stopping – the NAS still answers; offline – it stopped answering;
 * back – after a restart it answers again; late – a restart takes longer than expected.
 */
export type DownPhase = 'stopping' | 'offline' | 'back' | 'late';

/** A restart normally takes 2–3 minutes on the NSA310; after this the screen says so. */
export const LATE_AFTER_MS = 10 * 60_000;

export function nextPhase(action: SystemPowerAction, phase: DownPhase, up: boolean, elapsedMs: number): DownPhase {
  if (phase === 'back') return 'back';
  if (phase === 'stopping') return up ? 'stopping' : 'offline';
  // offline or late
  if (up && action === 'reboot') return 'back';
  if (action === 'reboot' && elapsedMs >= LATE_AFTER_MS) return 'late';
  return phase;
}
