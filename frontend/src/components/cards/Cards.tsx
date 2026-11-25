import { Cpu, HardDrive, MemoryStick, Network, Server, Thermometer, Activity, ShieldCheck } from 'lucide-react';
import type { ReactNode } from 'react';
import type { Overview, Smart } from '../../api/types';
import { cs } from '../../i18n/cs';
import { DASH, formatBytes, formatNumber, formatPercent, formatSpeed, formatTemp, formatUptime, ratioPercent } from '../../lib/format';
import { Badge } from '../ui/badge';
import { Card, CardTitle } from '../ui/card';
import { Progress } from '../ui/progress';
import { Skeleton } from '../ui/skeleton';
import { Sparkline } from '../Sparkline';

const t = cs.overview;

const Row = ({ k, children }: { k: string; children: ReactNode }) => (
  <div className="flex items-baseline justify-between gap-3 py-0.5 text-sm">
    <dt className="text-muted-foreground">{k}</dt>
    <dd className="min-w-0 break-words text-right font-medium">{children}</dd>
  </div>
);

export const CardSkeleton = ({ title, icon }: { title: string; icon: ReactNode }) => (
  <Card aria-busy="true" aria-label={title}>
    <CardTitle>{icon}{title}</CardTitle>
    <Skeleton className="mb-2 h-5 w-2/3" /><Skeleton className="mb-2 h-5 w-1/2" /><Skeleton className="h-5 w-3/4" />
  </Card>
);

const ic = 'h-4 w-4';

export function SystemCard({ o }: { o: Overview }) {
  return (
    <Card aria-labelledby="c-sys">
      <CardTitle id="c-sys"><Server className={ic} aria-hidden />{t.system}</CardTitle>
      <dl>
        <Row k={t.hostname}>{o.host.hostname}</Row>
        <Row k={t.os}>{o.host.os}</Row>
        <Row k={t.kernel}>{o.host.kernel}</Row>
        <Row k={t.board}>{o.host.boardModel}</Row>
        <Row k={t.uptime}>{formatUptime(o.host.uptimeSeconds)}</Row>
      </dl>
    </Card>
  );
}

export function CpuCard({ o, history }: { o: Overview; history: number[] }) {
  const { cpu } = o;
  return (
    <Card aria-labelledby="c-cpu">
      <CardTitle id="c-cpu"><Cpu className={ic} aria-hidden />{t.cpu}</CardTitle>
      <div className="mb-1 text-3xl font-semibold tabular-nums">{formatPercent(cpu.usagePercent)}</div>
      <Sparkline values={history} label={t.cpuHistory} min={0} max={100} />
      <dl className="mt-3">
        <Row k={t.load}>{[cpu.load1, cpu.load5, cpu.load15].map((v) => formatNumber(v, 2)).join(' / ')}</Row>
        <Row k={t.cores}>{cpu.cores}</Row>
        <Row k="Model">{cpu.model}</Row>
      </dl>
    </Card>
  );
}

function UsageRow({ label, used, total }: { label: string; used: number | null; total: number | null }) {
  if (total == null || total <= 0 || used == null) {
    return <div className="py-1 text-sm"><span className="text-muted-foreground">{label}: </span>{DASH}</div>;
  }
  const p = ratioPercent(used, total);
  return (
    <div className="py-1.5">
      <div className="mb-1 flex justify-between text-sm">
        <span className="font-medium">{label}</span>
        <span className="tabular-nums text-muted-foreground">{formatBytes(used)} {t.of} {formatBytes(total)} ({formatPercent(p, 0)})</span>
      </div>
      <Progress value={p} label={t.ariaUsage(label, p)} />
    </div>
  );
}

export function MemoryCard({ o }: { o: Overview }) {
  const m = o.memory;
  const ramUsed = m.availableBytes == null ? null : m.totalBytes - m.availableBytes;
  const swapUsed = m.swapTotalBytes == null || m.swapFreeBytes == null ? null : m.swapTotalBytes - m.swapFreeBytes;
  return (
    <Card aria-labelledby="c-mem">
      <CardTitle id="c-mem"><MemoryStick className={ic} aria-hidden />{t.memory}</CardTitle>
      <UsageRow label={t.ram} used={ramUsed} total={m.totalBytes} />
      {m.swapTotalBytes === 0
        ? <p className="py-1 text-sm text-muted-foreground">{t.noSwap}</p>
        : <UsageRow label={t.swap} used={swapUsed} total={m.swapTotalBytes} />}
    </Card>
  );
}

export function SensorsCard({ o, history }: { o: Overview; history: number[] }) {
  const s = o.sensors;
  const bad = s.status !== 'ok';
  return (
    <Card aria-labelledby="c-sen">
      <CardTitle id="c-sen"><Thermometer className={ic} aria-hidden />{t.sensors}</CardTitle>
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
              <Row key={x.id} k={x.label}>{x.rpm == null ? DASH : `${formatNumber(x.rpm)} ${t.rpm}`}</Row>
            ))}
          </dl>
          <div className="mt-2"><Sparkline values={history} label={t.tempHistory} /></div>
        </>
      )}
    </Card>
  );
}

export function NetworkCard({ o }: { o: Overview }) {
  return (
    <Card aria-labelledby="c-net">
      <CardTitle id="c-net"><Network className={ic} aria-hidden />{t.network}</CardTitle>
      {o.network.length === 0 && <p className="text-sm text-muted-foreground">{t.noInterfaces}</p>}
      <ul className="divide-y divide-border">
        {o.network.map((n) => (
          <li key={n.name} className="py-2 first:pt-0 last:pb-0">
            <div className="flex items-center justify-between">
              <span className="font-semibold">{n.name}</span>
              <Badge variant={n.up ? 'success' : 'neutral'}>{n.up ? t.linkUp : t.linkDown}</Badge>
            </div>
            <dl className="mt-1">
              <Row k={t.speed}>{formatSpeed(n.speedMbps)}</Row>
              <Row k={t.received}>{formatBytes(n.rxBytes)}</Row>
              <Row k={t.sent}>{formatBytes(n.txBytes)}</Row>
              <Row k={t.addresses}>
                {n.addresses.length ? n.addresses.map((a) => <div key={a}>{a}</div>) : DASH}
              </Row>
            </dl>
          </li>
        ))}
      </ul>
    </Card>
  );
}

export function StorageCard({ o }: { o: Overview }) {
  const { disks, filesystems } = o.storage;
  return (
    <Card aria-labelledby="c-sto">
      <CardTitle id="c-sto"><HardDrive className={ic} aria-hidden />{t.storage}</CardTitle>
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
    </Card>
  );
}

export function SmartCard({ smart, error }: { smart: Smart | null; error: boolean }) {
  return (
    <Card aria-labelledby="c-smart">
      <CardTitle id="c-smart"><ShieldCheck className={ic} aria-hidden />{t.smart}</CardTitle>
      {error && !smart && <p className="text-sm text-danger">{t.smartError}</p>}
      {!smart && !error && <Skeleton className="h-16 w-full" />}
      {smart?.error && <Badge variant="warning">{smart.error === 'smartctl_unavailable' ? t.smartUnavailable : t.smartError}</Badge>}
      {smart && !smart.error && smart.disks.length === 0 && <p className="text-sm text-muted-foreground">{t.smartNoDisks}</p>}
      <ul className="divide-y divide-border">
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
    </Card>
  );
}

const stateVariant = { active: 'success', inactive: 'neutral', failed: 'danger', unknown: 'warning' } as const;

export function ServicesCard({ o }: { o: Overview }) {
  return (
    <Card aria-labelledby="c-svc">
      <CardTitle id="c-svc"><Activity className={ic} aria-hidden />{t.services}</CardTitle>
      {o.services.length === 0 && <p className="text-sm text-muted-foreground">{t.noServices}</p>}
      <ul className="space-y-1.5">
        {o.services.map((s) => (
          <li key={s.name} className="flex items-center justify-between text-sm">
            <span className="font-medium">{s.label}</span>
            <Badge variant={stateVariant[s.state] ?? 'warning'}>{t.serviceState[s.state] ?? t.serviceState.unknown}</Badge>
          </li>
        ))}
      </ul>
    </Card>
  );
}
