import { Activity, ChevronRight, RefreshCw, ScrollText, ShieldCheck } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ServiceAction, ServiceInfo, ServiceList, ServiceLog } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { formatBytes, formatPercent } from '../lib/format';
import {
  groupPorts, isDangerAction, isProblem, isRunning, needsConfirm, portSummary, priorityTone, problems, sortServices, stateTone, type Tone,
} from '../lib/services';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { RowMenu, type RowMenuItem } from './ui/row-menu';
import { Skeleton } from './ui/skeleton';
import { NfsPanel } from './NfsPanel';
import { SmbPanel } from './SmbPanel';
import { SshPanel } from './SshPanel';

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

const DOT: Record<Tone, string> = {
  success: 'bg-success', warning: 'bg-warning', danger: 'bg-danger', neutral: 'bg-muted-foreground/50',
};

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
  const [open, setOpen] = useState(false);
  const installed = s.state !== 'not-installed';
  const running = isRunning(s);
  const tone = stateTone(s.state);
  const detailId = `svc-detail-${s.name}`;
  const summary = [portSummary(s.ports), running && s.memoryBytes != null ? formatBytes(s.memoryBytes) : '']
    .filter(Boolean).join(' · ');
  const items: RowMenuItem[] = [
    ...s.actions.map((a) => ({ key: a, label: t.action[a] ?? a, danger: isDangerAction(a), disabled: busy, onSelect: () => onAction(a) })),
    { key: 'log', label: t.log, icon: <ScrollText className="h-4 w-4" aria-hidden />, onSelect: onLog },
  ];
  return (
    <li className={isProblem(s) ? 'bg-danger/5' : undefined}>
      <div className="flex items-center gap-2 py-1.5 pr-1">
        <button type="button" disabled={!installed} aria-expanded={installed ? open : undefined}
          aria-controls={installed ? detailId : undefined} aria-label={installed ? t.detailsOf(s.label) : undefined}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 text-left hover:bg-muted/60 disabled:cursor-default disabled:hover:bg-transparent"
          onClick={() => setOpen((v) => !v)}>
          <ChevronRight className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? 'rotate-90' : ''} ${installed ? '' : 'invisible'}`} aria-hidden />
          <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[tone]}`} aria-hidden />
          <span className={`truncate font-medium ${installed ? '' : 'text-muted-foreground'}`}>{s.label}</span>
          {s.protected && <ShieldCheck className="h-4 w-4 shrink-0 text-muted-foreground" aria-label={t.protectedBadge} />}
          <span className="hidden truncate text-xs text-muted-foreground sm:inline">{s.name}</span>
          {summary && <span className="ml-auto hidden shrink-0 pl-2 text-xs tabular-nums text-muted-foreground md:inline">{summary}</span>}
        </button>
        <Badge variant={tone} className="shrink-0">{t.state[s.state] ?? s.state}</Badge>
        {!readOnly && installed
          ? <RowMenu label={t.actionsOf(s.label)} items={items} />
          : <span className="w-8 shrink-0" aria-hidden />}
      </div>
      {s.result && !open && <p role="note" className="truncate pb-2 pl-12 text-sm text-danger">{t.lastError(s.result, s.exitStatus)}</p>}
      {installed && open && (
        <div id={detailId} className="space-y-2 pb-3 pl-12 pr-2">
          {s.description && <p className="text-sm text-muted-foreground">{s.description}</p>}
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3 lg:grid-cols-6">
            <Meta label={t.since}>{dateTime(s.since)}</Meta>
            <Meta label={t.pid}>{s.pid ? s.pid : '–'}</Meta>
            <Meta label={t.memory}>{s.memoryBytes != null ? formatBytes(s.memoryBytes) : '–'}</Meta>
            <Meta label={t.cpu}>{!running ? '–' : s.cpuPercent != null ? formatPercent(s.cpuPercent) : s.pid ? t.cpuPending : '–'}</Meta>
            <Meta label={t.ports}><Ports s={s} /></Meta>
            <Meta label={t.boot}>{t.enabled[s.enabled] ?? (s.enabled || '–')}</Meta>
          </dl>
          {s.protected && <p className="text-xs text-muted-foreground">{t.protectedHint}</p>}
          {s.result && <p role="note" className="text-sm text-danger">{t.lastError(s.result, s.exitStatus)}</p>}
        </div>
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

function ServiceListPanel({ readOnly }: { readOnly: boolean }) {
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
        <ul className="-mx-2 mt-3 divide-y divide-border">
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

type Tab = 'list' | 'smb' | 'nfs' | 'ssh';
const TABS: Tab[] = ['list', 'smb', 'nfs', 'ssh'];

/** Services app: the unit list and the file service tabs (SMB, NFS, SSH/SFTP). */
export function ServicesPage({ readOnly }: { readOnly: boolean }) {
  const [tab, setTab] = useState<Tab>('list');
  const refs = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});
  function onKey(e: React.KeyboardEvent) {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    const i = TABS.indexOf(tab);
    const n = TABS[(i + (e.key === 'ArrowRight' ? 1 : TABS.length - 1)) % TABS.length] ?? 'list';
    setTab(n);
    refs.current[n]?.focus();
  }
  return (
    <div className="space-y-4">
      <div role="tablist" aria-label={cs.pages.services.title} className="flex flex-wrap gap-2" onKeyDown={onKey}>
        {TABS.map((k) => (
          <button key={k} ref={(el) => { refs.current[k] = el; }} type="button" role="tab" id={`svc-tab-${k}`}
            aria-selected={tab === k} aria-controls={`svc-panel-${k}`} tabIndex={tab === k ? 0 : -1}
            className={`rounded-md px-3 py-1.5 text-sm font-medium ${tab === k ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'}`}
            onClick={() => setTab(k)}>
            {cs.smb.tabs[k]}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`svc-panel-${tab}`} aria-labelledby={`svc-tab-${tab}`}>
        {tab === 'list' ? <ServiceListPanel readOnly={readOnly} />
          : tab === 'smb' ? <SmbPanel readOnly={readOnly} />
            : tab === 'nfs' ? <NfsPanel readOnly={readOnly} /> : <SshPanel readOnly={readOnly} />}
      </div>
    </div>
  );
}
