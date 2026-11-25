import { History, RefreshCw, RotateCcw, TriangleAlert } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ConfigDetail, ConfigEntry, ConfigFileDiff, ConfigHistory as History_, ConfigRollbackResult } from '../api/types';
import { compactDiff, diffStats, driftedFiles, filesToRestore } from '../lib/config';
import { formatBytes } from '../lib/format';
import { cs } from '../i18n/cs';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Skeleton } from './ui/skeleton';

const t = cs.config;
const dt = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '–' : d.toLocaleString('cs-CZ');
};
const fileLabel = (id: string) => t.files[id] ?? id;
const kindVariant = { baseline: 'neutral', change: 'success', manual: 'warning', rollback: 'neutral' } as const;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 404 || e.status === 409) return e.message;
  }
  return cs.errors.generic;
}

function KindBadge({ e }: { e: ConfigEntry }) {
  const text = e.kind === 'rollback' && e.rollbackOf ? t.rollbackOf(e.rollbackOf) : t.kinds[e.kind] ?? e.kind;
  return <Badge variant={kindVariant[e.kind] ?? 'neutral'}>{text}</Badge>;
}

function FileChange({ f }: { f: ConfigFileDiff }) {
  const { added, removed } = diffStats(f.lines);
  return (
    <section className="rounded-lg border border-border/70">
      <header className="flex flex-wrap items-baseline gap-x-2 border-b border-border/70 px-3 py-2">
        <h3 className="font-medium">{fileLabel(f.id)}</h3>
        <span className="font-mono text-xs text-muted-foreground break-all">{f.path}</span>
        {f.lines && <span className="ml-auto font-mono text-xs text-muted-foreground" aria-hidden>{t.stats(added, removed)}</span>}
      </header>
      {!f.present && <p className="px-3 pt-2 text-warning">{t.removedFile}</p>}
      {f.present && !f.currentPresent && <p className="px-3 pt-2">{t.createdFile}</p>}
      {f.tooLarge ? <p className="px-3 py-2 text-muted-foreground">{t.tooLarge}</p> : f.lines && f.lines.length > 0 && (
        <pre className="overflow-x-auto px-0 py-2 font-mono text-xs leading-5">
          {compactDiff(f.lines).map((l, i) => l === null
            ? <div key={i} className="px-3 text-muted-foreground">{t.folded}</div>
            : (
              <div key={i} className={`px-3 ${l.op === '+' ? 'bg-success/10 text-success' : l.op === '-' ? 'bg-danger/10 text-danger' : ''}`}>
                <span aria-hidden className="select-none pr-2">{l.op === ' ' ? ' ' : l.op === '-' ? '−' : '+'}</span>
                {l.op !== ' ' && <span className="sr-only">{l.op === '+' ? t.srAdded : t.srRemoved}</span>}
                {l.text || ' '}
              </div>
            ))}
        </pre>
      )}
    </section>
  );
}

function VersionDialog({ id, onClose, onDone }: { id: number; onClose: () => void; onDone: (r: ConfigRollbackResult) => void }) {
  const [detail, setDetail] = useState<ConfigDetail | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [takeover, setTakeover] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api<ConfigDetail>(`/config/history/${id}`).then((d) => { if (alive) setDetail(d); })
      .catch((e: unknown) => { if (alive) setLoadErr(apiMessage(e)); });
    return () => { alive = false; };
  }, [id]);

  const changes = detail ? filesToRestore(detail.files) : [];
  const drift = (detail?.drift.length ?? 0) > 0;
  const canRestore = changes.length > 0 && (!drift || takeover);

  async function restore() {
    setBusy(true); setErr(null);
    try {
      onDone(await api<ConfigRollbackResult>(`/config/rollback/${id}`, { method: 'POST', body: { takeover } }));
    } catch (e) { setErr(apiMessage(e)); }
    finally { setBusy(false); }
  }

  return (
    <Dialog title={t.dialogTitle(id)} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="button" onClick={() => void restore()} disabled={busy || !canRestore}>
          <RotateCcw className="h-4 w-4" aria-hidden />{busy ? t.restoring : t.confirm}
        </Button>
      </>}>
      {!detail ? (
        loadErr ? <p role="alert" className="text-danger">{loadErr}</p>
          : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-32 w-full" /></div>
      ) : (
        <>
          <p className="flex flex-wrap items-center gap-2 text-muted-foreground">
            <KindBadge e={detail.entry} />
            <span>{dt(detail.entry.time)} · {detail.entry.user === 'system' ? t.system : t.by(detail.entry.user)}</span>
          </p>
          {changes.length === 0 ? <p>{t.same}</p> : (
            <>
              <p>{t.willChange}</p>
              {changes.map((f) => <FileChange key={f.id} f={f} />)}
              {detail.files.filter((f) => f.same).length > 0 && (
                <p className="text-muted-foreground">{t.unchanged}: {detail.files.filter((f) => f.same).map((f) => fileLabel(f.id)).join(', ')}</p>
              )}
              {changes.some((f) => f.id === 'network') && <p role="note" className="rounded-md border border-warning/50 bg-warning/10 p-2">{t.networkWarn}</p>}
              {changes.some((f) => f.id === 'disk-power') && <p role="note" className="text-muted-foreground">{t.diskWarn}</p>}
              {drift && (
                <div className="space-y-2 rounded-md border border-warning/50 bg-warning/10 p-2">
                  <p>{t.driftWarn} ({detail.drift.map(fileLabel).join(', ')})</p>
                  <label className="flex items-center gap-2 font-medium">
                    <input type="checkbox" checked={takeover} onChange={(e) => setTakeover(e.target.checked)} className="h-4 w-4" />
                    {t.takeover}
                  </label>
                </div>
              )}
            </>
          )}
          {err && <p role="alert" className="font-medium text-danger">{err}</p>}
        </>
      )}
    </Dialog>
  );
}

export function ConfigHistoryWidget({ className }: { className?: string }) {
  const [data, setData] = useState<History_ | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
  const [msg, setMsg] = useState<string | null>(null);

  const load = useCallback(async () => {
    try { setData(await api<History_>('/config/history')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const drifted = data ? driftedFiles(data) : [];
  const pending = data?.drift.some((d) => d.state === 'pending') ?? false;

  return (
    <Widget id="sys-config" title={t.title} icon={<History />} className={className}
      right={<div className="flex items-center gap-2">
        {drifted.length > 0 && <Badge variant="warning">{t.driftTitle}</Badge>}
        <Button type="button" variant="outline" size="sm" onClick={() => { setMsg(null); void load(); }} aria-label={t.refresh}>
          <RefreshCw className="h-4 w-4" aria-hidden />
        </Button>
      </div>}>
      <p className="text-sm text-muted-foreground">{t.hint}</p>
      {msg && <p role="status" className="mt-3 text-sm text-success">{msg}</p>}
      {!data ? (
        failed ? <p role="alert" className="mt-3 text-sm text-danger">{t.loadError}</p>
          : <div aria-busy="true" aria-label={cs.common.loading} className="mt-3"><Skeleton className="h-32 w-full" /></div>
      ) : (
        <>
          {drifted.length > 0 && (
            <div role="note" className="mt-3 flex gap-2 rounded-md border border-warning/50 bg-warning/10 p-3 text-sm">
              <TriangleAlert className="h-5 w-5 shrink-0 text-warning" aria-hidden />
              <div>
                <p className="font-medium">{t.driftTitle}</p>
                <ul className="mt-1">{drifted.map((d) => <li key={d.id}>{fileLabel(d.id)} <span className="font-mono text-xs text-muted-foreground break-all">{d.path}</span></li>)}</ul>
                <p className="mt-1 text-muted-foreground">{t.driftBody}</p>
              </div>
            </div>
          )}
          {pending && <p className="mt-3 text-sm text-muted-foreground">{t.pendingBody}</p>}
          {data.entries.length === 0 ? <p className="mt-3 text-sm">{t.empty}</p> : (
            <ol className="mt-3 divide-y divide-border">
              {data.entries.map((e, i) => (
                <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
                  <span className="font-medium tabular-nums">{t.version(e.id)}</span>
                  <KindBadge e={e} />
                  {i === 0 && <Badge variant="success">{t.current}</Badge>}
                  <span className="text-muted-foreground">{dt(e.time)} · {e.user === 'system' ? t.system : t.by(e.user)}</span>
                  <span className="basis-full sm:basis-auto">{e.changed.length ? e.changed.map(fileLabel).join(', ') : t.nothingChanged}</span>
                  <Button type="button" variant="outline" size="sm" className="ml-auto" onClick={() => { setMsg(null); setOpen(e.id); }}>
                    {t.open}
                  </Button>
                </li>
              ))}
            </ol>
          )}
          <p className="mt-3 text-xs text-muted-foreground">{t.limits(data.maxEntries, formatBytes(data.usedBytes), formatBytes(data.maxBytes))}</p>
        </>
      )}
      {open !== null && (
        <VersionDialog id={open} onClose={() => setOpen(null)}
          onDone={(r) => {
            setOpen(null);
            setMsg(r.restored.length === 0 ? t.nothingDone : r.networkPending ? `${t.done(r.rollbackOf)} ${t.donePending}` : t.done(r.rollbackOf));
            void load();
          }} />
      )}
    </Widget>
  );
}
