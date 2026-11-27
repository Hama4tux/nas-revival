import { Download, RefreshCw, Search } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { LogCategory, LogEntry, LogList, LogRetention, ServiceList } from '../api/types';
import { cs } from '../i18n/cs';
import { formatBytes } from '../lib/format';
import {
  LOG_CATEGORIES, LOG_MAX_RETENTION, LOG_MAX_USE, LOG_PRESETS, LOG_TEXT_MAX, auditActionLabel, buildLogQuery, defaultFilters, exportFilename, exportText,
  logTone, mergeOlder, oldestTime, priorityName, textError, type LogFilters, type LogPreset,
} from '../lib/logs';
import { cn } from '../lib/utils';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.logs;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-2 text-sm';
const time = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('cs-CZ');
};

function loadMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 504) return t.timeout;
    if (e.status === 400 && e.message) return e.message;
  }
  return t.loadError;
}

/** Loads a log query; paging to older entries repeats it with `until`. Stale responses are ignored. */
function useLogs(filters: LogFilters, category: LogCategory) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [more, setMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const seq = useRef(0);
  const loaded = useRef<LogEntry[]>([]);

  useEffect(() => {
    const id = ++seq.current;
    setLoading(true); setErr(null);
    void (async () => {
      try {
        const r = await api<LogList>(`/logs?${buildLogQuery(filters, { now: new Date(), category })}`);
        if (id !== seq.current) return;
        loaded.current = r.entries; setEntries(r.entries); setMore(r.more);
      } catch (e) {
        if (id !== seq.current) return;
        loaded.current = []; setEntries([]); setMore(false); setErr(loadMessage(e));
      } finally { if (id === seq.current) setLoading(false); }
    })();
  }, [filters, category, tick]);

  const older = useCallback(async () => {
    const until = oldestTime(loaded.current);
    if (!until) return;
    const id = seq.current;
    setLoadingOlder(true); setErr(null);
    try {
      const r = await api<LogList>(`/logs?${buildLogQuery(filters, { now: new Date(), category, until })}`);
      if (id !== seq.current) return;
      loaded.current = mergeOlder(loaded.current, r.entries); setEntries(loaded.current); setMore(r.more);
    } catch (e) { if (id === seq.current) setErr(loadMessage(e)); }
    finally { setLoadingOlder(false); }
  }, [filters, category]);

  return { entries, more, loading, loadingOlder, err, refresh: () => setTick((n) => n + 1), older };
}

function Field({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return <label className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}><span className="text-muted-foreground">{label}</span>{children}</label>;
}

function PresetSelect({ value, onChange }: { value: LogPreset; onChange: (v: LogPreset) => void }) {
  return (
    <Field label={t.range}>
      <select className={selectCls} value={value} onChange={(e) => onChange(e.target.value as LogPreset)}>
        {LOG_PRESETS.map((p) => <option key={p} value={p}>{t.presets[p]}</option>)}
      </select>
    </Field>
  );
}

function Footer({ n, more, loadingOlder, onOlder }: { n: number; more: boolean; loadingOlder: boolean; onOlder: () => void }) {
  return (
    <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
      <span role="status">{t.count(n, more)}</span>
      {more && <Button type="button" variant="outline" size="sm" disabled={loadingOlder} onClick={onOlder}>{loadingOlder ? t.loadingOlder : t.older}</Button>}
    </div>
  );
}

function JournalPanel() {
  const [filters, setFilters] = useState<LogFilters>(defaultFilters);
  const [draft, setDraft] = useState('');
  const [units, setUnits] = useState<{ name: string; label: string }[]>([]);
  const { entries, more, loading, loadingOlder, err, refresh, older } = useLogs(filters, filters.category);
  const draftErr = textError(draft);

  useEffect(() => {
    api<ServiceList>('/services').then((l) => setUnits(l.services.map((s) => ({ name: s.name, label: s.label })))).catch(() => { /* the unit filter is optional */ });
  }, []);

  const set = (p: Partial<LogFilters>) => setFilters((f) => ({ ...f, ...p }));
  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (draftErr) return;
    const text = draft.trim();
    if (text === filters.text) refresh(); else set({ text });
  }
  function download() {
    const url = URL.createObjectURL(new Blob([exportText(entries)], { type: 'text/plain;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url; a.download = exportFilename(new Date());
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  return (
    <Widget id="logs-journal" title={t.tabs.journal} icon={<Search />}
      right={<div className="flex gap-2">
        <Button type="button" variant="outline" size="sm" onClick={refresh} disabled={loading}><RefreshCw className="h-4 w-4" aria-hidden />{t.refresh}</Button>
        <Button type="button" variant="outline" size="sm" onClick={download} disabled={entries.length === 0}><Download className="h-4 w-4" aria-hidden />{t.export}</Button>
      </div>}>
      <div className="mb-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t.category}>
          <select className={selectCls} value={filters.category} onChange={(e) => set({ category: e.target.value as LogCategory, unit: '' })}>
            {LOG_CATEGORIES.map((c) => <option key={c} value={c}>{t.categories[c]}</option>)}
          </select>
        </Field>
        <Field label={t.unit}>
          <select className={selectCls} value={filters.unit} disabled={filters.category !== 'all'} onChange={(e) => set({ unit: e.target.value })}>
            <option value="">{t.anyUnit}</option>
            {units.map((u) => <option key={u.name} value={u.name}>{u.label}</option>)}
          </select>
        </Field>
        <Field label={t.severity}>
          <select className={selectCls} value={filters.priority} onChange={(e) => set({ priority: Number(e.target.value) })}>
            {t.severities.map((n, p) => <option key={p} value={p}>{t.severityOption(n, p)}</option>)}
          </select>
        </Field>
        <PresetSelect value={filters.preset} onChange={(preset) => set({ preset })} />
      </div>
      <form onSubmit={submit} className="mb-3 flex flex-wrap items-start gap-2" role="search">
        <div className="min-w-0 flex-1 basis-60">
          <Input value={draft} onChange={(e) => setDraft(e.target.value)} maxLength={LOG_TEXT_MAX + 20} placeholder={t.searchPlaceholder} aria-label={t.search}
            autoComplete="off" spellCheck={false} aria-invalid={draftErr != null} className="aria-[invalid=true]:border-danger" />
          {draftErr && <p role="alert" className="mt-1 text-sm text-danger">{draftErr === 'long' ? t.textLong : t.textControl}</p>}
        </div>
        <Button type="submit" disabled={draftErr != null}><Search className="h-4 w-4" aria-hidden />{t.searchGo}</Button>
      </form>
      <Entries entries={entries} loading={loading} err={err} />
      {!loading && !err && entries.length > 0 && <Footer n={entries.length} more={more} loadingOlder={loadingOlder} onOlder={() => void older()} />}
    </Widget>
  );
}

function Entries({ entries, loading, err }: { entries: LogEntry[]; loading: boolean; err: string | null }) {
  if (loading) return <div aria-busy="true"><p role="status" className="mb-2 text-sm text-muted-foreground">{t.loading}</p><Skeleton className="h-40 w-full" /></div>;
  if (err) return <p role="alert" className="font-medium text-danger">{err}</p>;
  if (entries.length === 0) return <p className="text-muted-foreground">{t.empty}</p>;
  return (
    <ol aria-label={t.entriesLabel} className="max-h-[65vh] overflow-y-auto rounded-md bg-muted/60 p-2 font-mono text-xs">
      {entries.map((e, i) => {
        const tone = logTone(e.priority);
        return (
          <li key={`${e.time}-${i}`} className={cn('flex flex-col gap-x-2 border-b border-border/40 py-1 last:border-0 sm:flex-row', tone === 'danger' && 'text-danger', tone === 'warning' && 'text-warning')}>
            <span className="flex shrink-0 items-center gap-2">
              <span className="text-muted-foreground">{time(e.time)}</span>
              <Badge variant={tone === 'neutral' ? 'neutral' : tone} className="font-mono">{priorityName(e.priority)}</Badge>
            </span>
            <span className="min-w-0 whitespace-pre-wrap break-words">
              {e.ident && <span className="text-muted-foreground">{e.ident}{e.pid ? `[${e.pid}]` : ''}: </span>}
              {e.message}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function AuditPanel() {
  const [filters, setFilters] = useState<LogFilters>({ ...defaultFilters, category: 'audit', preset: 'week' });
  const { entries, more, loading, loadingOlder, err, refresh, older } = useLogs(filters, 'audit');
  const c = t.auditCols;
  return (
    <Widget id="logs-audit" title={t.tabs.audit} icon={<Search />}
      right={<Button type="button" variant="outline" size="sm" onClick={refresh} disabled={loading}><RefreshCw className="h-4 w-4" aria-hidden />{t.refresh}</Button>}>
      <p className="mb-3 text-sm text-muted-foreground">{t.auditHint}</p>
      <div className="mb-3 max-w-xs"><PresetSelect value={filters.preset} onChange={(preset) => setFilters((f) => ({ ...f, preset }))} /></div>
      {loading && <div aria-busy="true"><p role="status" className="mb-2 text-sm text-muted-foreground">{t.loading}</p><Skeleton className="h-40 w-full" /></div>}
      {!loading && err && <p role="alert" className="font-medium text-danger">{err}</p>}
      {!loading && !err && entries.length === 0 && <p className="text-muted-foreground">{t.auditEmpty}</p>}
      {!loading && !err && entries.length > 0 && (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-muted-foreground">
                <tr>{[c.time, c.user, c.action, c.target, c.client, c.result].map((h) => <th key={h} scope="col" className="whitespace-nowrap px-2 py-1 font-medium">{h}</th>)}</tr>
              </thead>
              <tbody>
                {entries.map((e, i) => {
                  const a = e.audit;
                  const fail = a?.result === 'fail';
                  return (
                    <tr key={`${e.time}-${i}`} className="border-t border-border/60 align-top">
                      <td className="whitespace-nowrap px-2 py-1.5 tabular-nums">{time(e.time)}</td>
                      <td className="px-2 py-1.5 break-all">{a?.user || '–'}</td>
                      <td className="px-2 py-1.5">{a ? auditActionLabel(a.action, t.actions) : e.message}</td>
                      <td className="px-2 py-1.5 break-all">{a?.target || '–'}</td>
                      <td className="px-2 py-1.5 font-mono text-xs break-all">{a?.client || '–'}</td>
                      <td className="px-2 py-1.5">{a?.result ? <Badge variant={fail ? 'danger' : 'success'}>{fail ? t.resultFail : t.resultOk}</Badge> : '–'}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <Footer n={entries.length} more={more} loadingOlder={loadingOlder} onOlder={() => void older()} />
        </>
      )}
    </Widget>
  );
}

function RetentionPanel({ readOnly }: { readOnly: boolean }) {
  const [saved, setSaved] = useState<LogRetention | null>(null);
  const [form, setForm] = useState<Pick<LogRetention, 'maxUse' | 'maxRetention'> | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState(false);
  const [busy, setBusy] = useState(false);
  const r = t.ret;

  const load = useCallback(async () => {
    try {
      const v = await api<LogRetention>('/logs/retention');
      setSaved(v); setForm({ maxUse: v.maxUse, maxRetention: v.maxRetention }); setErr(null);
    } catch { setErr(r.loadError); }
  }, [r.loadError]);
  useEffect(() => { void load(); }, [load]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!form || readOnly) return;
    setBusy(true); setErr(null); setOk(false);
    try {
      const v = await api<LogRetention>('/logs/retention', { method: 'PUT', body: form });
      setSaved(v); setForm({ maxUse: v.maxUse, maxRetention: v.maxRetention }); setOk(true);
    } catch (x) { setErr(x instanceof ApiError && x.status === 0 ? cs.errors.network : x instanceof ApiError && x.status === 400 && x.message ? x.message : r.saveError); }
    finally { setBusy(false); }
  }

  const dirty = saved != null && form != null && (form.maxUse !== saved.maxUse || form.maxRetention !== saved.maxRetention);
  return (
    <Widget id="logs-retention" title={r.title} icon={<Search />}>
      {!form && !err && <Skeleton className="h-24 w-full" />}
      {err && <p role="alert" className="mb-3 font-medium text-danger">{err}</p>}
      {form && saved && (
        <form onSubmit={(e) => void save(e)} className="max-w-xl space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={r.maxUse}>
              <select className={selectCls} disabled={readOnly || busy} value={form.maxUse} onChange={(e) => { setOk(false); setForm({ ...form, maxUse: e.target.value as LogRetention['maxUse'] }); }}>
                {LOG_MAX_USE.map((v) => <option key={v} value={v}>{r.uses[v]}</option>)}
              </select>
            </Field>
            <Field label={r.maxRetention}>
              <select className={selectCls} disabled={readOnly || busy} value={form.maxRetention} onChange={(e) => { setOk(false); setForm({ ...form, maxRetention: e.target.value as LogRetention['maxRetention'] }); }}>
                {LOG_MAX_RETENTION.map((v) => <option key={v} value={v}>{r.periods[v]}</option>)}
              </select>
            </Field>
          </div>
          <p className="text-sm"><span className="text-muted-foreground">{r.usage}: </span>{saved.usageBytes == null ? r.usageUnknown : formatBytes(saved.usageBytes)}</p>
          <p className="text-sm text-muted-foreground">{r.note}</p>
          {readOnly && <p className="text-sm text-muted-foreground">{r.readOnly}</p>}
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={readOnly || busy || !dirty}>{busy ? r.saving : r.save}</Button>
            <span role="status" className="text-sm text-success">{ok ? r.saved : ''}</span>
          </div>
        </form>
      )}
    </Widget>
  );
}

type Tab = 'journal' | 'audit' | 'retention';
const TABS: Tab[] = ['journal', 'audit', 'retention'];

export function LogsPage({ readOnly }: { readOnly: boolean }) {
  const [tab, setTab] = useState<Tab>('journal');
  return (
    <div className="space-y-4">
      <div role="tablist" aria-label={t.tabsLabel} className="flex flex-wrap gap-1 rounded-lg bg-muted p-1">
        {TABS.map((k) => (
          <button key={k} type="button" role="tab" id={`logs-tab-${k}`} aria-selected={tab === k} aria-controls={`logs-panel-${k}`} tabIndex={tab === k ? 0 : -1}
            onClick={() => setTab(k)}
            onKeyDown={(e) => {
              const d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
              if (d) { const n = TABS[(TABS.indexOf(k) + d + TABS.length) % TABS.length]!; setTab(n); document.getElementById(`logs-tab-${n}`)?.focus(); }
            }}
            className={cn('h-9 flex-1 rounded-md px-3 text-sm font-medium sm:flex-none', tab === k ? 'bg-background shadow-sm' : 'text-muted-foreground hover:text-foreground')}>
            {t.tabs[k]}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`logs-panel-${tab}`} aria-labelledby={`logs-tab-${tab}`}>
        {tab === 'journal' && <JournalPanel />}
        {tab === 'audit' && <AuditPanel />}
        {tab === 'retention' && <RetentionPanel readOnly={readOnly} />}
      </div>
    </div>
  );
}
