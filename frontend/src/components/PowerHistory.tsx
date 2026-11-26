import { AlertTriangle, RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { DiskPowerHistory } from '../api/types';
import { cs } from '../i18n/cs';
import { formatNumber, formatPercent } from '../lib/format';
import { perDay, recentChanges, timeline, type SegmentKind } from '../lib/powerHistory';
import { Button } from './ui/button';
import { Skeleton } from './ui/skeleton';

const t = cs.power.history;
const segCls: Record<SegmentKind, string> = { active: 'bg-primary', asleep: 'bg-muted-foreground/40', untracked: 'bg-transparent' };
const time = (ms: number) => new Date(ms).toLocaleString('cs-CZ', { day: 'numeric', month: 'numeric', hour: '2-digit', minute: '2-digit' });

function Counter({ label, value, rate, limit, warn, rateKnown }: {
  label: string; value: number | null; rate: number | null; limit: number; warn: boolean; rateKnown: boolean;
}) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">
        <b>{value == null ? t.noCounter : formatNumber(value)}</b>
        {rateKnown && rate != null && (
          <span className={`ml-2 text-xs ${warn ? 'font-medium text-warning' : 'text-muted-foreground'}`}>{t.perDay(perDay(rate), limit)}</span>
        )}
      </span>
    </div>
  );
}

/** PWR-04: 24 h timeline of the power state, wake counts and the SMART wear counters with their daily growth. */
export function PowerHistory({ diskId }: { diskId: string }) {
  const [data, setData] = useState<DiskPowerHistory | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const path = `/storage/disks/${encodeURIComponent(diskId)}/power/history`;

  const load = useCallback(async () => {
    setBusy(true);
    try {
      setData(await api<DiskPowerHistory>(path)); setNow(Date.now()); setFailed(false);
    } catch { setFailed(true); } finally { setBusy(false); }
  }, [path]);
  useEffect(() => { void load(); }, [load]);

  const head = (
    <div className="flex items-center justify-between gap-2">
      <h3 className="text-sm font-medium">{t.title}</h3>
      <Button type="button" size="sm" variant="outline" onClick={() => void load()} disabled={busy} aria-label={cs.power.refresh}>
        <RefreshCw className="h-4 w-4" aria-hidden />
      </Button>
    </div>
  );
  if (!data) {
    return (
      <section aria-label={t.title} className="space-y-3 border-t border-border pt-4">
        {head}
        {failed ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
          : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-24 w-full" /></div>}
      </section>
    );
  }

  const g = data.growth;
  const segs = timeline(data.events, now);
  const changes = recentChanges(data.events, 8);
  const rateKnown = g.spanHours > 0;

  return (
    <section aria-label={t.title} className="space-y-4 border-t border-border pt-4">
      {head}
      {data.events.length === 0 ? <p className="text-sm text-muted-foreground">{t.none}</p> : (
        <>
          <div>
            <div role="img" aria-label={`${t.timeline}: ${t.standbyShare} ${formatPercent(data.standbyPercent24h, 0)}`}
              className="flex h-4 w-full overflow-hidden rounded border border-border">
              {segs.map((s) => (
                <div key={s.from} className={segCls[s.kind]} style={{ width: `${s.percent}%` }}
                  title={`${t.legend[s.kind]}: ${time(s.from)} – ${time(s.to)}`} />
              ))}
            </div>
            <div className="mt-1 flex justify-between text-xs text-muted-foreground">
              <span>{t.from}</span>
              <span className="flex gap-3">
                <span className="flex items-center gap-1"><i className="inline-block h-2 w-3 rounded-sm bg-primary" />{t.legend.active}</span>
                <span className="flex items-center gap-1"><i className="inline-block h-2 w-3 rounded-sm bg-muted-foreground/40" />{t.legend.asleep}</span>
              </span>
              <span>{t.now}</span>
            </div>
          </div>
          <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
            <div><dt className="text-xs text-muted-foreground">{t.wakes24h}</dt><dd className="font-medium">{data.wakes24h}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t.wakes7d}</dt><dd className="font-medium">{data.wakes7d}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t.standbyShare}</dt><dd className="font-medium">{formatPercent(data.standbyPercent24h, 0)}</dd></div>
          </dl>
          {changes.length > 0 && (
            <details className="text-sm">
              <summary className="cursor-pointer text-muted-foreground">{t.changes}</summary>
              <ul className="mt-2 space-y-1">
                {changes.map((e) => (
                  <li key={e.t} className="flex justify-between gap-3"><span>{time(e.t)}</span><span>{cs.power.state[e.state]}</span></li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}

      <div className="space-y-2">
        <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t.wear}</h4>
        <Counter label={t.loadCycles} value={g.latestLoadCycles} rate={g.loadCyclesPerDay} limit={g.loadCycleLimit} warn={g.loadCyclesWarn} rateKnown={rateKnown} />
        <Counter label={t.startStop} value={g.latestStartStop} rate={g.startStopPerDay} limit={g.startStopLimit} warn={g.startStopWarn} rateKnown={rateKnown} />
        {!rateKnown && g.latestCounterAt != null && <p className="text-xs text-muted-foreground">{t.rateLater}</p>}
        {(g.loadCyclesWarn || g.startStopWarn) && (
          <div role="alert" className="space-y-1 rounded-lg border border-warning/50 bg-warning/10 p-3 text-sm">
            {g.loadCyclesWarn && <p className="flex gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />{t.warnLoad}</p>}
            {g.startStopWarn && <p className="flex gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />{t.warnStart}</p>}
          </div>
        )}
      </div>

      <p className="text-xs text-muted-foreground">
        {t.sampled(Math.round(data.sampleSeconds / 60))}
        {g.latestCounterAt != null && <> {t.counterAt(time(g.latestCounterAt))}</>}
        {data.trackedSince != null && <> {t.since(time(data.trackedSince))}.</>}
      </p>
    </section>
  );
}
