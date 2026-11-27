import { AlertTriangle, Download, PackageCheck, RefreshCw, RotateCcw, Tag } from 'lucide-react';
import { useCallback, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { UpdatesState } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { formatBytes } from '../lib/format';
import { plural, sortPackages, upgradePercent } from '../lib/updates';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Progress } from './ui/progress';
import { Skeleton } from './ui/skeleton';

const t = cs.upd;
const dt = (iso?: string) => {
  const d = iso ? new Date(iso) : null;
  return !d || Number.isNaN(d.getTime()) ? '–' : d.toLocaleString('cs-CZ');
};

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    return t.errors[e.code] ?? (e.message || cs.errors.generic);
  }
  return cs.errors.generic;
}

function LastRun({ s }: { s: UpdatesState }) {
  const u = s.upgrade;
  if (!u) return null;
  if (u.result === 'running') {
    return (
      <section aria-labelledby="upd-running" className="space-y-2 rounded-lg border border-primary/40 bg-primary/5 p-3 text-sm">
        <h3 id="upd-running" className="font-semibold">{t.running}</h3>
        <Progress value={upgradePercent(u)} label={t.running} warn={101} crit={101} />
        <p aria-live="polite" className="text-muted-foreground">{t.progress(u.done, u.total)}</p>
        <p className="text-muted-foreground">{t.confirmWarn}</p>
      </section>
    );
  }
  return (
    <p className="text-sm">
      <span className="font-medium">{t.lastRun}:</span> {dt(u.finishedAt ?? u.startedAt)}{' '}
      <Badge variant={u.result === 'ok' ? 'success' : 'danger'}>{u.result === 'ok' ? t.ok : t.failed}</Badge>
      {u.result === 'ok' && <span className="text-muted-foreground"> · {t.installed(u.packages?.length ?? u.done)}</span>}
      {u.message && <span className="mt-1 block whitespace-pre-wrap font-mono text-xs text-danger">{u.message}</span>}
    </p>
  );
}

function PackageList({ s }: { s: UpdatesState }) {
  const pkgs = sortPackages(s.packages);
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <caption className="sr-only">{t.title}</caption>
        <thead className="text-left text-muted-foreground">
          <tr><th scope="col" className="py-1 pr-3 font-medium">{t.pkg}</th><th scope="col" className="py-1 pr-3 font-medium">{t.version}</th>
            <th scope="col" className="hidden py-1 pr-3 font-medium sm:table-cell">{t.origin}</th><th scope="col" className="py-1 text-right font-medium">{t.size}</th></tr>
        </thead>
        <tbody className="divide-y divide-border">
          {pkgs.map((p) => (
            <tr key={p.name} className="align-top">
              <td className="py-1.5 pr-3">
                <span className="font-mono">{p.name}</span>
                {p.security && <Badge variant="warning" className="ml-2">{t.security}</Badge>}
                {!p.from && <Badge className="ml-2">{t.newPkg}</Badge>}
              </td>
              <td className="break-all py-1.5 pr-3 font-mono text-xs">{p.from ? `${p.from} → ${p.to}` : p.to}</td>
              <td className="hidden py-1.5 pr-3 text-xs text-muted-foreground sm:table-cell">{p.origin}</td>
              <td className="whitespace-nowrap py-1.5 text-right tabular-nums">{p.size > 0 ? formatBytes(p.size) : '–'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Update Manager (W5.4): Debian updates – check, list with security marked, install all; versions read-only. */
export function UpdatesPanel({ readOnly }: { readOnly: boolean }) {
  const [s, setS] = useState<UpdatesState | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  // systemd may report a just-started job as inactive for a moment: poll fast for a while after an action.
  const [fastUntil, setFastUntil] = useState(0);
  const load = useCallback(() => {
    api<UpdatesState>('/updates').then((d) => { setS(d); setFailed(false); }).catch(() => setFailed(true));
  }, []);
  // A running job is followed every 3 s; otherwise the page only refreshes now and then.
  useVisiblePoll(load, s?.running || Date.now() < fastUntil ? 3000 : 60000);

  async function act(path: string, body?: unknown) {
    setBusy(true); setMsg(null);
    try { setS(await api<UpdatesState>(path, { method: 'POST', body })); setFastUntil(Date.now() + 20000); }
    catch (e) { setMsg(apiMessage(e)); load(); }
    finally { setBusy(false); setConfirm(false); }
  }

  if (!s) {
    return failed ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-40 w-full" /></div>;
  }
  const sec = s.packages.filter((p) => p.security).length;
  const n = s.packages.length;
  const canAct = !readOnly && !s.running && !busy;
  return (
    <div className="grid items-start gap-4 lg:grid-cols-5">
      <Widget id="upd-debian" title={t.title} icon={<PackageCheck />} className="lg:col-span-5"
        right={n > 0 ? <Badge variant={sec > 0 ? 'warning' : 'neutral'}>{n}</Badge> : s.checkedAt && <Badge variant="success">{t.upToDate}</Badge>}>
        <div className="space-y-3">
          {s.restartRecommended && (
            <div className="flex items-start gap-2 rounded-lg border border-warning/50 bg-warning/10 p-3 text-sm">
              <RotateCcw className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
              <p><span className="font-semibold">{t.restart}: </span>
                {s.restartPackages.length > 0 ? t.restartBody.replace('{pkgs}', s.restartPackages.slice(0, 4).join(', ')) : t.restartGeneric}</p>
            </div>
          )}
          <LastRun s={s} />
          {s.running === 'check' && <p aria-live="polite" className="text-sm text-muted-foreground">{t.runningCheck}</p>}
          {s.checkError && (
            <div className="flex items-start gap-2 text-sm text-danger">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <p>{t.checkFailed} <span className="whitespace-pre-wrap font-mono text-xs">{s.checkError}</span></p>
            </div>
          )}
          <p className="text-sm">
            {n > 0 ? t.summary(n, sec, plural(n, ...t.pkgWords)) : s.checkedAt ? t.upToDate : t.neverChecked}
            {n > 0 && <span className="text-muted-foreground"> {t.download(formatBytes(s.downloadBytes))}, {t.free(formatBytes(s.freeBytes))}.</span>}
          </p>
          {n > 0 && <PackageList s={s} />}
          {s.keptBack.length > 0 && <p className="text-sm text-muted-foreground">{t.keptBack} <span className="font-mono">{s.keptBack.join(' ')}</span></p>}
          <div className="flex flex-wrap items-center gap-3">
            {!readOnly && (
              <>
                <Button type="button" variant="outline" disabled={!canAct} onClick={() => void act('/updates/check')}>
                  <RefreshCw className="h-4 w-4" aria-hidden />{s.running === 'check' ? t.checking : t.check}
                </Button>
                {n > 0 && (
                  <Button type="button" disabled={!canAct || !s.listedAt} onClick={() => { setMsg(null); setConfirm(true); }}>
                    <Download className="h-4 w-4" aria-hidden />{s.running === 'upgrade' ? t.installing : t.install}
                  </Button>
                )}
              </>
            )}
            <span className="text-sm text-muted-foreground">{s.checkedAt ? t.lastCheck(dt(s.checkedAt)) : ''}</span>
          </div>
          <div role="alert">{msg && <p className="text-sm font-medium text-danger">{msg}</p>}</div>
          <p className="text-xs text-muted-foreground">{t.auto}</p>
          {readOnly && <p className="text-sm text-muted-foreground">{t.readOnly}</p>}
        </div>
      </Widget>
      <Widget id="upd-versions" title={t.versions} icon={<Tag />} className="lg:col-span-5">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">{t.app}</dt><dd className="font-mono">{s.versions.app || '–'}</dd>
          <dt className="text-muted-foreground">{t.kernel}</dt><dd className="font-mono">{s.versions.kernel || '–'}</dd>
        </dl>
        <p className="mt-2 text-xs text-muted-foreground">{t.later}</p>
      </Widget>
      {confirm && s.listedAt && (
        <Dialog title={t.confirmTitle} onClose={() => setConfirm(false)} busy={busy}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setConfirm(false)} disabled={busy}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus disabled={busy} onClick={() => void act('/updates/upgrade', { listedAt: s.listedAt })}>{t.install}</Button>
          </>}>
          <div className="space-y-2 text-sm">
            <p>{t.confirmBody(n, sec, formatBytes(s.downloadBytes))}</p>
            <p className="font-medium text-warning">{t.confirmWarn}</p>
            <p className="text-muted-foreground">{t.confirmConf}</p>
          </div>
        </Dialog>
      )}
    </div>
  );
}
