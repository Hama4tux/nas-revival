import { Download, History, RefreshCw, RotateCcw, TriangleAlert, Upload } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ConfigDetail, ConfigEntry, ConfigFileDiff, ConfigHistory as History_, ConfigImportPreview, ConfigImportResult, ConfigRollbackResult } from '../api/types';
import { compactDiff, diffStats, driftedFiles, filesToRestore, readArchive } from '../lib/config';
import { formatBytes } from '../lib/format';
import { cs } from '../i18n/cs';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field } from './ui/field';
import { Skeleton } from './ui/skeleton';
import { FirewallPendingBox } from './FirewallPanel';

const t = cs.config;
const dt = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '–' : d.toLocaleString('cs-CZ');
};
const fileLabel = (id: string) => t.files[id] ?? id;
const kindVariant = { baseline: 'neutral', change: 'success', manual: 'warning', rollback: 'neutral', import: 'neutral' } as const;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 404 || e.status === 409) return e.message;
  }
  return cs.errors.generic;
}

/** The result message with the confirmations a restored network or firewall part still waits for. */
function withPending(text: string, r: { networkPending: unknown; firewallPending?: unknown }): string {
  return [text, r.networkPending ? t.donePending : '', r.firewallPending ? t.fwPending : ''].filter(Boolean).join(' ');
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
            <ChangeList files={detail.files} drift={detail.drift} takeover={takeover} setTakeover={setTakeover} intro={t.willChange} />
          )}
          {err && <p role="alert" className="font-medium text-danger">{err}</p>}
        </>
      )}
    </Dialog>
  );
}

/** Lists the files an import or rollback changes, the unchanged ones and the warnings. */
function ChangeList({ files, drift, takeover, setTakeover, intro }: {
  files: ConfigFileDiff[]; drift: string[]; takeover: boolean; setTakeover: (v: boolean) => void; intro: string;
}) {
  const changes = filesToRestore(files);
  const same = files.filter((f) => f.same);
  return (
    <>
      <p>{intro}</p>
      {changes.map((f) => <FileChange key={f.id} f={f} />)}
      {same.length > 0 && <p className="text-muted-foreground">{t.unchanged}: {same.map((f) => fileLabel(f.id)).join(', ')}</p>}
      {changes.some((f) => f.id === 'network') && <p role="note" className="rounded-md border border-warning/50 bg-warning/10 p-2">{t.networkWarn}</p>}
      {changes.some((f) => f.id === 'firewall') && <p role="note" className="rounded-md border border-warning/50 bg-warning/10 p-2">{t.firewallWarn}</p>}
      {changes.some((f) => f.id === 'disk-power') && <p role="note" className="text-muted-foreground">{t.diskWarn}</p>}
      {drift.length > 0 && (
        <div className="space-y-2 rounded-md border border-warning/50 bg-warning/10 p-2">
          <p>{t.driftWarn} ({drift.map(fileLabel).join(', ')})</p>
          <label className="flex items-center gap-2 font-medium">
            <input type="checkbox" checked={takeover} onChange={(e) => setTakeover(e.target.checked)} className="h-4 w-4" />
            {t.takeover}
          </label>
        </div>
      )}
    </>
  );
}

function ImportDialog({ onClose, onDone }: { onClose: () => void; onDone: (r: ConfigImportResult) => void }) {
  const [archive, setArchive] = useState<Record<string, unknown> | null>(null);
  const [preview, setPreview] = useState<ConfigImportPreview | null>(null);
  const [takeover, setTakeover] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function pick(f: File | undefined) {
    setArchive(null); setPreview(null); setErr(null); setTakeover(false);
    if (!f) return;
    const r = readArchive(await f.text());
    if ('error' in r) { setErr(t[r.error]); return; }
    setBusy(true);
    try {
      setPreview(await api<ConfigImportPreview>('/config/import/preview', { method: 'POST', body: { archive: r.archive } }));
      setArchive(r.archive);
    } catch (e) { setErr(apiMessage(e)); }
    finally { setBusy(false); }
  }

  async function submit() {
    if (!archive) return;
    setBusy(true); setErr(null);
    try { onDone(await api<ConfigImportResult>('/config/import', { method: 'POST', body: { archive, takeover } })); }
    catch (e) { setErr(apiMessage(e)); }
    finally { setBusy(false); }
  }

  const changes = preview ? filesToRestore(preview.files) : [];
  const canImport = !!archive && changes.length > 0 && (preview!.drift.length === 0 || takeover);
  return (
    <Dialog title={t.importTitle} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="button" onClick={() => void submit()} disabled={busy || !canImport}>
          <Upload className="h-4 w-4" aria-hidden />{busy && archive ? t.importing : t.importConfirm}
        </Button>
      </>}>
      <p className="text-muted-foreground">{t.importHint}</p>
      <Field label={t.importFile}>
        {(p) => <input {...p} type="file" accept=".json,application/json" data-autofocus className="block w-full text-sm"
          disabled={busy} onChange={(e) => void pick(e.target.files?.[0])} />}
      </Field>
      {busy && !archive && <p role="status" className="text-muted-foreground">{t.reading}</p>}
      {preview && (
        <>
          <p className="text-muted-foreground">{t.fileFrom(preview.hostname, dt(preview.created))}</p>
          {preview.otherHost && <p role="note" className="rounded-md border border-warning/50 bg-warning/10 p-2">{t.otherHost}</p>}
          {changes.length === 0 ? <p>{t.importSame}</p> : (
            <>
              <ChangeList files={preview.files} drift={preview.drift} takeover={takeover} setTakeover={setTakeover} intro={t.importWillChange} />
              <p className="text-muted-foreground">{t.importUndo}</p>
            </>
          )}
        </>
      )}
      {err && <p role="alert" className="font-medium text-danger">{err}</p>}
    </Dialog>
  );
}

export function ConfigHistoryWidget({ className, readOnly = false }: { className?: string; readOnly?: boolean }) {
  const [data, setData] = useState<History_ | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
  const [importing, setImporting] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [fwDeadline, setFwDeadline] = useState<string | null>(null);

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
      {!readOnly && (
        <section aria-labelledby="cfg-transfer" className="mt-3 rounded-lg border border-border/70 p-3 text-sm">
          <h3 id="cfg-transfer" className="font-medium">{t.transferTitle}</h3>
          <p className="mt-1 text-muted-foreground">{t.transferHint}</p>
          <div className="mt-2 flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm">
              <a href="/api/v1/config/export" download><Download className="h-4 w-4" aria-hidden />{t.exportBtn}</a>
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => { setMsg(null); setImporting(true); }}>
              <Upload className="h-4 w-4" aria-hidden />{t.importBtn}
            </Button>
          </div>
        </section>
      )}
      {msg && <p role="status" className="mt-3 text-sm text-success">{msg}</p>}
      {fwDeadline && (
        <div className="mt-3">
          <FirewallPendingBox deadline={fwDeadline}
            onConfirmed={() => { setFwDeadline(null); setMsg(t.fwConfirmed); void load(); }}
            onExpired={() => { setFwDeadline(null); setMsg(t.fwExpired); void load(); }} />
        </div>
      )}
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
      {importing && (
        <ImportDialog onClose={() => setImporting(false)}
          onDone={(r) => {
            setImporting(false);
            setMsg(r.restored.length === 0 ? t.importNothing : withPending(t.imported(r.restored.length), r));
            setFwDeadline(r.firewallPending?.deadline ?? null);
            void load();
          }} />
      )}
      {open !== null && (
        <VersionDialog id={open} onClose={() => setOpen(null)}
          onDone={(r) => {
            setOpen(null);
            setMsg(r.restored.length === 0 ? t.nothingDone : withPending(t.done(r.rollbackOf), r));
            setFwDeadline(r.firewallPending?.deadline ?? null);
            void load();
          }} />
      )}
    </Widget>
  );
}
