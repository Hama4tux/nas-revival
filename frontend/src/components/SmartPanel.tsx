import { Activity, AlertTriangle, RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SmartAttribute, SmartDetail, SmartTestStarted, SmartTestType } from '../api/types';
import { cs } from '../i18n/cs';
import { decodedDiffers, hoursAgo, orderAttributes, pollMs, testDonePercent } from '../lib/smart';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { Progress } from './ui/progress';
import { Skeleton } from './ui/skeleton';

const t = cs.smart;
const dash = '–';
const levelVariant = { crit: 'danger', warn: 'warning', info: 'neutral', ok: 'neutral' } as const;
const rowTint = { crit: 'bg-danger/10', warn: 'bg-warning/10', info: '', ok: '' } as const;

function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 404 || e.status === 409) return e.message;
  }
  return cs.errors.generic;
}

function AttrRow({ a }: { a: SmartAttribute }) {
  const norm = [a.value, a.worst, a.thresh].map((v) => v ?? dash).join(' / ');
  const flag = a.whenFailed === 'now' ? t.failedNow : a.whenFailed === 'past' ? t.failedPast : null;
  return (
    <tr className={`border-t border-border/70 ${rowTint[a.level]}`}>
      <td className="py-1.5 pr-2 tabular-nums text-muted-foreground">{a.id}</td>
      <td className="py-1.5 pr-2">
        <span className="font-medium">{t.names[a.id] ?? a.name}</span>
        {t.names[a.id] && <span className="ml-1 hidden text-xs text-muted-foreground sm:inline">{a.name}</span>}
        <span className="ml-1 inline-flex flex-wrap gap-1 align-middle">
          {t.level[a.level] && <Badge variant={levelVariant[a.level]}>{t.level[a.level]}</Badge>}
          {flag && <Badge variant="danger">{flag}</Badge>}
        </span>
      </td>
      <td className="py-1.5 pr-2 text-right font-medium tabular-nums">{a.count.toLocaleString('cs-CZ')}</td>
      <td className="hidden py-1.5 pr-2 text-right tabular-nums text-muted-foreground md:table-cell">{norm}</td>
      <td className="hidden py-1.5 text-right font-mono text-xs text-muted-foreground sm:table-cell">
        {decodedDiffers(a) ? a.raw : ''}
      </td>
    </tr>
  );
}

export function SmartPanel({ diskId }: { diskId: string }) {
  const [data, setData] = useState<SmartDetail | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [all, setAll] = useState(false);
  const [confirm, setConfirm] = useState<SmartTestType | null>(null);
  const [starting, setStarting] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const path = `/storage/disks/${encodeURIComponent(diskId)}/smart`;

  const load = useCallback(async () => {
    setBusy(true);
    try { setData(await api<SmartDetail>(path)); setFailed(false); } catch { setFailed(true); } finally { setBusy(false); }
  }, [path]);
  useEffect(() => { void load(); }, [load]);

  // Refresh progress while a test runs and the page is visible.
  const every = pollMs(data);
  useEffect(() => {
    if (every == null) return;
    const id = window.setInterval(() => { if (document.visibilityState === 'visible') void load(); }, every);
    return () => clearInterval(id);
  }, [every, load]);

  async function start(type: SmartTestType) {
    setStarting(true); setMsg(null);
    try {
      const r = await api<SmartTestStarted>(`${path}/test`, { method: 'POST', body: { type } });
      setMsg({ ok: true, text: t.started(r.estimatedMinutes) });
      setConfirm(null);
      await load();
    } catch (e) {
      setMsg({ ok: false, text: errorText(e) });
      setConfirm(null);
    } finally {
      setStarting(false);
    }
  }

  if (!data) {
    return failed
      ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-40 w-full" /></div>;
  }

  const done = testDonePercent(data);
  const attrs = orderAttributes(data.attributes, all);
  const minutes = confirm === 'long' ? data.longMinutes : data.shortMinutes;
  const healthVariant = data.health === 'passed' ? 'success' : data.health === 'failed' ? 'danger' : 'neutral';
  const canTest = !data.error && (data.standby || data.selfTestsSupported);

  return (
    <section aria-label={t.title} className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
        <Badge variant={healthVariant}>{t.health[data.health]}</Badge>
        {data.temperatureCelsius != null && <span>{t.temp}: <b className="tabular-nums">{data.temperatureCelsius} °C</b></span>}
        {data.powerOnHours != null && <span>{t.hours}: <b className="tabular-nums">{data.powerOnHours.toLocaleString('cs-CZ')} h</b></span>}
        {data.powerCycles != null && <span>{t.cycles}: <b className="tabular-nums">{data.powerCycles.toLocaleString('cs-CZ')}</b></span>}
        <span className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
          {t.checkedAt(new Date(data.checkedAt).toLocaleTimeString('cs-CZ'))}
          <Button type="button" size="sm" variant="outline" onClick={() => void load()} disabled={busy} aria-label={t.refresh}>
            <RefreshCw className="h-4 w-4" aria-hidden />
          </Button>
        </span>
      </div>

      {data.error && <p role="alert" className="text-sm text-danger">{data.error === 'smartctl_unavailable' ? t.unavailable : t.failed}</p>}
      {data.standby && <p role="note" className="rounded-lg border border-border p-3 text-sm text-muted-foreground">{t.standby}</p>}
      {failed && <p role="alert" className="text-sm text-danger">{t.loadError}</p>}

      <div>
        <h4 className="mb-2 flex items-center gap-2 text-sm font-medium"><Activity className="h-4 w-4" aria-hidden />{t.selfTest}</h4>
        {done != null ? (
          <div className="space-y-1" aria-live="polite">
            <p className="text-sm">{t.running(done)}</p>
            <Progress value={done} label={t.progressLabel} warn={101} crit={101} />
          </div>
        ) : data.selfTest && (
          <p className="text-sm text-muted-foreground">{t.lastResult}: {data.selfTest.text}</p>
        )}
        {!data.standby && !data.error && !data.selfTestsSupported && <p className="text-sm text-muted-foreground">{t.noSelfTests}</p>}
        {canTest && (
          <div className="mt-3 flex flex-wrap gap-2">
            {(['short', 'long'] as const).map((type) => (
              <Button key={type} type="button" variant="outline" disabled={done != null || starting} onClick={() => setConfirm(type)}>
                {t[type]} <span className="text-muted-foreground">{t.minutes(type === 'long' ? data.longMinutes : data.shortMinutes)}</span>
              </Button>
            ))}
          </div>
        )}
        {msg && <p role={msg.ok ? 'status' : 'alert'} className={`mt-2 text-sm ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</p>}
      </div>

      {data.attributes.length > 0 ? (
        <div>
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h4 className="text-sm font-medium">{t.attributes}</h4>
            <Button type="button" size="sm" variant="ghost" onClick={() => setAll((v) => !v)} aria-pressed={all}>
              {all ? t.showKey : t.showAll(data.attributes.length)}
            </Button>
          </div>
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="pb-1 pr-2 font-normal">{t.col.id}</th>
                <th scope="col" className="pb-1 pr-2 font-normal">{t.col.name}</th>
                <th scope="col" className="pb-1 pr-2 text-right font-normal">{t.col.count}</th>
                <th scope="col" className="hidden pb-1 pr-2 text-right font-normal md:table-cell">{t.col.norm}</th>
                <th scope="col" className="hidden pb-1 text-right font-normal sm:table-cell">{t.col.raw}</th>
              </tr>
            </thead>
            <tbody>{attrs.map((a) => <AttrRow key={a.id} a={a} />)}</tbody>
          </table>
        </div>
      ) : !data.standby && !data.error && <p className="text-sm text-muted-foreground">{t.noAttributes}</p>}

      {!data.standby && !data.error && (
        <div>
          <h4 className="mb-2 text-sm font-medium">{t.history}</h4>
          {data.history.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t.noHistory}</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {data.history.map((h, i) => (
                <li key={i} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border/70 py-1.5 first:border-t-0">
                  <span className="w-20 font-medium">{t.types[h.type]}</span>
                  <Badge variant={h.passed ? 'success' : 'warning'}>{h.passed ? t.passed : t.notPassed}</Badge>
                  <span className="text-muted-foreground">
                    {h.status}{h.remainingPercent != null && !h.passed ? ` · ${t.remaining(h.remainingPercent)}` : ''}
                  </span>
                  <span className="ml-auto text-xs tabular-nums text-muted-foreground">{t.ago(hoursAgo(data, h.lifetimeHours), h.lifetimeHours)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {confirm && (
        <Dialog title={t.confirmTitle(confirm === 'long')} onClose={() => setConfirm(null)} busy={starting}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setConfirm(null)} disabled={starting}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void start(confirm)} disabled={starting}>
              {starting ? t.starting : t.start}
            </Button>
          </>}>
          <p className="flex gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{t.confirmBody(confirm === 'long', minutes)}</span>
          </p>
        </Dialog>
      )}
    </section>
  );
}
