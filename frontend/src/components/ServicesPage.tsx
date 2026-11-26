import { Activity, RefreshCw, ScrollText, ShieldCheck } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ServiceAction, ServiceInfo, ServiceList, ServiceLog } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { formatBytes, formatPercent } from '../lib/format';
import { groupPorts, needsConfirm, priorityTone, problems, sortServices, stateTone } from '../lib/services';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Skeleton } from './ui/skeleton';

const t = cs.services;
// systemctl show costs ~50 ms per unit on the NSA310; 10 s keeps an open page cheap.
const POLL_MS = 10_000;
const LOG_LINES = [50, 100, 200];

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const dateTime = (iso: string | null) => {
  const d = iso ? new Date(iso) : null;
  return d && !Number.isNaN(d.getTime()) ? d.toLocaleString('cs-CZ') : '–';
};

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words font-medium tabular-nums">{children}</dd>
    </div>
  );
}

function Ports({ s }: { s: ServiceInfo }) {
  const g = groupPorts(s.ports);
  if (g.length === 0) return <>{t.noPorts}</>;
  return (
    <>
      {g.map((p, i) => (
        <span key={`${p.proto}/${p.port}`}>
          {i > 0 && ', '}{p.proto.toUpperCase()} {p.port}
          {p.localOnly && <span className="text-xs font-normal text-muted-foreground"> ({t.localOnly})</span>}
        </span>
      ))}
    </>
  );
}

function ServiceRow({ s, readOnly, busy, onAction, onLog }: {
  s: ServiceInfo; readOnly: boolean; busy: boolean; onAction: (a: ServiceAction) => void; onLog: () => void;
}) {
  const installed = s.state !== 'not-installed';
  // A stopped unit keeps its last CPU counter; a share of CPU only means something while it runs.
  const running = s.state === 'active' || s.state === 'activating' || s.state === 'deactivating';
  return (
    <li className="space-y-2 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-semibold">{s.label}</span>
        <Badge variant={stateTone(s.state)}>{t.state[s.state] ?? s.state}</Badge>
        {s.protected && <Badge title={t.protectedHint}><ShieldCheck className="h-3 w-3" aria-hidden />{t.protectedBadge}</Badge>}
        <span className="text-xs text-muted-foreground">{s.name}</span>
      </div>
      {!installed ? <p className="text-sm text-muted-foreground">{t.notInstalled}</p> : (
        <>
          {s.description && <p className="text-sm text-muted-foreground">{s.description}</p>}
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3 lg:grid-cols-6">
            <Meta label={t.since}>{dateTime(s.since)}</Meta>
            <Meta label={t.pid}>{s.pid ? s.pid : '–'}</Meta>
            <Meta label={t.memory}>{s.memoryBytes != null ? formatBytes(s.memoryBytes) : '–'}</Meta>
            <Meta label={t.cpu}>{!running ? '–' : s.cpuPercent != null ? formatPercent(s.cpuPercent) : s.pid ? t.cpuPending : '–'}</Meta>
            <Meta label={t.ports}><Ports s={s} /></Meta>
            <Meta label={t.boot}>{t.enabled[s.enabled] ?? (s.enabled || '–')}</Meta>
          </dl>
          {s.result && <p role="note" className="text-sm text-danger">{t.lastError(s.result, s.exitStatus)}</p>}
          {!readOnly && (
            <div role="group" aria-label={t.actionsOf(s.label)} className="flex flex-wrap gap-2">
              {s.actions.map((a) => (
                <Button key={a} type="button" size="sm" variant="outline"
                  className={a === 'stop' || a === 'disable' ? 'text-danger' : undefined}
                  disabled={busy} onClick={() => onAction(a)}>
                  {t.action[a]}
                </Button>
              ))}
              <Button type="button" size="sm" variant="ghost" onClick={onLog}>
                <ScrollText className="h-4 w-4" aria-hidden />{t.log}
              </Button>
            </div>
          )}
        </>
      )}
    </li>
  );
}

function ConfirmDialog({ s, action, busy, err, onClose, onConfirm }: {
  s: ServiceInfo; action: ServiceAction; busy: boolean; err: string | null; onClose: () => void; onConfirm: () => void;
}) {
  const label = t.action[action] ?? action;
  const text = action === 'stop' ? t.confirm.stop : action === 'disable' ? t.confirm.disable : null;
  return (
    <Dialog title={t.confirm.title(label, s.label)} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="button" variant={action === 'stop' || action === 'disable' ? 'destructive' : 'default'} onClick={onConfirm} disabled={busy}>
          {busy ? t.actionBusy : label}
        </Button>
      </>}>
      {text && <p>{text}</p>}
      {s.protected && <p className="text-warning">{t.confirm.protected}</p>}
      {err && <p role="alert" className="font-medium text-danger">{err}</p>}
    </Dialog>
  );
}

function LogDialog({ s, onClose }: { s: ServiceInfo; onClose: () => void }) {
  const [lines, setLines] = useState(100);
  const [log, setLog] = useState<ServiceLog | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const end = useRef<HTMLLIElement>(null);

  const load = useCallback(async () => {
    setLoading(true); setErr(null);
    try { setLog(await api<ServiceLog>(`/services/${encodeURIComponent(s.name)}/log?lines=${lines}`)); }
    catch (e) { setErr(e instanceof ApiError && e.status === 403 ? t.logAdminOnly : t.logError + ' ' + apiMessage(e)); }
    finally { setLoading(false); }
  }, [s.name, lines]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { end.current?.scrollIntoView({ block: 'end' }); }, [log]);

  return (
    <Dialog title={t.logTitle(s.label)} onClose={onClose}
      footer={<>
        <label className="mr-auto flex items-center gap-2 text-sm">
          {t.logLines}
          <select className="h-8 rounded-md border border-border bg-background/80 px-2" value={lines} onChange={(e) => setLines(Number(e.target.value))}>
            {LOG_LINES.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <Button type="button" variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
          <RefreshCw className="h-4 w-4" aria-hidden />{t.logRefresh}
        </Button>
        <Button type="button" size="sm" onClick={onClose}>{cs.common.close}</Button>
      </>}>
      {err && <p role="alert" className="font-medium text-danger">{err}</p>}
      {!log && !err && <Skeleton className="h-40 w-full" />}
      {log && log.entries.length === 0 && <p className="text-muted-foreground">{t.logEmpty}</p>}
      {log && log.entries.length > 0 && (
        <ol aria-busy={loading} className="max-h-[55vh] overflow-y-auto rounded-md bg-muted/60 p-2 font-mono text-xs">
          {log.entries.map((e, i) => {
            const tone = priorityTone(e.priority);
            return (
              <li key={i} className={`whitespace-pre-wrap break-words py-0.5 ${tone === 'danger' ? 'text-danger' : tone === 'warning' ? 'text-warning' : ''}`}>
                <span className="text-muted-foreground">{new Date(e.time).toLocaleString('cs-CZ')}</span>{' '}
                {e.ident && <span className="text-muted-foreground">{e.ident}{e.pid ? `[${e.pid}]` : ''}: </span>}
                {e.message}
              </li>
            );
          })}
          <li ref={end} aria-hidden />
        </ol>
      )}
    </Dialog>
  );
}

type Open = { kind: 'confirm'; s: ServiceInfo; action: ServiceAction } | { kind: 'log'; s: ServiceInfo };

export function ServicesPage({ readOnly }: { readOnly: boolean }) {
  const [data, setData] = useState<ServiceList | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [open, setOpen] = useState<Open | null>(null);
  const loading = useRef(false);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try { setData(await api<ServiceList>('/services')); setFailed(false); }
    catch { setFailed(true); }
    finally { loading.current = false; }
  }, []);
  useVisiblePoll(() => void load(), POLL_MS);

  async function run(s: ServiceInfo, action: ServiceAction) {
    setBusy(s.name); setMsg(null); setErr(null);
    try {
      const n = await api<ServiceInfo>(`/services/${encodeURIComponent(s.name)}/${action}`, { method: 'POST' });
      setData((d) => d && { services: d.services.map((x) => (x.name === n.name ? n : x)) });
      setMsg(t.actionDone(s.label, t.action[action] ?? action));
      setOpen(null);
    } catch (e) {
      setErr(apiMessage(e));
      void load();
    } finally { setBusy(null); }
  }

  function act(s: ServiceInfo, action: ServiceAction) {
    if (needsConfirm(action, s)) { setErr(null); setOpen({ kind: 'confirm', s, action }); }
    else void run(s, action);
  }

  if (!data) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }
  const list = sortServices(data.services);
  const bad = problems(list);

  return (
    <div className="space-y-4">
      <Widget id="services-list" title={t.listTitle} icon={<Activity />}
        right={<Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => { setMsg(null); void load(); }}>
          <RefreshCw className="h-4 w-4" aria-hidden />
        </Button>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        <p className={`mt-2 text-sm font-medium ${bad.length ? 'text-danger' : 'text-success'}`}>
          {bad.length ? t.problems(bad.length) : t.allOk}
        </p>
        {failed && <p role="alert" className="mt-2 text-sm text-warning">{t.loadError}</p>}
        {msg && <p role="status" className="mt-2 text-sm text-success">{msg}</p>}
        {err && !open && <p role="alert" className="mt-2 text-sm font-medium text-danger">{err}</p>}
        <ul className="mt-2 divide-y divide-border">
          {list.map((s) => (
            <ServiceRow key={s.name} s={s} readOnly={readOnly} busy={busy !== null}
              onAction={(a) => act(s, a)} onLog={() => setOpen({ kind: 'log', s })} />
          ))}
        </ul>
      </Widget>
      {open?.kind === 'confirm' && (
        <ConfirmDialog s={open.s} action={open.action} busy={busy === open.s.name} err={err}
          onClose={() => { setOpen(null); setErr(null); }} onConfirm={() => void run(open.s, open.action)} />
      )}
      {open?.kind === 'log' && <LogDialog s={open.s} onClose={() => setOpen(null)} />}
    </div>
  );
}
