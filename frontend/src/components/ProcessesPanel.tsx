import { ListTree, Lock } from 'lucide-react';
import { useCallback, useMemo, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ProcessInfo, ProcessKillResult, ProcessList } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { formatBytes, formatPercent } from '../lib/format';
import { filterProcesses, killPath, sortProcesses, userProcesses, type ProcessSort } from '../lib/processes';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.processes;
const SORTS: ProcessSort[] = ['cpu', 'memory', 'name', 'pid'];
const PAGE = 50;

function KillDialog({ p, busy, err, onClose, onKill }: {
  p: ProcessInfo; busy: boolean; err: string | null; onClose: () => void; onKill: (force: boolean) => void;
}) {
  return (
    <Dialog title={t.confirmTitle(p.name)} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="button" variant="outline" className="text-danger" onClick={() => onKill(true)} disabled={busy}>{t.force}</Button>
        <Button type="button" variant="destructive" onClick={() => onKill(false)} disabled={busy} data-autofocus>
          {busy ? t.stopping : t.stop}
        </Button>
      </>}>
      <p className="break-all font-mono text-xs">{p.cmd}</p>
      <p className="tabular-nums text-muted-foreground">{t.pid} {p.pid} · {p.user} · {formatBytes(p.rssBytes)}</p>
      <p>{t.confirmText}</p>
      <p className="text-muted-foreground">{t.forceText}</p>
      {err && <p role="alert" className="font-medium text-danger">{err}</p>}
    </Dialog>
  );
}

function Row({ p, readOnly, onStop }: { p: ProcessInfo; readOnly: boolean; onStop: () => void }) {
  const reason = p.protected ? t.protectedReason[p.protected] : undefined;
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_3.5rem_4.5rem] items-center gap-x-3 py-2 sm:grid-cols-[minmax(0,1fr)_3.5rem_4.5rem_6.5rem]">
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2">
          <span className="max-w-full truncate font-medium">{p.name}</span>
          {reason && <Badge title={reason}><Lock className="h-3 w-3" aria-hidden />{t.protectedBadge}</Badge>}
        </div>
        <p className="truncate font-mono text-xs text-muted-foreground" title={p.cmd}>{p.cmd}</p>
        <p className="text-xs text-muted-foreground tabular-nums">
          {t.pid} {p.pid} · {p.user} · {t.stateNames[p.state] ?? p.state}{p.threads > 1 ? ` · ${t.threads.toLowerCase()} ${p.threads}` : ''}
        </p>
      </div>
      <span className="text-right text-sm tabular-nums">{formatPercent(p.cpuPercent)}</span>
      <span className="text-right text-sm tabular-nums">{formatBytes(p.rssBytes)}</span>
      {!readOnly && (
        <div className="col-span-3 mt-1 flex justify-end sm:col-span-1 sm:mt-0">
          {!p.protected && (
            <Button type="button" size="sm" variant="outline" onClick={onStop} aria-label={t.stopLabel(p.name, p.pid)}>{t.stop}</Button>
          )}
        </div>
      )}
    </li>
  );
}

/** Process list (W3.2): polled every 5 s only while this tab is shown. */
export function ProcessesPanel({ readOnly }: { readOnly: boolean }) {
  const [list, setList] = useState<ProcessList | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<ProcessSort>('cpu');
  const [kernel, setKernel] = useState(false);
  const [all, setAll] = useState(false);
  const [open, setOpen] = useState<ProcessInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const loading = useRef(false);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try {
      setList(await api<ProcessList>('/monitor/processes'));
      setError(null);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, 'unknown', String(e)));
    } finally {
      loading.current = false;
    }
  }, []);
  useVisiblePoll(() => void load(), 5_000);

  const shown = useMemo(() => {
    if (!list) return [];
    return sortProcesses(filterProcesses(userProcesses(list.processes, kernel), query), sort);
  }, [list, kernel, query, sort]);

  async function kill(p: ProcessInfo, force: boolean) {
    setBusy(true);
    setErr(null);
    try {
      const r = await api<ProcessKillResult>(killPath(p.pid), { method: 'POST', body: { start: p.start, force } });
      setOpen(null);
      setNotice(t.done(r.name, r.pid, r.signal));
      void load();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  if (!list) {
    return error ? (
      <div role="alert" className="glass rounded-lg border-danger/40 p-3 text-sm text-danger">
        {error.status === 503 ? t.unavailable : t.loadError}
      </div>
    ) : <Skeleton className="h-96 rounded-xl" aria-busy="true" aria-label={cs.common.loading} />;
  }
  const total = userProcesses(list.processes, kernel).length;
  const rows = all ? shown : shown.slice(0, PAGE);
  return (
    <Widget id="mon-procs" title={t.title} icon={<ListTree />}
      right={<span className="tabular-nums text-muted-foreground">{t.count(shown.length, total)}</span>}>
      {error && <p role="alert" className="mb-2 text-sm text-danger">{t.loadError}</p>}
      {notice && <p role="status" className="mb-2 text-sm text-success">{notice}</p>}
      <div className="flex flex-wrap items-end gap-3">
        <label className="min-w-0 flex-1 basis-56 text-sm">
          <span className="mb-1 block text-muted-foreground">{t.search}</span>
          <Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t.searchPlaceholder} />
        </label>
        <div role="group" aria-label={t.sortBy} className="flex flex-wrap gap-1">
          {SORTS.map((k) => (
            <button key={k} type="button" aria-pressed={sort === k} onClick={() => setSort(k)}
              className={`rounded-md px-3 py-1.5 text-sm font-medium ${sort === k ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'}`}>
              {t.sort[k]}
            </button>
          ))}
        </div>
      </div>
      <label className="mt-3 flex items-center gap-2 text-sm">
        <input type="checkbox" checked={kernel} onChange={(e) => setKernel(e.target.checked)} />
        {t.showKernel}
      </label>
      <div className="mt-3 grid grid-cols-[minmax(0,1fr)_3.5rem_4.5rem] gap-x-3 border-b border-border/70 pb-1 text-xs font-medium text-muted-foreground sm:grid-cols-[minmax(0,1fr)_3.5rem_4.5rem_6.5rem]" aria-hidden>
        <span>{t.name}</span><span className="text-right">{t.cpu}</span><span className="text-right">{t.memory}</span>
      </div>
      {rows.length === 0 && <p className="py-3 text-sm text-muted-foreground">{t.none}</p>}
      <ul className="divide-y divide-border/60">
        {rows.map((p) => (
          <Row key={`${p.pid}-${p.start}`} p={p} readOnly={readOnly} onStop={() => { setErr(null); setNotice(null); setOpen(p); }} />
        ))}
      </ul>
      {!all && shown.length > PAGE && (
        <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => setAll(true)}>{t.showAll(shown.length)}</Button>
      )}
      <p className="mt-3 text-xs text-muted-foreground">{t.hint}{readOnly ? ` ${t.readOnly}` : ''}</p>
      {open && <KillDialog p={open} busy={busy} err={err} onClose={() => setOpen(null)} onKill={(f) => void kill(open, f)} />}
    </Widget>
  );
}
