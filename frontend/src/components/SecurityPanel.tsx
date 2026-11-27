import { LogOut, MonitorSmartphone, RefreshCw, ShieldAlert } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { FindingStatus, SecurityAdvisor, SecurityFinding, WebSession, WebSessions } from '../api/types';
import { cs } from '../i18n/cs';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Skeleton } from './ui/skeleton';

const t = cs.sec;
const dt = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '–' : d.toLocaleString('cs-CZ');
};
const variant: Record<FindingStatus, 'danger' | 'warning' | 'neutral' | 'success'> =
  { crit: 'danger', warn: 'warning', info: 'neutral', ok: 'success', unknown: 'neutral' };

const fill = (s: string, p?: Record<string, string>) => s.replace(/\{(\w+)\}/g, (_, k: string) => p?.[k] ?? '?');

function findingText(f: SecurityFinding): { title: string; text: string } {
  const m = t.findings[f.id];
  if (!m) return { title: f.id, text: '' };
  const text = f.status === 'ok' ? m.ok : f.status === 'unknown' ? '' : (f.status === 'warn' && m.warn) || m.bad;
  return { title: m.title, text: fill(text, f.params) };
}

function AdvisorWidget() {
  const [data, setData] = useState<SecurityAdvisor | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const load = useCallback(() => {
    setBusy(true); setFailed(false);
    api<SecurityAdvisor>('/security/advisor').then(setData).catch(() => setFailed(true)).finally(() => setBusy(false));
  }, []);
  useEffect(load, [load]);

  const bad = data?.findings.filter((f) => f.status === 'crit' || f.status === 'warn' || f.status === 'info').length ?? 0;
  return (
    <Widget id="sec-advisor" title={t.advisor} icon={<ShieldAlert />} className="lg:col-span-5"
      right={data && <Badge variant={bad === 0 ? 'success' : 'warning'}>{t.summary(bad)}</Badge>}>
      {!data ? (failed ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
        : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-32 w-full" /></div>) : (
        <>
          {bad === 0 && <p className="mb-3 text-sm text-success">{t.allOk}</p>}
          <ul className="divide-y divide-border">
            {data.findings.map((f) => {
              const { title, text } = findingText(f);
              return (
                <li key={f.id} className="flex flex-col gap-1 py-2 sm:flex-row sm:items-start sm:gap-3">
                  <Badge variant={variant[f.status]} className="w-fit shrink-0">{t.status[f.status]}</Badge>
                  <div className="min-w-0 flex-1 text-sm">
                    <p className="font-medium">{title}</p>
                    {text && <p className="text-muted-foreground">{text}</p>}
                  </div>
                  {f.fix && f.status !== 'ok' && f.status !== 'unknown' && (
                    <a href={f.fix} className="text-sm font-medium text-primary underline-offset-2 hover:underline">{t.fix}</a>
                  )}
                </li>
              );
            })}
          </ul>
          <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            <span>{t.checkedAt(dt(data.checkedAt))}</span>
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={load}>
              <RefreshCw className="h-4 w-4" aria-hidden />{t.refresh}
            </Button>
          </div>
        </>
      )}
    </Widget>
  );
}

type Confirm = { kind: 'one'; s: WebSession } | { kind: 'others' };

function SessionsWidget({ onSelfRevoked }: { onSelfRevoked: () => void }) {
  const [data, setData] = useState<WebSessions | null>(null);
  const [failed, setFailed] = useState(false);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const load = useCallback(() => {
    api<WebSessions>('/security/sessions').then((d) => { setData(d); setFailed(false); }).catch(() => setFailed(true));
  }, []);
  useEffect(load, [load]);

  async function run() {
    if (!confirm) return;
    setBusy(true); setMsg(null);
    try {
      if (confirm.kind === 'one') {
        await api(`/security/sessions/${encodeURIComponent(confirm.s.id)}`, { method: 'DELETE' });
        if (confirm.s.current) { onSelfRevoked(); return; }
      } else {
        const r = await api<{ revoked: number }>('/security/sessions/revoke-others', { method: 'POST' });
        setMsg({ ok: true, text: t.revokedOthers(r.revoked) });
      }
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError && e.status === 404 ? t.gone : cs.errors.generic });
    } finally {
      setBusy(false); setConfirm(null); load();
    }
  }

  const others = data ? data.sessions.filter((s) => !s.current).length : 0;
  return (
    <Widget id="sec-sessions" title={t.sessions} icon={<MonitorSmartphone />} className="lg:col-span-5">
      {!data ? (failed ? <p role="alert" className="text-sm text-danger">{t.sessionsLoadError}</p>
        : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-24 w-full" /></div>) : (
        <>
          <p className="mb-2 text-sm text-muted-foreground">
            {t.sessionsHint(Math.round(data.idleTimeoutSeconds / 60), Math.round(data.absoluteTimeoutSeconds / 3600))}
          </p>
          <ul className="divide-y divide-border">
            {data.sessions.map((s) => (
              <li key={s.id} className="flex flex-col gap-2 py-2 sm:flex-row sm:items-center sm:gap-3">
                <div className="min-w-0 flex-1 text-sm">
                  <p className="font-medium">
                    {s.username} <span className="font-mono text-muted-foreground">{s.client}</span>
                    {s.current && <Badge variant="success" className="ml-2">{t.you}</Badge>}
                  </p>
                  <p className="truncate text-muted-foreground" title={s.agent}>{s.agent || '–'}</p>
                  <p className="text-muted-foreground">{t.created}: {dt(s.createdAt)} · {t.lastSeen}: {dt(s.lastSeenAt)}</p>
                </div>
                <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => { setMsg(null); setConfirm({ kind: 'one', s }); }}>
                  <LogOut className="h-4 w-4" aria-hidden />{t.revoke}
                </Button>
              </li>
            ))}
          </ul>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Button type="button" variant="outline" disabled={others === 0} onClick={() => { setMsg(null); setConfirm({ kind: 'others' }); }}>
              {t.revokeOthers}
            </Button>
            {others === 0 && <span className="text-sm text-muted-foreground">{t.noOthers}</span>}
            {msg && <span role="status" className={`text-sm ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</span>}
          </div>
        </>
      )}
      {confirm && (
        <Dialog title={confirm.kind === 'one' ? t.revokeTitle : t.revokeOthersTitle} onClose={() => setConfirm(null)} busy={busy}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setConfirm(null)} disabled={busy}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void run()} disabled={busy}>{t.revoke}</Button>
          </>}>
          {confirm.kind === 'one' ? (
            <p>{t.revokeBody(confirm.s.username, confirm.s.client)}{confirm.s.current && <> {t.revokeOwnBody}</>}</p>
          ) : <p>{t.revokeOthersBody}</p>}
        </Dialog>
      )}
    </Widget>
  );
}

/** Security Center (W5.1): advisor for everyone who reads, sessions for administrators. */
export function SecurityPanel({ readOnly }: { readOnly: boolean }) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-5">
      <AdvisorWidget />
      {readOnly ? <p className="text-sm text-muted-foreground lg:col-span-5">{t.adminOnly}</p>
        : <SessionsWidget onSelfRevoked={() => location.reload()} />}
    </div>
  );
}
