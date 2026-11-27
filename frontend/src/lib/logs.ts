import type { LogCategory, LogEntry, LogMaxRetention, LogMaxUse } from '../api/types';
import type { Tone } from './services';

export const LOG_TEXT_MAX = 100;
export const LOG_PAGE = 200;
export const LOG_CATEGORIES: LogCategory[] = ['all', 'kernel', 'auth', 'nas', 'services'];
export const LOG_PRESETS = ['hour', 'day', 'week', 'all'] as const;
export type LogPreset = (typeof LOG_PRESETS)[number];
export const LOG_MAX_USE: LogMaxUse[] = ['32M', '64M', '128M', '256M'];
export const LOG_MAX_RETENTION: LogMaxRetention[] = ['none', '1week', '1month', '3month', '1year'];

/** Journal priorities 0..7 (syslog names). */
export const PRIORITY_NAMES = ['emerg', 'alert', 'crit', 'err', 'warning', 'notice', 'info', 'debug'] as const;
export const priorityName = (p: number): string => PRIORITY_NAMES[p] ?? String(p);

/** 0-3 error, 4 warning, rest neutral (same idea as the Services journal). */
export const logTone = (p: number): Tone => (p <= 3 ? 'danger' : p === 4 ? 'warning' : 'neutral');

export interface LogFilters {
  category: LogCategory;
  /** Services catalog unit; only sent with category "all". */
  unit: string;
  /** Show this severity and worse (0..7). */
  priority: number;
  preset: LogPreset;
  text: string;
}

export const defaultFilters: LogFilters = { category: 'all', unit: '', priority: 6, preset: 'day', text: '' };

const PRESET_MS: Record<Exclude<LogPreset, 'all'>, number> = { hour: 3600_000, day: 86_400_000, week: 7 * 86_400_000 };

/** Start of the time range as RFC 3339, null for "everything". */
export function presetSince(p: LogPreset, now: Date): string | null {
  return p === 'all' ? null : new Date(now.getTime() - PRESET_MS[p]).toISOString();
}

/** Text search must be at most 100 characters without control characters (the backend rejects the rest). */
export const textError = (text: string): 'long' | 'control' | null => {
  const t = text.trim();
  if (Array.from(t).length > LOG_TEXT_MAX) return 'long';
  // eslint-disable-next-line no-control-regex
  return /[\u0000-\u001f\u007f]/.test(t) ? 'control' : null;
};

/** Query string for GET /logs. `until` (oldest loaded time) pages to older entries. Throws RangeError on invalid text. */
export function buildLogQuery(f: LogFilters, o: { now: Date; until?: string | null; limit?: number; category?: LogCategory }): string {
  if (textError(f.text)) throw new RangeError('invalid text');
  const q = new URLSearchParams();
  const category = o.category ?? f.category;
  q.set('category', category);
  if (category === 'all' && f.unit) q.set('unit', f.unit);
  if (category !== 'audit') q.set('priority', String(Math.min(7, Math.max(0, Math.trunc(f.priority)))));
  const since = presetSince(f.preset, o.now);
  if (since) q.set('since', since);
  if (o.until) q.set('until', o.until);
  const text = f.text.trim();
  if (text) q.set('text', text);
  q.set('limit', String(Math.min(1000, Math.max(1, o.limit ?? LOG_PAGE))));
  return q.toString();
}

const key = (e: LogEntry) => `${e.time}\u0000${e.ident}\u0000${e.message}`;

/** Appends an older page (newest first); entries repeated at the boundary are dropped. */
export function mergeOlder(loaded: LogEntry[], page: LogEntry[]): LogEntry[] {
  const seen = new Set(loaded.map(key));
  const out = [...loaded];
  for (const e of page) {
    const k = key(e);
    if (!seen.has(k)) { seen.add(k); out.push(e); }
  }
  return out;
}

/** Time to pass as `until` for the next (older) page. */
export const oldestTime = (entries: LogEntry[]): string | null => entries.length ? entries[entries.length - 1]!.time : null;

const identOf = (e: LogEntry) => (e.ident ? `${e.ident}${e.pid ? `[${e.pid}]` : ''}` : '-');

/** One line per entry: ISO time, priority name, ident[pid], message (newlines flattened). */
export function exportText(entries: LogEntry[]): string {
  return entries.map((e) => `${e.time} ${priorityName(e.priority)} ${identOf(e)}: ${e.message.replace(/\s*[\r\n]+\s*/g, ' ')}`).join('\n') + (entries.length ? '\n' : '');
}

export function exportFilename(now: Date): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `nas-revival-log-${now.getFullYear()}${p(now.getMonth() + 1)}${p(now.getDate())}-${p(now.getHours())}${p(now.getMinutes())}.txt`;
}

export const AUDIT_ACTIONS = [
  'user.create', 'user.role', 'user.password', 'user.delete', 'login', 'login.fail', 'password.own', 'password.own.fail',
  'session.revoke', 'session.revoke_others', 'smb.settings', 'network.settings', 'hostname', 'tls.install', 'tls.regenerate',
  'service.start', 'service.stop', 'service.restart', 'service.enable', 'service.disable', 'firewall.apply', 'firewall.confirm', 'logs.retention',
] as const;

/** Czech label for a known action id, the raw id otherwise. */
export function auditActionLabel(id: string, labels: Record<string, string>): string {
  return Object.prototype.hasOwnProperty.call(labels, id) ? labels[id]! : id;
}
