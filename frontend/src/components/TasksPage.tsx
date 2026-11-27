import { CalendarClock, FolderClock, History, Loader2, Pencil, Play, Plus, RefreshCw, Square, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { StorageDisk, StorageReport, Task, TaskDef, TaskHistory, TaskList, TaskRun, TaskWeekday } from '../api/types';
import { formatBytes } from '../lib/format';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import {
  MAX_TASKS, WEEKDAYS, newBackup, formatDuration, formatWhen, isPowerTask, listError, newTask, nextRunLabel, normalizeTask, pollInterval,
  resultVariant, scheduleLabel, sortDays, validateTask, weekdayShort,
} from '../lib/tasks';
import { BackupTaskFields } from './BackupTaskFields';
import { BackupVersions } from './BackupVersions';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.tasks;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

function RunExtra({ run }: { run: TaskRun }) {
  if (run.bytes == null && run.usedBytes == null) return null;
  return <span className="block break-words text-xs text-muted-foreground">{t.runBytes(formatBytes(run.bytes ?? 0), run.usedBytes != null ? formatBytes(run.usedBytes) : undefined)}</span>;
}

function RunBadge({ run }: { run: TaskRun }) {
  return <Badge variant={resultVariant[run.result]}>{t.result[run.result]}</Badge>;
}

function EditorDialog({ initial, disks, onSave, onClose }: {
  initial: TaskDef; disks: StorageDisk[]; onSave: (t: TaskDef) => Promise<void>; onClose: () => void;
}) {
  const [form, setForm] = useState<TaskDef>(initial);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [touched, setTouched] = useState(false);
  const errors = validateTask(form);
  const set = (p: Partial<TaskDef>) => { setForm((f) => ({ ...f, ...p })); setErr(null); };
  const setSched = (p: Partial<TaskDef['schedule']>) => set({ schedule: { ...form.schedule, ...p } });
  const diskId = form.smart?.diskId ?? '';
  const known = disks.some((d) => d.id === diskId);
  const show = (k: keyof typeof errors) => (touched ? errors[k] ?? null : null);
  const days = form.schedule.weekdays ?? [];

  function changeType(type: TaskDef['type']) {
    set({
      type,
      smart: type === 'smart' ? form.smart ?? { diskId: disks[0]?.id ?? '', test: 'short' } : undefined,
      backup: type === 'backup-pull' ? form.backup ?? newBackup() : undefined,
    });
  }
  function changeKind(kind: TaskDef['schedule']['kind']) {
    const time = form.schedule.time;
    set({ schedule: kind === 'daily' ? { kind, time } : kind === 'weekly' ? { kind, time, weekdays: days.length ? days : ['mon'] } : { kind, time, day: form.schedule.day ?? 1 } });
  }
  const toggleDay = (d: TaskWeekday) => setSched({ weekdays: sortDays(days.includes(d) ? days.filter((x) => x !== d) : [...days, d]) });

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (Object.keys(errors).length > 0) return;
    setBusy(true); setErr(null);
    try { await onSave(normalizeTask(form)); } catch (ex) { setErr(apiMessage(ex)); setBusy(false); }
  }

  const ed = t.editor;
  return (
    <Dialog title={initial.name === '' && initial.id === '' ? ed.addTitle : ed.editTitle} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{ed.cancel}</Button>
        <Button type="submit" form="task-form" disabled={busy}>{busy ? t.saving : ed.save}</Button>
      </>}>
      <form id="task-form" onSubmit={(e) => void submit(e)} className="space-y-4" noValidate>
        <Field label={ed.name} hint={ed.nameHint} error={show('name')}>
          {(p) => <Input {...p} className={invalidCls} data-autofocus value={form.name} maxLength={80} onChange={(e) => set({ name: e.target.value })} />}
        </Field>
        <Field label={ed.type} hint={t.typeHint[form.type]}>
          {(p) => (
            <select {...p} className={selectCls} value={form.type} onChange={(e) => changeType(e.target.value as TaskDef['type'])}>
              {(['smart', 'recycle-cleanup', 'backup-pull', 'reboot', 'shutdown'] as const).map((k) => <option key={k} value={k}>{t.types[k]}</option>)}
            </select>
          )}
        </Field>
        {form.type === 'smart' && (
          <>
            <Field label={ed.disk} error={show('diskId')} hint={disks.length === 0 ? ed.noDisks : undefined}>
              {(p) => (
                <select {...p} className={`${selectCls} ${invalidCls}`} value={diskId}
                  onChange={(e) => set({ smart: { diskId: e.target.value, test: form.smart?.test ?? 'short' } })}>
                  <option value="">{ed.chooseDisk}</option>
                  {!known && diskId !== '' && <option value={diskId}>{ed.diskMissing(diskId)}</option>}
                  {disks.map((d) => <option key={d.id} value={d.id}>{d.model ?? d.name} ({d.name})</option>)}
                </select>
              )}
            </Field>
            <Field label={ed.test} error={show('test')}>
              {(p) => (
                <select {...p} className={selectCls} value={form.smart?.test ?? 'short'}
                  onChange={(e) => set({ smart: { diskId, test: e.target.value as 'short' | 'long' } })}>
                  {(['short', 'long'] as const).map((k) => <option key={k} value={k}>{t.tests[k]}</option>)}
                </select>
              )}
            </Field>
          </>
        )}
        {isPowerTask(form.type) && (
          <div role="note" className="space-y-1 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm">
            {form.type === 'shutdown' && <p className="font-medium">{t.power.noPowerOn}</p>}
            <p>{t.power.noCatchUp}</p>
          </div>
        )}
        {form.type === 'backup-pull' && form.backup && (
          <BackupTaskFields value={form.backup} onChange={(backup) => set({ backup })} errors={errors} isNew={initial.id === ''} />
        )}
        <Field label={ed.kind}>
          {(p) => (
            <select {...p} className={selectCls} value={form.schedule.kind} onChange={(e) => changeKind(e.target.value as TaskDef['schedule']['kind'])}>
              {(['daily', 'weekly', 'monthly'] as const).map((k) => <option key={k} value={k}>{t.kinds[k]}</option>)}
            </select>
          )}
        </Field>
        {form.schedule.kind === 'weekly' && (
          <fieldset>
            <legend className="text-sm font-medium">{ed.weekdays}</legend>
            <div className="mt-1 flex flex-wrap gap-2">
              {WEEKDAYS.map((d) => (
                <button key={d} type="button" aria-pressed={days.includes(d)} onClick={() => toggleDay(d)}
                  className={`h-10 min-w-10 rounded-md border px-3 text-sm font-medium ${days.includes(d) ? 'border-primary bg-primary/15' : 'border-border hover:bg-muted'}`}>
                  {weekdayShort(d)}
                </button>
              ))}
            </div>
            {show('weekdays') && <p className="mt-1 text-xs font-medium text-danger">{show('weekdays')}</p>}
          </fieldset>
        )}
        {form.schedule.kind === 'monthly' && (
          <Field label={ed.day} hint={ed.dayHint} error={show('day')}>
            {(p) => (
              <Input {...p} className={invalidCls} type="number" inputMode="numeric" min={1} max={28} value={form.schedule.day ?? ''}
                onChange={(e) => setSched({ day: e.target.value === '' ? undefined : Number(e.target.value) })} />
            )}
          </Field>
        )}
        <Field label={ed.time} hint={ed.timeHint} error={show('time')}>
          {(p) => <Input {...p} className={invalidCls} type="time" value={form.schedule.time} onChange={(e) => setSched({ time: e.target.value })} />}
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="h-4 w-4" checked={form.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
          <span className="font-medium">{t.enabled}</span>
        </label>
        {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
      </form>
    </Dialog>
  );
}

function HistoryDialog({ task, onClose }: { task: Task; onClose: () => void }) {
  const [runs, setRuns] = useState<TaskRun[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api<TaskHistory>(`/tasks/${encodeURIComponent(task.id)}/history`)
      .then((h) => { if (live) setRuns(h.runs); }).catch(() => { if (live) setFailed(true); });
    return () => { live = false; };
  }, [task.id]);
  return (
    <Dialog title={t.historyTitle(task.name)} onClose={onClose}
      footer={<Button type="button" variant="outline" onClick={onClose}>{cs.common.close}</Button>}>
      {failed ? <p role="alert" className="text-danger">{t.historyError}</p>
        : !runs ? <Skeleton className="h-24 w-full" />
          : runs.length === 0 ? <p className="text-muted-foreground">{t.historyEmpty}</p> : (
            <ul className="divide-y divide-border">
              {runs.map((r, i) => (
                <li key={`${r.start}-${i}`} className="space-y-1 py-2">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <RunBadge run={r} />
                    <span className="tabular-nums">{formatWhen(r.start)}</span>
                    <span className="text-xs text-muted-foreground">{t.duration}: {formatDuration(r.start, r.end)}</span>
                  </div>
                  {r.message && <p className="break-words text-muted-foreground">{r.message}</p>}
                  <RunExtra run={r} />
                </li>
              ))}
            </ul>
          )}
    </Dialog>
  );
}

function TaskCard({ task, readOnly, busy, onToggle, onRun, onStop, onEdit, onHistory, onVersions, onRemove }: {
  task: Task; readOnly: boolean; busy: boolean;
  onToggle: () => void; onRun: () => void; onStop: () => void; onEdit: () => void; onHistory: () => void; onVersions: () => void; onRemove: () => void;
}) {
  const n = task.name;
  const sub = task.type === 'smart' && task.smart ? `${t.types.smart}, ${t.testShort[task.smart.test]} test`
    : task.type === 'backup-pull' && task.backup ? `${t.types[task.type]}: ${task.backup.user}@${task.backup.host}` : t.types[task.type];
  return (
    <li className="space-y-3 rounded-lg border border-border p-3">
      <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
        <div className="min-w-0 flex-1 basis-48">
          <h3 className="break-words font-semibold">{n}</h3>
          <p className="break-all text-sm text-muted-foreground">{sub}</p>
          <p className="text-sm font-medium">{scheduleLabel(task.schedule)}</p>
        </div>
        <label className="flex h-10 items-center gap-2 text-sm">
          <input type="checkbox" role="switch" className="h-4 w-4" checked={task.enabled} disabled={readOnly || busy}
            aria-label={t.enabledAria(n)} onChange={onToggle} />
          {task.enabled ? t.enabled : t.disabledBadge}
        </label>
      </div>
      <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-2">
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">{t.next}</dt>
          <dd className="break-words font-medium">{task.enabled ? nextRunLabel(task.nextRun) : t.off}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">{t.last}</dt>
          <dd className="space-y-1" aria-live="polite">
            {task.running ? (
              <span role="status" className="inline-flex items-center gap-2 font-medium">
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden />{t.running}
              </span>
            ) : task.lastRun ? (
              <>
                <span className="flex flex-wrap items-center gap-2"><RunBadge run={task.lastRun} /><span className="tabular-nums">{formatWhen(task.lastRun.start)}</span></span>
                {task.lastRun.message && <span className="block break-words text-muted-foreground">{task.lastRun.message}</span>}
                <RunExtra run={task.lastRun} />
              </>
            ) : <span className="text-muted-foreground">{t.noRun}</span>}
          </dd>
        </div>
      </dl>
      <div className="flex flex-wrap gap-2">
        {!readOnly && (task.running || !isPowerTask(task.type)) && (task.running
          ? <Button type="button" size="sm" variant="outline" disabled={busy} aria-label={t.stopAria(n)} onClick={onStop}><Square className="h-4 w-4" aria-hidden />{t.stop}</Button>
          : <Button type="button" size="sm" variant="outline" disabled={busy} aria-label={t.runAria(n)} onClick={onRun}><Play className="h-4 w-4" aria-hidden />{t.runNow}</Button>)}
        <Button type="button" size="sm" variant="outline" aria-label={t.historyAria(n)} onClick={onHistory}><History className="h-4 w-4" aria-hidden />{t.history}</Button>
        {!readOnly && task.type === 'backup-pull' && <Button type="button" size="sm" variant="outline" aria-label={t.versionsAria(n)} onClick={onVersions}><FolderClock className="h-4 w-4" aria-hidden />{t.versions}</Button>}
        {!readOnly && <Button type="button" size="sm" variant="outline" disabled={busy} aria-label={t.editAria(n)} onClick={onEdit}><Pencil className="h-4 w-4" aria-hidden />{t.edit}</Button>}
        {!readOnly && <Button type="button" size="sm" variant="outline" disabled={busy} aria-label={t.removeAria(n)} onClick={onRemove}><Trash2 className="h-4 w-4" aria-hidden />{t.remove}</Button>}
      </div>
      {!readOnly && !task.running && isPowerTask(task.type) && <p className="text-xs text-muted-foreground">{t.power.runHint}</p>}
    </li>
  );
}

export function TasksPage({ readOnly }: { readOnly: boolean }) {
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [disks, setDisks] = useState<StorageDisk[]>([]);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [editing, setEditing] = useState<TaskDef | null>(null);
  const [history, setHistory] = useState<Task | null>(null);
  const [versions, setVersions] = useState<Task | null>(null);
  const [removing, setRemoving] = useState<Task | null>(null);
  const loading = useRef(false);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try { setTasks((await api<TaskList>('/tasks')).tasks); setFailed(false); }
    catch { setFailed(true); }
    finally { loading.current = false; }
  }, []);
  useVisiblePoll(() => { void load(); }, pollInterval(tasks ?? []));
  useEffect(() => {
    if (readOnly) return;
    api<StorageReport>('/storage').then((r) => setDisks(r.disks.filter((d) => d.smart))).catch(() => setDisks([]));
  }, [readOnly]);

  async function put(next: TaskDef[]) {
    const r = await api<TaskList>('/tasks', { method: 'PUT', body: { tasks: next.map(normalizeTask) } });
    setTasks(r.tasks);
  }
  async function guarded(fn: () => Promise<void>, okText?: string) {
    setBusy(true); setMsg(null);
    try { await fn(); if (okText) setMsg({ ok: true, text: okText }); }
    catch (e) { setMsg({ ok: false, text: apiMessage(e) }); }
    finally { setBusy(false); }
  }
  const defs = (tasks ?? []) as TaskDef[];

  async function saveEdited(d: TaskDef) {
    const exists = d.id !== '' && defs.some((x) => x.id === d.id);
    const next = exists ? defs.map((x) => (x.id === d.id ? d : x)) : [...defs, d];
    const le = listError(next);
    if (le) throw new ApiError(400, 'invalid', le);
    await put(next);
    setEditing(null);
    setMsg({ ok: true, text: t.saved });
  }
  const toggle = (task: Task) => void guarded(() => put(defs.map((x) => (x.id === task.id ? { ...x, enabled: !x.enabled } : x))));
  const action = (task: Task, what: 'run' | 'stop') => void guarded(async () => {
    await api(`/tasks/${encodeURIComponent(task.id)}/${what}`, { method: 'POST' });
    setMsg({ ok: true, text: what === 'run' ? t.runStarted(task.name) : t.stopRequested(task.name) });
    await load();
  });
  async function confirmRemove() {
    if (!removing) return;
    const id = removing.id;
    await guarded(() => put(defs.filter((x) => x.id !== id)), t.saved);
    setRemoving(null);
  }

  if (!tasks) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }
  return (
    <div className="space-y-4">
      <Widget id="tasks-list" title={t.title} icon={<CalendarClock />}
        right={<Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}><RefreshCw className="h-4 w-4" aria-hidden /></Button>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        {failed && <p role="alert" className="mt-2 text-sm text-danger">{t.loadError}</p>}
        {msg && <p role={msg.ok ? 'status' : 'alert'} className={`mt-2 text-sm font-medium ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</p>}
        {tasks.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">{t.empty}</p> : (
          <ul className="mt-3 space-y-3">
            {tasks.map((task) => (
              <TaskCard key={task.id} task={task} readOnly={readOnly} busy={busy}
                onToggle={() => toggle(task)} onRun={() => action(task, 'run')} onStop={() => action(task, 'stop')}
                onEdit={() => setEditing(normalizeTask(task))} onHistory={() => setHistory(task)} onVersions={() => setVersions(task)} onRemove={() => setRemoving(task)} />
            ))}
          </ul>
        )}
        {readOnly ? <p role="note" className="mt-3 text-sm text-muted-foreground">{t.readOnly}</p>
          : tasks.length < MAX_TASKS && (
            <Button type="button" variant="outline" size="sm" className="mt-3" disabled={busy} onClick={() => setEditing(newTask(disks[0]?.id ?? ''))}>
              <Plus className="mr-1 h-4 w-4" aria-hidden />{t.add}
            </Button>
          )}
      </Widget>
      {editing && <EditorDialog initial={editing} disks={disks} onSave={saveEdited} onClose={() => setEditing(null)} />}
      {history && <HistoryDialog task={history} onClose={() => setHistory(null)} />}
      {versions && <BackupVersions task={versions} onClose={() => setVersions(null)} />}
      {removing && (
        <Dialog title={t.removeTitle(removing.name)} onClose={() => setRemoving(null)} busy={busy}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setRemoving(null)} disabled={busy}>{cs.common.cancel}</Button>
            <Button type="button" variant="destructive" onClick={() => void confirmRemove()} disabled={busy}>{t.removeConfirm}</Button>
          </>}>
          <p>{t.removeBody}</p>
        </Dialog>
      )}
    </div>
  );
}
