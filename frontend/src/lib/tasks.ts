import { cs } from '../i18n/cs';
import { SHARE_NAME_RE } from './shares';
import type { BackupParams, Task, TaskDef, TaskRun, TaskSchedule, TaskType, TaskWeekday } from '../api/types';

export const WEEKDAYS: TaskWeekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
export const MAX_TASKS = 32;
export const MAX_BACKUP_TASKS = 8;
export const MAX_POWER_TASKS = 4;
export const DEFAULT_BACKUP_ROOT = '/var/backups';
export const NAME_MAX = 40;
const ID_RE = /^[a-z0-9]{1,16}$/;
const DISK_RE = /^[A-Za-z0-9._:-]{1,128}$/;
const TIME_RE = /^([01]\d|2[0-3]):[0-5]\d$/;

export const weekdayShort = (d: TaskWeekday) => cs.tasks.weekdayShort[d];

/** Weekdays in canonical Mon..Sun order. */
export const sortDays = (days: TaskWeekday[]): TaskWeekday[] => WEEKDAYS.filter((d) => days.includes(d));

/** "Denně v 03:30", "Po, Čt v 03:30", "Každý měsíc 1. v 02:00". */
export function scheduleLabel(s: TaskSchedule): string {
  if (s.kind === 'daily') return cs.tasks.fmt.daily(s.time);
  if (s.kind === 'weekly') return cs.tasks.fmt.weekly(sortDays(s.weekdays ?? []).map(weekdayShort).join(', '), s.time);
  return cs.tasks.fmt.monthly(s.day ?? '', s.time);
}

export const timeOk = (t: string) => TIME_RE.test(t);

const HOST_RE = /^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))*$/;
const IPV6_RE = /^[0-9A-Fa-f:.]{2,45}$/;
const USER_RE = /^[a-z_][a-z0-9_-]{0,31}$/;
const PATH_RE = /^[A-Za-z0-9._-]+(\/[A-Za-z0-9._-]+)*$/;
const EXCLUDE_RE = /^[A-Za-z0-9._*?/[\]-]+$/;

export type TaskErrors = Partial<Record<'name' | 'time' | 'weekdays' | 'day' | 'diskId' | 'test' | 'list'
  | 'destShare' | 'destFolder' | 'host' | 'port' | 'user' | 'paths' | 'keepDays' | 'freshFile' | 'freshAge' | 'bwlimit' | 'excludes', string>>;

export function hostOk(h: string): boolean {
  if (h === '' || h.length > 253 || h.startsWith('-')) return false;
  if (h.includes(':')) return IPV6_RE.test(h) && (h.match(/:/g) ?? []).length >= 2;
  return HOST_RE.test(h);
}
/** Port 0 / missing means the default 22. */
export const portOk = (p: number | undefined) => p === undefined || p === 0 || (Number.isInteger(p) && p >= 1 && p <= 65535);
export const userOk = (u: string) => USER_RE.test(u);
/** Relative path on the server (rrsync root): safe characters, no "." / ".." segment, max 128. */
export const relPathOk = (p: string) => p.length <= 128 && PATH_RE.test(p) && !p.split('/').some((s) => s === '.' || s === '..');
const intIn = (n: number, lo: number, hi: number) => Number.isInteger(n) && n >= lo && n <= hi;

function validateBackup(b: BackupParams | undefined, e: TaskErrors) {
  const v = cs.tasks.v;
  if (!b) { e.host = v.host; return; }
  if (!b.dest || !SHARE_NAME_RE.test(b.dest.share)) e.destShare = v.destShare;
  if (b.dest?.folder && !SHARE_NAME_RE.test(b.dest.folder.trim())) e.destFolder = v.destFolder;
  if (!hostOk(b.host.trim())) e.host = v.host;
  if (!portOk(b.port)) e.port = v.port;
  if (!userOk(b.user.trim())) e.user = v.user;
  const paths = b.paths.map((p) => p.trim());
  if (paths.length < 1 || paths.length > 8 || paths.some((p) => !relPathOk(p)) || new Set(paths).size !== paths.length) e.paths = v.paths;
  if (!intIn(b.keepDays, 1, 90)) e.keepDays = v.keepDays;
  if (!intIn(b.bwlimitKiB, 0, 1_000_000)) e.bwlimit = v.bwlimit;
  if (b.fresh) {
    const f = b.fresh.file.trim();
    if (!relPathOk(f) || !paths.some((p) => f.startsWith(`${p}/`))) e.freshFile = v.freshFile;
    if (!intIn(b.fresh.maxAgeHours, 1, 720)) e.freshAge = v.freshAge;
  }
  const ex = b.excludes.map((x) => x.trim());
  if (ex.length > 16 || ex.some((x) => x.length < 1 || x.length > 100 || x.startsWith('-') || !EXCLUDE_RE.test(x))) e.excludes = v.excludes;
}

/** Mirrors the backend validation of a single task (messages are Czech). */
export function validateTask(t: TaskDef): TaskErrors {
  const v = cs.tasks.v;
  const e: TaskErrors = {};
  const name = t.name.trim();
  if (name === '') e.name = v.name;
  else if ([...name].length > NAME_MAX) e.name = v.nameLong(NAME_MAX);
  else if (/[\u0000-\u001f\u007f-\u009f]/.test(name)) e.name = v.nameCtl;
  if (t.id !== '' && !ID_RE.test(t.id)) e.list = v.id;
  if (!timeOk(t.schedule.time)) e.time = v.time;
  if (t.schedule.kind === 'weekly') {
    const d = t.schedule.weekdays ?? [];
    if (d.length === 0 || new Set(d).size !== d.length || d.some((x) => !WEEKDAYS.includes(x))) e.weekdays = v.weekdays;
  }
  if (t.schedule.kind === 'monthly') {
    const d = t.schedule.day;
    if (!Number.isInteger(d) || (d as number) < 1 || (d as number) > 28) e.day = v.day;
  }
  if (t.type === 'smart') {
    if (!t.smart || !DISK_RE.test(t.smart.diskId)) e.diskId = v.disk;
    if (!t.smart || (t.smart.test !== 'short' && t.smart.test !== 'long')) e.test = v.test;
  }
  if (t.type === 'backup-pull') validateBackup(t.backup, e);
  return e;
}

/** Restart and shutdown tasks (W3.5): no "run now", the top bar power menu does that with confirmation. */
export const isPowerTask = (type: TaskType) => type === 'reboot' || type === 'shutdown';

export const taskOk = (t: TaskDef) => Object.keys(validateTask(t)).length === 0;

/** Error for the whole list (count limit, unique ids); undefined when fine. */
export function listError(tasks: TaskDef[]): string | undefined {
  const v = cs.tasks.v;
  if (tasks.length > MAX_TASKS) return v.tooMany(MAX_TASKS);
  const ids = tasks.map((t) => t.id).filter((i) => i !== '');
  if (new Set(ids).size !== ids.length) return v.dupIds;
  if (tasks.filter((t) => t.type === 'backup-pull').length > MAX_BACKUP_TASKS) return v.tooManyBackups(MAX_BACKUP_TASKS);
  if (tasks.filter((t) => isPowerTask(t.type)).length > MAX_POWER_TASKS) return v.tooManyPower(MAX_POWER_TASKS);
  return undefined;
}

/** Drops fields that do not belong to the schedule kind / task type, trims the name. */
export function normalizeTask(t: TaskDef): TaskDef {
  const { kind, time } = t.schedule;
  const schedule: TaskSchedule = kind === 'daily' ? { kind, time }
    : kind === 'weekly' ? { kind, time, weekdays: sortDays(t.schedule.weekdays ?? []) }
      : { kind, time, day: t.schedule.day };
  const out: TaskDef = { id: t.id, name: t.name.trim(), type: t.type, enabled: t.enabled, schedule };
  if (t.type === 'smart' && t.smart) out.smart = { diskId: t.smart.diskId, test: t.smart.test };
  if (t.type === 'backup-pull' && t.backup) {
    const b = t.backup;
    const folder = b.dest.folder?.trim() ?? '';
    out.backup = {
      dest: { share: b.dest.share, ...(folder !== '' ? { folder } : {}) },
      host: b.host.trim(), port: b.port || 22, user: b.user.trim(), paths: b.paths.map((p) => p.trim()).filter((p) => p !== ''),
      keepDays: b.keepDays, bwlimitKiB: b.bwlimitKiB || 0, excludes: b.excludes.map((x) => x.trim()).filter((x) => x !== ''),
      ...(b.fresh ? { fresh: { file: b.fresh.file.trim(), maxAgeHours: b.fresh.maxAgeHours } } : {}),
    };
  }
  return out;
}

export const DEFAULT_BACKUP_FOLDER = 'daily-backup';
export const newBackup = (): BackupParams => ({ dest: { share: '', folder: DEFAULT_BACKUP_FOLDER }, host: '', port: 22, user: '', paths: [''], keepDays: 35, bwlimitKiB: 0, excludes: [] });

/** authorized_keys line with the folder the admin chose; null when the folder is not a plain absolute path. */
export function authorizedLine(hint: string, root: string): string | null {
  if (!/^\/[A-Za-z0-9._/-]*$/.test(root) || root.includes('..')) return null;
  return hint.replace(`rrsync -ro ${DEFAULT_BACKUP_ROOT}`, `rrsync -ro ${root}`);
}

export const newTask = (diskId = ''): TaskDef => ({
  id: '', name: '', type: 'smart', enabled: true,
  schedule: { kind: 'weekly', time: '02:00', weekdays: ['sun'] },
  smart: { diskId, test: 'short' },
});

/** Keeps the editable part of a Task (drops running / nextRun / lastRun). */
export const toDef = (t: TaskDef): TaskDef => normalizeTask(t);

const pad = (n: number) => String(n).padStart(2, '0');

/** "14. 3. 2026 v 03:30" in local time, or a dash. */
export function formatWhen(iso: string | null | undefined): string {
  if (!iso) return cs.tasks.fmt.dash;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return cs.tasks.fmt.dash;
  return cs.tasks.fmt.when(`${d.getDate()}. ${d.getMonth() + 1}. ${d.getFullYear()}`, `${pad(d.getHours())}:${pad(d.getMinutes())}`);
}

/** Whole-second duration as "42 s", "5 min 3 s", "2 h 10 min". */
export function formatDuration(startIso: string, endIso: string): string {
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (!Number.isFinite(ms) || ms < 0) return cs.tasks.fmt.dash;
  const s = Math.round(ms / 1000);
  if (s < 60) return cs.tasks.fmt.sec(s);
  const m = Math.floor(s / 60);
  if (m < 60) return cs.tasks.fmt.minSec(m, s % 60);
  const h = Math.floor(m / 60);
  return cs.tasks.fmt.hourMin(h, m % 60);
}

/** Relative next-run text: "za 5 min", "za 3 h", otherwise the date. */
export function nextRunLabel(iso: string | null, now = Date.now()): string {
  if (!iso) return cs.tasks.fmt.dash;
  const diff = new Date(iso).getTime() - now;
  if (!Number.isFinite(diff)) return cs.tasks.fmt.dash;
  if (diff <= 0) return cs.tasks.fmt.soon;
  const min = Math.round(diff / 60000);
  if (min < 1) return cs.tasks.fmt.soon;
  if (min < 60) return cs.tasks.fmt.inMin(min, formatWhen(iso));
  if (min < 24 * 60) return cs.tasks.fmt.inHours(Math.round(min / 60), formatWhen(iso));
  return formatWhen(iso);
}

export const resultVariant = { ok: 'success', failed: 'danger', skipped: 'warning', stopped: 'neutral' } as const;

/** Polling interval: fast while any task runs. */
export const pollInterval = (tasks: { running: boolean }[]) => (tasks.some((t) => t.running) ? 10_000 : 60_000);

export const lastResult = (r: TaskRun | null) => r?.result ?? null;

/** Dot colour of a compact task row: a disabled task is grey whatever it did last. */
export function taskTone(t: Pick<Task, 'enabled' | 'running' | 'lastRun'>): 'success' | 'warning' | 'danger' | 'neutral' {
  if (t.running) return 'warning';
  if (!t.enabled || !t.lastRun) return 'neutral';
  return resultVariant[t.lastRun.result];
}

/** An enabled task whose last run failed: the row is tinted and shows the message while collapsed. */
export const isTaskProblem = (t: Pick<Task, 'enabled' | 'running' | 'lastRun'>) =>
  t.enabled && !t.running && t.lastRun?.result === 'failed';

/** What the task does, one line: "Test disku (SMART), dlouhý test", "Stažení zálohy ze serveru: backup@pve → zalohy/daily-backup". */
export function taskSummary(t: TaskDef): string {
  const type = cs.tasks.types[t.type] ?? t.type;
  if (t.type === 'smart' && t.smart) return `${type}, ${cs.tasks.testShort[t.smart.test]} test`;
  if (t.type === 'backup-pull' && t.backup) {
    const d = t.backup.dest;
    return `${type}: ${t.backup.user}@${t.backup.host}${d?.share ? ` → ${d.share}${d.folder ? `/${d.folder}` : ''}` : ''}`;
  }
  return type;
}
