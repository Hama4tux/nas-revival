import { Cpu, HardDrive, MemoryStick, Network } from 'lucide-react';
import { useCallback, useRef, useState, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type { MonitorHistory, MonitorSample } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { formatBytes, formatNumber, formatPercent, ratioPercent } from '../lib/format';
import { cpuBusy, mergeHistory, monitorPath, names, peak, seriesOf, sum } from '../lib/monitor';
import { formatRate } from '../lib/netrate';
import { AreaChart } from './AreaChart';
import { Widget } from './ui/card';
import { Progress } from './ui/progress';
import { ProcessesPanel } from './ProcessesPanel';
import { Skeleton } from './ui/skeleton';

const t = cs.monitor;

function Row({ label, value, hint }: { label: ReactNode; value: string; hint?: string }) {
  return (
    <div className="flex justify-between gap-3 py-0.5 text-sm" title={hint}>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right font-medium tabular-nums">{value}</dd>
    </div>
  );
}

// The second series is amber: primary and accent are both blue and hard to tell apart in a legend.
const Dot = ({ tone }: { tone: 'primary' | 'warning' }) => (
  <span aria-hidden className={`mr-1.5 inline-block h-2 w-2 rounded-full ${tone === 'primary' ? 'bg-primary' : 'bg-warning'}`} />
);

/** Two-series chart with a legend, the peak and the time span below it. */
function Chart({ label, a, b, format, span }: {
  label: string; span: string; format: (v: number) => string;
  a: { name: string; values: number[] }; b: { name: string; values: number[] };
}) {
  return (
    <figure>
      <AreaChart label={label} series={[{ values: a.values, tone: 'primary' }, { values: b.values, tone: 'warning' }]} />
      <figcaption className="mt-1 flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span><Dot tone="primary" />{a.name}<span className="mx-2" /><Dot tone="warning" />{b.name}</span>
        <span className="tabular-nums">{t.peak} {format(Math.max(peak(a.values), peak(b.values)))} · {span}</span>
      </figcaption>
    </figure>
  );
}

function CpuCard({ samples, span }: { samples: MonitorSample[]; span: string }) {
  const c = samples[samples.length - 1]!.cpu;
  return (
    <Widget id="mon-cpu" title={t.cpu} icon={<Cpu />} right={<span className="text-lg font-semibold tabular-nums">{formatPercent(cpuBusy(c))}</span>}>
      <Chart label={t.cpuChart} span={span} format={(v) => formatPercent(v, 0)}
        a={{ name: t.cpuBusy, values: samples.map((s) => cpuBusy(s.cpu)) }}
        b={{ name: t.iowait, values: samples.map((s) => s.cpu.iowait) }} />
      <dl className="mt-3 grid gap-x-6 sm:grid-cols-2">
        <Row label={t.user} value={formatPercent(c.user)} />
        <Row label={t.system} value={formatPercent(c.system)} />
        <Row label={t.iowait} value={formatPercent(c.iowait)} hint={t.iowaitHint} />
        <Row label={t.irq} value={formatPercent(c.irq)} />
        {c.steal > 0 && <Row label={t.steal} value={formatPercent(c.steal)} />}
      </dl>
    </Widget>
  );
}

function MemoryCard({ h, span }: { h: MonitorHistory; span: string }) {
  const s = h.samples, m = s[s.length - 1]!.memory, psi = s[s.length - 1]!.psi;
  const total = h.memTotalBytes;
  const swapping = m.swapInBps + m.swapOutBps > 0;
  return (
    <Widget id="mon-mem" title={t.memory} icon={<MemoryStick />}
      right={<span className="tabular-nums text-muted-foreground">{formatBytes(m.usedBytes)} / {formatBytes(total)}</span>}>
      <Chart label={t.memChart} span={span} format={formatBytes}
        a={{ name: t.used, values: s.map((x) => x.memory.usedBytes) }}
        b={{ name: t.cache, values: s.map((x) => x.memory.cacheBytes) }} />
      <div className="mt-3 space-y-1">
        <Progress value={ratioPercent(m.usedBytes, total)} label={`${t.used} ${formatPercent(ratioPercent(m.usedBytes, total), 0)}`} />
      </div>
      <dl className="mt-2 grid gap-x-6 sm:grid-cols-2">
        <Row label={t.used} value={formatBytes(m.usedBytes)} />
        <Row label={t.cache} value={formatBytes(m.cacheBytes)} />
        <Row label={t.available} value={formatBytes(m.availableBytes)} />
        <Row label={t.swap} value={`${formatBytes(m.swapUsedBytes)} / ${formatBytes(h.swapTotalBytes)}`} />
        <Row label={t.swapIo} value={`${formatRate(m.swapInBps)} / ${formatRate(m.swapOutBps)}`} />
      </dl>
      {swapping && <p role="status" className="mt-2 text-sm text-warning">{t.swapActive}</p>}
      <h3 className="mt-4 text-sm font-semibold">{t.psi}</h3>
      {h.psiAvailable && psi ? (
        <dl className="mt-1 grid gap-x-6 sm:grid-cols-3">
          <Row label={t.psiCpu} value={formatPercent(psi.cpuSome)} hint={t.psiSome} />
          <Row label={t.psiMem} value={`${formatPercent(psi.memorySome)} / ${formatPercent(psi.memoryFull)}`} hint={`${t.psiSome} / ${t.psiFull}`} />
          <Row label={t.psiIo} value={`${formatPercent(psi.ioSome)} / ${formatPercent(psi.ioFull)}`} hint={`${t.psiSome} / ${t.psiFull}`} />
        </dl>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">{t.psiUnavailable}</p>
      )}
    </Widget>
  );
}

function DisksCard({ samples, span }: { samples: MonitorSample[]; span: string }) {
  const last = samples[samples.length - 1]!;
  const list = names(samples, 'disks');
  return (
    <Widget id="mon-disks" title={t.disks} icon={<HardDrive />}>
      {list.length === 0 && <p className="text-sm text-muted-foreground">{t.noDisks}</p>}
      <div className="space-y-5">
        {list.map((n) => {
          const d = last.disks.find((x) => x.name === n);
          return (
            <div key={n}>
              <h3 className="mb-2 font-mono text-sm font-semibold">{n}</h3>
              <Chart label={t.diskChart(n)} span={span} format={formatRate}
                a={{ name: t.read, values: seriesOf(samples, 'disks', n, (x) => x.readBps) }}
                b={{ name: t.write, values: seriesOf(samples, 'disks', n, (x) => x.writeBps) }} />
              {d && (
                <dl className="mt-2 grid gap-x-6 sm:grid-cols-2">
                  <Row label={<><Dot tone="primary" />{t.read}</>} value={formatRate(d.readBps)} />
                  <Row label={<><Dot tone="warning" />{t.write}</>} value={formatRate(d.writeBps)} />
                  <Row label={t.iops} value={`${formatNumber(d.readIops, 1)} / ${formatNumber(d.writeIops, 1)}`} />
                  <Row label={t.busy} value={formatPercent(d.busyPercent)} />
                  <Row label={t.await} value={`${formatNumber(d.awaitMs, 1)} ms`} />
                </dl>
              )}
            </div>
          );
        })}
      </div>
      <p className="mt-3 text-xs text-muted-foreground">{t.diskHint}</p>
    </Widget>
  );
}

function NetCard({ h, span }: { h: MonitorHistory; span: string }) {
  const samples = h.samples, last = samples[samples.length - 1]!;
  const list = names(samples, 'net');
  return (
    <Widget id="mon-net" title={t.network} icon={<Network />}>
      {list.length === 0 && <p className="text-sm text-muted-foreground">{t.noNet}</p>}
      <div className="space-y-5">
        {list.map((n) => {
          const x = last.net.find((i) => i.name === n);
          const tot = h.netTotals.find((i) => i.name === n);
          const errs = sum(seriesOf(samples, 'net', n, (i) => i.errors)), drops = sum(seriesOf(samples, 'net', n, (i) => i.drops));
          return (
            <div key={n}>
              <h3 className="mb-2 font-mono text-sm font-semibold">{n}</h3>
              <Chart label={t.netChart(n)} span={span} format={formatRate}
                a={{ name: t.rx, values: seriesOf(samples, 'net', n, (i) => i.rxBps) }}
                b={{ name: t.tx, values: seriesOf(samples, 'net', n, (i) => i.txBps) }} />
              <dl className="mt-2 grid gap-x-6 sm:grid-cols-2">
                {x && <Row label={<><Dot tone="primary" />{t.rx}</>} value={formatRate(x.rxBps)} />}
                {x && <Row label={<><Dot tone="warning" />{t.tx}</>} value={formatRate(x.txBps)} />}
                <Row label={t.errorsHour} value={`${errs} / ${drops}`} />
              </dl>
              {tot && (
                <p className="mt-1 text-xs text-muted-foreground tabular-nums">
                  {t.sinceBoot} {formatBytes(tot.rxBytes)}, {t.sent} {formatBytes(tot.txBytes)}, {t.errorsBoot(tot.rxErrors + tot.txErrors, tot.drops)}
                </p>
              )}
            </div>
          );
        })}
      </div>
    </Widget>
  );
}

function ResourcesPanel() {
  const [hist, setHist] = useState<MonitorHistory | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const ref = useRef<MonitorHistory | null>(null);
  const busy = useRef(false);

  const load = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const next = await api<MonitorHistory>(monitorPath(ref.current));
      ref.current = mergeHistory(ref.current, next);
      setHist(ref.current);
      setError(null);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(0, 'unknown', String(e)));
    } finally {
      busy.current = false;
    }
  }, []);
  // The server samples every 10 s; polling faster would only repeat the last point.
  useVisiblePoll(() => void load(), 10_000);

  if (!hist) {
    return error ? (
      <div role="alert" className="glass rounded-lg border-danger/40 p-3 text-sm text-danger">
        {error.status === 503 ? t.unavailable : t.loadError}
      </div>
    ) : (
      <div className="grid gap-4 lg:grid-cols-2" aria-busy="true" aria-label={cs.common.loading}>
        <Skeleton className="h-72 rounded-xl" /><Skeleton className="h-72 rounded-xl" />
      </div>
    );
  }
  const s = hist.samples;
  if (s.length === 0) return <p role="status" className="glass rounded-lg p-3 text-sm text-muted-foreground">{t.collecting}</p>;
  const minutes = Math.round((s[s.length - 1]!.t - s[0]!.t) / 60_000);
  const span = t.span(minutes);
  return (
    <div className="space-y-4">
      {error && <div role="alert" className="glass rounded-lg border-danger/40 p-3 text-sm text-danger">{t.loadError}</div>}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <CpuCard samples={s} span={span} />
        <MemoryCard h={hist} span={span} />
        <DisksCard samples={s} span={span} />
        <NetCard h={hist} span={span} />
      </div>
    </div>
  );
}

type Tab = 'resources' | 'processes';
const TABS: Tab[] = ['resources', 'processes'];

/** Monitor app: resource history and the process list; only the shown tab polls. */
export function MonitorPage({ readOnly }: { readOnly: boolean }) {
  const [tab, setTab] = useState<Tab>('resources');
  const refs = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});
  function onKey(e: React.KeyboardEvent) {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    const n = tab === 'resources' ? 'processes' : 'resources';
    setTab(n);
    refs.current[n]?.focus();
  }
  return (
    <div className="space-y-4">
      <div role="tablist" aria-label={cs.pages.monitor.title} className="flex flex-wrap gap-2" onKeyDown={onKey}>
        {TABS.map((k) => (
          <button key={k} ref={(el) => { refs.current[k] = el; }} type="button" role="tab" id={`mon-tab-${k}`}
            aria-selected={tab === k} aria-controls={`mon-panel-${k}`} tabIndex={tab === k ? 0 : -1}
            className={`rounded-md px-3 py-1.5 text-sm font-medium ${tab === k ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'}`}
            onClick={() => setTab(k)}>
            {t.tabs[k]}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`mon-panel-${tab}`} aria-labelledby={`mon-tab-${tab}`}>
        {tab === 'resources' ? <ResourcesPanel /> : <ProcessesPanel readOnly={readOnly} />}
      </div>
    </div>
  );
}
