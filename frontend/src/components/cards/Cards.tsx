import { Cpu, HardDrive, Network, Server, Thermometer, Activity, ShieldCheck } from 'lucide-react';
import type { ReactNode } from 'react';
import type { Overview, Smart } from '../../api/types';
import { cs } from '../../i18n/cs';
import { DASH, formatBytes, formatNumber, formatPercent, formatSpeed, formatTemp, formatUptime, ratioPercent } from '../../lib/format';
import { Badge } from '../ui/badge';
import { Widget } from '../ui/card';
import { Progress } from '../ui/progress';
import { Skeleton } from '../ui/skeleton';
import type { NetRate } from '../../hooks/useOverview';
import { formatRate } from '../../lib/netrate';
import { Sparkline } from '../Sparkline';

const t = cs.overview;

const Row = ({ k, children }: { k: string; children: ReactNode }) => (
  <div className="flex items-baseline justify-between gap-3 py-0.5 text-sm">
    <dt className="text-muted-foreground">{k}</dt>
    <dd className="min-w-0 break-words text-right font-medium">{children}</dd>
  </div>
);

export const CardSkeleton = ({ title, icon, id }: { title: string; icon: ReactNode; id: string }) => (
  <Widget id={id} title={title} icon={icon} aria-busy="true">
    <Skeleton className="mb-2 h-5 w-2/3" /><Skeleton className="mb-2 h-5 w-1/2" /><Skeleton className="h-5 w-3/4" />
  </Widget>
);

export function SystemCard({ o }: { o: Overview }) {
  return (
    <Widget id="c-sys" title={t.system} icon={<Server />} tone="bg-slate-600">
      <dl>
        <Row k={t.hostname}>{o.host.hostname}</Row>
        <Row k={t.os}>{o.host.os}</Row>
        <Row k={t.kernel}>{o.host.kernel}</Row>
        <Row k={t.board}>{o.host.boardModel}</Row>
        <Row k={t.uptime}>{formatUptime(o.host.uptimeSeconds)}</Row>
      </dl>
    </Widget>
  );
}

function UsageRow({ label, used, total }: { label: string; used: number | null; total: number | null }) {
  if (total == null || total <= 0 || used == null) {
    return <div className="py-1 text-sm"><span className="text-muted-foreground">{label}: </span>{DASH}</div>;
  }
  const p = ratioPercent(used, total);
  return (
    <div className="py-1.5">
      <div className="mb-1 flex justify-between gap-2 text-sm">
        <span className="font-medium">{label}</span>
        <span className="tabular-nums text-muted-foreground">{formatBytes(used)} {t.of} {formatBytes(total)} ({formatPercent(p, 0)})</span>
      </div>
      <Progress value={p} label={t.ariaUsage(label, p)} />
    </div>
  );
}

export function ResourcesCard({ o, history }: { o: Overview; history: number[] }) {
  const { cpu, memory: m } = o;
  const ramUsed = m.availableBytes == null ? null : m.totalBytes - m.availableBytes;
  const swapUsed = m.swapTotalBytes == null || m.swapFreeBytes == null ? null : m.swapTotalBytes - m.swapFreeBytes;
  return (
    <Widget id="c-res" title={t.resources} icon={<Cpu />} tone="bg-blue-600">
      <div className="flex items-end justify-between gap-3">
        <div>
          <div className="text-xs text-muted-foreground">{t.cpu}</div>
          <div className="text-3xl font-semibold tabular-nums">{formatPercent(cpu.usagePercent)}</div>
        </div>
        <div className="w-1/2 max-w-[10rem]"><Sparkline values={history} label={t.cpuHistory} min={0} max={100} /></div>
      </div>
      <div className="mt-3">
        <UsageRow label={t.ram} used={ramUsed} total={m.totalBytes} />
        {m.swapTotalBytes === 0
          ? <p className="py-1 text-sm text-muted-foreground">{t.noSwap}</p>
          : <UsageRow label={t.swap} used={swapUsed} total={m.swapTotalBytes} />}
      </div>
      <dl className="mt-2 border-t border-border/60 pt-2">
        <Row k={t.load}>{[cpu.load1, cpu.load5, cpu.load15].map((v) => formatNumber(v, 2)).join(' / ')}</Row>
        <Row k={t.cores}>{cpu.cores}</Row>
        <Row k="Model">{cpu.model}</Row>
      </dl>
    </Widget>
  );
}

export function SensorsCard({ o, history }: { o: Overview; history: number[] }) {
  const s = o.sensors;
  const bad = s.status !== 'ok';
  return (
    <Widget id="c-sen" title={t.sensors} icon={<Thermometer />} tone="bg-orange-500">
      {bad ? (
        <Badge variant={s.status === 'fault' ? 'danger' : 'warning'} role="status">
          {t.sensorStatus[s.status] ?? t.sensorStatus.unknown}
        </Badge>
      ) : (
        <>
          <dl>
            {s.temperatures.length === 0 && <p className="text-sm text-muted-foreground">{t.noSensors}</p>}
            {s.temperatures.map((x) => (
              <Row key={x.id} k={x.label}>{formatTemp(x.milliCelsius)}</Row>
            ))}
            {s.fans.map((x) => (
              <Row key={x.id} k={x.label}>
                {x.rpm === 0 && <Badge variant="warning" className="mr-2">{t.fanStopped}</Badge>}
                {x.rpm == null ? DASH : `${formatNumber(x.rpm)} ${t.rpm}`}
              </Row>
            ))}
          </dl>
          <div className="mt-2"><Sparkline values={history} label={t.tempHistory} /></div>
        </>
      )}
    </Widget>
  );
}

export function NetworkCard({ o, rates, history }: { o: Overview; rates: Record<string, NetRate>; history: Record<string, number[]> }) {
  return (
    <Widget id="c-net" title={t.network} icon={<Network />} tone="bg-cyan-600">
      {o.network.length === 0 && <p className="text-sm text-muted-foreground">{t.noInterfaces}</p>}
      <ul className="divide-y divide-border/60">
        {o.network.map((n) => {
          const r = rates[n.name];
          return (
            <li key={n.name} className="py-2 first:pt-0 last:pb-0">
              <div className="flex items-center justify-between">
                <span className="font-semibold">{n.name}</span>
                <Badge variant={n.up ? 'success' : 'neutral'}>{n.up ? t.linkUp : t.linkDown}</Badge>
              </div>
              {n.up && (
                <div className="mt-2 flex items-center gap-3">
                  <dl className="shrink-0 text-sm tabular-nums">
                    <div className="flex gap-2"><dt className="w-20 text-muted-foreground">{t.down}</dt><dd className="font-medium">{formatRate(r?.rx)}</dd></div>
                    <div className="flex gap-2"><dt className="w-20 text-muted-foreground">{t.up}</dt><dd className="font-medium">{formatRate(r?.tx)}</dd></div>
                  </dl>
                  <div className="min-w-0 flex-1"><Sparkline values={history[n.name] ?? []} label={`${t.netHistory} ${n.name}`} min={0} /></div>
                </div>
              )}
              <dl className="mt-1">
                <Row k={t.speed}>{formatSpeed(n.speedMbps)}</Row>
                <Row k={t.received}>{formatBytes(n.rxBytes)}</Row>
                <Row k={t.sent}>{formatBytes(n.txBytes)}</Row>
                <Row k={t.addresses}>
                  {n.addresses.length ? n.addresses.map((a) => <div key={a}>{a}</div>) : DASH}
                </Row>
              </dl>
            </li>
          );
        })}
      </ul>
    </Widget>
  );
}

export function StorageCard({ o }: { o: Overview }) {
  const { disks, filesystems } = o.storage;
  return (
    <Widget id="c-sto" title={t.storage} icon={<HardDrive />} tone="bg-emerald-600">
      <h3 className="mb-1 text-sm font-medium">{t.disks}</h3>
      {disks.length === 0 && <p className="text-sm text-muted-foreground">{t.noDisks}</p>}
      <ul className="mb-3 space-y-1 text-sm">
        {disks.map((d) => (
          <li key={d.name} className="flex flex-wrap justify-between gap-x-3">
            <span><b>{d.name}</b> {d.model}</span>
            <span className="text-muted-foreground">
              {formatBytes(d.sizeBytes)} · {d.rotational ? t.hdd : t.ssd}{d.removable ? ` · ${t.removable}` : ''}
            </span>
          </li>
        ))}
      </ul>
      <h3 className="mb-1 text-sm font-medium">{t.filesystems}</h3>
      {filesystems.length === 0 && <p className="text-sm text-muted-foreground">{t.noFilesystems}</p>}
      <ul>
        {filesystems.map((f) => {
          const p = ratioPercent(f.usedBytes, f.totalBytes);
          return (
            <li key={f.mount} className="py-1.5">
              <div className="mb-1 flex flex-wrap items-center justify-between gap-x-3 text-sm">
                <span className="font-medium">{f.mount} <span className="font-normal text-muted-foreground">{f.fsType}</span></span>
                <span className="flex items-center gap-2 tabular-nums text-muted-foreground">
                  {p > 95 ? <Badge variant="danger">{t.critical}</Badge> : p > 85 ? <Badge variant="warning">{t.warnHigh}</Badge> : null}
                  {formatBytes(f.usedBytes)} {t.of} {formatBytes(f.totalBytes)} ({formatPercent(p, 0)})
                </span>
              </div>
              <Progress value={p} label={t.ariaUsage(f.mount, p)} />
            </li>
          );
        })}
      </ul>
    </Widget>
  );
}

export function SmartCard({ smart, error }: { smart: Smart | null; error: boolean }) {
  return (
    <Widget id="c-smart" title={t.smart} icon={<ShieldCheck />} tone="bg-teal-600">
      {error && !smart && <p className="text-sm text-danger">{t.smartError}</p>}
      {!smart && !error && <Skeleton className="h-16 w-full" />}
      {smart?.error && <Badge variant="warning">{smart.error === 'smartctl_unavailable' ? t.smartUnavailable : t.smartError}</Badge>}
      {smart && !smart.error && smart.disks.length === 0 && <p className="text-sm text-muted-foreground">{t.smartNoDisks}</p>}
      <ul className="divide-y divide-border/60">
        {smart?.disks.map((d) => (
          <li key={d.name} className="py-2 first:pt-0 last:pb-0">
            <div className="flex items-center justify-between gap-2">
              <span><b>{d.name}</b> <span className="text-sm text-muted-foreground">{d.model}</span></span>
              {d.standby
                ? <Badge>{t.standby}</Badge>
                : <Badge variant={d.health === 'passed' ? 'success' : d.health === 'failed' ? 'danger' : 'neutral'}>{t.health[d.health]}</Badge>}
            </div>
            {!d.standby && (
              <dl className="mt-1">
                <Row k={t.temp}>{d.temperatureCelsius == null ? DASH : `${formatNumber(d.temperatureCelsius)} °C`}</Row>
                <Row k={t.powerOn}>{d.powerOnHours == null ? DASH : `${formatNumber(d.powerOnHours)} ${t.hours}`}</Row>
                <Row k={t.reallocated}>
                  <span className={d.reallocatedSectors ? 'text-danger' : ''}>{formatNumber(d.reallocatedSectors)}</span>
                </Row>
                <Row k={t.pending}>
                  <span className={d.pendingSectors ? 'text-danger' : ''}>{formatNumber(d.pendingSectors)}</span>
                </Row>
              </dl>
            )}
          </li>
        ))}
      </ul>
    </Widget>
  );
}

const stateVariant = { active: 'success', inactive: 'neutral', failed: 'danger', unknown: 'warning' } as const;

export function ServicesCard({ o }: { o: Overview }) {
  return (
    <Widget id="c-svc" title={t.services} icon={<Activity />} tone="bg-rose-500">
      {o.services.length === 0 && <p className="text-sm text-muted-foreground">{t.noServices}</p>}
      <ul className="space-y-1.5">
        {o.services.map((s) => (
          <li key={s.name} className="flex items-center justify-between text-sm">
            <span className="font-medium">{s.label}</span>
            <Badge variant={stateVariant[s.state] ?? 'warning'}>{t.serviceState[s.state] ?? t.serviceState.unknown}</Badge>
          </li>
        ))}
      </ul>
    </Widget>
  );
}
