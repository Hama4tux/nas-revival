import { cs } from '../i18n/cs';
import type { TaskDef, TaskRun, TaskSchedule, TaskWeekday } from '../api/types';

export const WEEKDAYS: TaskWeekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
export const MAX_TASKS = 32;
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

export type TaskErrors = Partial<Record<'name' | 'time' | 'weekdays' | 'day' | 'diskId' | 'test' | 'list', string>>;

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
  return e;
}

export const taskOk = (t: TaskDef) => Object.keys(validateTask(t)).length === 0;

/** Error for the whole list (count limit, unique ids); undefined when fine. */
export function listError(tasks: TaskDef[]): string | undefined {
  const v = cs.tasks.v;
  if (tasks.length > MAX_TASKS) return v.tooMany(MAX_TASKS);
  const ids = tasks.map((t) => t.id).filter((i) => i !== '');
  if (new Set(ids).size !== ids.length) return v.dupIds;
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
  return out;
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
