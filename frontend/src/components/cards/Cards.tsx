import {
  Activity, ArrowDown, ArrowUp, Cpu, Database, FileText, HardDrive, Info, MemoryStick, Network, Thermometer, Usb,
} from 'lucide-react';
import type { ReactNode } from 'react';
import type { Overview, Smart } from '../../api/types';
import type { NetHistory, NetRate } from '../../hooks/useOverview';
import { cs } from '../../i18n/cs';
import { deriveEvents } from '../../lib/events';
import {
  DASH, formatBytes, formatNumber, formatPercent, formatSpeed, formatTemp, ratioPercent,
} from '../../lib/format';
import { formatRate } from '../../lib/netrate';
import { cn } from '../../lib/utils';
import { AreaChart } from '../AreaChart';
import { Sparkline } from '../Sparkline';
import { Badge } from '../ui/badge';
import { Widget } from '../ui/card';
import { Progress } from '../ui/progress';
import { Skeleton } from '../ui/skeleton';

const t = cs.overview;

export const CardSkeleton = ({ title, icon, id, className }: { title: string; icon: ReactNode; id: string; className?: string }) => (
  <Widget id={id} title={title} icon={icon} aria-busy="true" className={className}>
    <Skeleton className="mb-2 h-5 w-2/3" /><Skeleton className="mb-2 h-5 w-1/2" /><Skeleton className="h-5 w-3/4" />
  </Widget>
);

/* ---------- top tiles: CPU, RAM, swap ---------- */

function Tile({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="glass flex gap-4 rounded-xl p-4">
      <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary [&>svg]:h-6 [&>svg]:w-6" aria-hidden>
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <h2 className="text-sm font-semibold">{title}</h2>
        {children}
      </div>
    </section>
  );
}

function UsageTile({ icon, title, used, total, empty }: { icon: ReactNode; title: string; used: number | null; total: number | null; empty?: string }) {
  const known = used != null && total != null && total > 0;
  const p = known ? ratioPercent(used, total) : 0;
  return (
    <Tile icon={icon} title={title}>
      {empty ? <p className="mt-1 text-sm text-muted-foreground">{empty}</p> : (
        <>
          <div className="mt-1 flex items-baseline justify-between gap-2 tabular-nums">
            <span className="text-lg font-semibold">{known ? `${formatBytes(used)} / ${formatBytes(total)}` : DASH}</span>
            <span className="text-sm text-muted-foreground">{known ? formatPercent(p, 0) : ''}</span>
          </div>
          <div className="mt-2"><Progress value={p} label={t.ariaUsage(title, p)} /></div>
        </>
      )}
    </Tile>
  );
}

export function ResourceTiles({ o, history }: { o: Overview; history: number[] }) {
  const m = o.memory;
  const ramUsed = m.availableBytes == null ? null : m.totalBytes - m.availableBytes;
  const swapUsed = m.swapTotalBytes == null || m.swapFreeBytes == null ? null : m.swapTotalBytes - m.swapFreeBytes;
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <Tile icon={<Cpu />} title={t.cpu}>
        <div className="flex items-end justify-between gap-3">
          <span className="text-3xl font-bold tabular-nums">{formatPercent(o.cpu.usagePercent)}</span>
          <div className="w-1/2 max-w-[10rem]"><Sparkline values={history} label={t.cpuHistory} min={0} max={100} /></div>
        </div>
      </Tile>
      <UsageTile icon={<MemoryStick />} title={t.ram} used={ramUsed} total={m.totalBytes} />
      <UsageTile icon={<Database />} title={t.swap} used={swapUsed} total={m.swapTotalBytes}
        empty={m.swapTotalBytes === 0 ? t.noSwap : undefined} />
    </div>
  );
}

/* ---------- storage (disks, volumes, SMART) ---------- */

type Fs = Overview['storage']['filesystems'][number];
type SmartDisk = Smart['disks'][number];

const onDisk = (f: Fs, disk: string) => new RegExp(`^/dev/${disk}(p?\\d+)?$`).test(f.device);

function diskRole(d: Overview['storage']['disks'][number], fss: Fs[]): string {
  if (d.removable) return t.removableDisk;
  const parts = [];
  if (fss.some((f) => f.mount === '/')) parts.push(t.systemDisk);
  if (fss.some((f) => f.mount.startsWith('/srv'))) parts.push(t.dataDisk.toLowerCase());
  return parts.length ? parts.join(' / ').replace(/^./, (c) => c.toUpperCase()) : (d.rotational ? t.hdd : t.ssd);
}

function FsRow({ f }: { f: Fs }) {
  const p = ratioPercent(f.usedBytes, f.totalBytes);
  return (
    <div className="py-1">
      <div className="mb-1 flex flex-wrap items-center justify-between gap-x-3 text-sm">
        <span className="font-medium">{f.mount} <span className="font-normal text-muted-foreground">{f.fsType}</span></span>
        <span className="flex items-center gap-2 tabular-nums text-muted-foreground">
          {p > 95 ? <Badge variant="danger">{t.critical}</Badge> : p > 85 ? <Badge variant="warning">{t.warnHigh}</Badge> : null}
          {formatBytes(f.usedBytes)} / {formatBytes(f.totalBytes)} ({formatPercent(p, 0)})
        </span>
      </div>
      <Progress value={p} label={t.ariaUsage(f.mount, p)} />
    </div>
  );
}

function SmartLine({ s }: { s: SmartDisk | undefined }) {
  if (!s) return null;
  if (s.standby) return <Badge>{t.standby}</Badge>;
  const bad = (s.reallocatedSectors ?? 0) > 0 || (s.pendingSectors ?? 0) > 0;
  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
      <Badge variant={s.health === 'passed' ? 'success' : s.health === 'failed' ? 'danger' : 'neutral'}>
        {t.smartShort}: {t.health[s.health]}
      </Badge>
      {s.temperatureCelsius != null && <span>{formatNumber(s.temperatureCelsius)} °C</span>}
      {s.powerOnHours != null && <span>{formatNumber(s.powerOnHours)} {t.hours}</span>}
      {bad && (
        <span className="font-medium text-danger">
          {t.reallocated} {formatNumber(s.reallocatedSectors)} · {t.pending} {formatNumber(s.pendingSectors)}
        </span>
      )}
    </span>
  );
}

export function StorageCard({ o, smart, smartError, className }: { o: Overview; smart: Smart | null; smartError: boolean; className?: string }) {
  const { disks, filesystems } = o.storage;
  const other = filesystems.filter((f) => !disks.some((d) => onDisk(f, d.name)));
  return (
    <Widget id="c-sto" title={t.storage} icon={<HardDrive />} className={className}
      right={<a href="#/storage" className="font-medium text-primary hover:underline">{t.manage}</a>}
      bodyClassName="divide-y divide-border/60 py-1">
      {disks.length === 0 && <p className="py-3 text-sm text-muted-foreground">{t.noDisks}</p>}
      {disks.map((d) => {
        const fss = filesystems.filter((f) => onDisk(f, d.name));
        const Icon = d.removable ? Usb : HardDrive;
        return (
          <div key={d.name} className="flex gap-4 py-3">
            <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground" aria-hidden>
              <Icon className="h-6 w-6" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline justify-between gap-x-3">
                <h3 className="font-semibold">{d.name} – {d.model || formatBytes(d.sizeBytes)}</h3>
                <span className="text-sm text-muted-foreground">{formatBytes(d.sizeBytes)} · {d.rotational ? t.hdd : t.ssd}</span>
              </div>
              <div className="mb-1 flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
                <span>{diskRole(d, fss)}</span>
                <SmartLine s={smart?.disks.find((s) => s.name === d.name)} />
              </div>
              {fss.length === 0 ? <p className="text-sm text-muted-foreground">{t.noVolumes}</p> : fss.map((f) => <FsRow key={f.mount} f={f} />)}
            </div>
          </div>
        );
      })}
      {other.length > 0 && (
        <div className="py-3">
          <h3 className="mb-1 text-sm font-semibold">{t.otherVolumes}</h3>
          {other.map((f) => <FsRow key={f.mount} f={f} />)}
        </div>
      )}
      {smartError && !smart && <p className="py-2 text-sm text-danger">{t.smartError}</p>}
      {smart?.error && (
        <p className="py-2 text-sm text-warning">{smart.error === 'smartctl_unavailable' ? t.smartUnavailable : t.smartError}</p>
      )}
    </Widget>
  );
}

/* ---------- services ---------- */

const dot: Record<string, string> = {
  active: 'bg-success', done: 'bg-success/60', inactive: 'bg-muted-foreground/50', failed: 'bg-danger', unknown: 'bg-warning',
};

export function ServicesCard({ o, className }: { o: Overview; className?: string }) {
  return (
    <Widget id="c-svc" title={t.services} icon={<Activity />} className={className} bodyClassName="py-2">
      {o.services.length === 0 && <p className="text-sm text-muted-foreground">{t.noServices}</p>}
      <ul className="divide-y divide-border/60">
        {o.services.map((s) => (
          <li key={s.name} className="flex items-center justify-between gap-3 py-2 text-sm">
            <span className="font-medium">{s.label}</span>
            <span className={cn('flex items-center gap-2', s.state === 'failed' && 'font-semibold text-danger')}>
              <span className={cn('h-2.5 w-2.5 rounded-full', dot[s.state] ?? dot.unknown)} aria-hidden />
              {t.serviceState[s.state] ?? t.serviceState.unknown}
            </span>
          </li>
        ))}
      </ul>
    </Widget>
  );
}

/* ---------- network ---------- */

const ipv4 = (n: Overview['network'][number]) => n.addresses.find((a) => !a.includes(':'))?.split('/')[0];

/** Interface shown in the big chart: first link that is up and has an IPv4 address. */
export function primaryInterface(o: Overview) {
  return o.network.find((n) => n.up && ipv4(n)) ?? o.network.find((n) => n.up) ?? o.network[0];
}

export function NetworkCard({ o, rates, history, className }: {
  o: Overview; rates: Record<string, NetRate>; history: Record<string, NetHistory>; className?: string;
}) {
  const n = primaryInterface(o);
  const r = n ? rates[n.name] : undefined;
  const h = n ? history[n.name] : undefined;
  const others = o.network.filter((x) => x !== n);
  return (
    <Widget id="c-net" title={t.network} icon={<Network />} className={className}
      right={n?.up ? (
        <span className="flex gap-4 font-semibold tabular-nums">
          <span className="flex items-center gap-1" title={t.down}><ArrowDown className="h-4 w-4 text-primary" aria-label={t.down} />{formatRate(r?.rx)}</span>
          <span className="flex items-center gap-1" title={t.up}><ArrowUp className="h-4 w-4 text-accent" aria-label={t.up} />{formatRate(r?.tx)}</span>
        </span>
      ) : undefined}>
      {!n ? <p className="text-sm text-muted-foreground">{t.noInterfaces}</p> : (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <span className="font-semibold">{n.name}</span>
            <span className="text-muted-foreground">{ipv4(n) ?? t.noAddresses}</span>
            <Badge variant={n.up ? 'success' : 'neutral'}>{n.up ? t.linkUp : t.linkDown}</Badge>
            {n.up && <span className="text-muted-foreground">{formatSpeed(n.speedMbps)}</span>}
          </div>
          <AreaChart label={`${t.netHistory} ${n.name}`}
            series={[{ values: h?.rx ?? [], tone: 'primary' }, { values: h?.tx ?? [], tone: 'accent' }]} />
          <dl className="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
            <div className="flex gap-1"><dt>{t.received}:</dt><dd className="font-medium text-foreground">{formatBytes(n.rxBytes)}</dd></div>
            <div className="flex gap-1"><dt>{t.sent}:</dt><dd className="font-medium text-foreground">{formatBytes(n.txBytes)}</dd></div>
            {others.map((x) => (
              <div key={x.name} className="flex gap-1"><dt>{x.name}:</dt><dd>{x.up ? t.linkUp : t.linkDown}</dd></div>
            ))}
          </dl>
        </>
      )}
    </Widget>
  );
}

/* ---------- temperatures and fan ---------- */

export function SensorsCard({ o, history, className }: { o: Overview; history: number[]; className?: string }) {
  const s = o.sensors;
  return (
    <Widget id="c-sen" title={t.sensors} icon={<Thermometer />} className={className}>
      {s.status !== 'ok' ? (
        <Badge variant={s.status === 'fault' ? 'danger' : 'warning'} role="status">
          {t.sensorStatus[s.status] ?? t.sensorStatus.unknown}
        </Badge>
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3">
            {s.temperatures.length === 0 && <p className="text-muted-foreground">{t.noSensors}</p>}
            {s.temperatures.map((x) => (
              <div key={x.id}>
                <dt className="text-xs text-muted-foreground">{x.label}</dt>
                <dd className="text-lg font-semibold tabular-nums">{formatTemp(x.milliCelsius)}</dd>
              </div>
            ))}
          </dl>
          {s.fans.map((x) => (
            <div key={x.id} className="mt-3 flex items-center justify-between gap-2 border-t border-border/60 pt-3 text-sm">
              <span className="text-muted-foreground">{x.label}</span>
              <span className="flex items-center gap-2 font-semibold tabular-nums">
                {x.rpm === 0 && <Badge variant="warning">{t.fanStopped}</Badge>}
                {x.rpm == null ? DASH : `${formatNumber(x.rpm)} ${t.rpm}`}
              </span>
            </div>
          ))}
          <div className="mt-3"><Sparkline values={history} label={t.tempHistory} /></div>
        </>
      )}
    </Widget>
  );
}

/* ---------- recent events ---------- */

const evDot: Record<string, string> = { boot: 'bg-accent', ...dot };
const evTime = new Intl.DateTimeFormat('cs-CZ', { day: 'numeric', month: 'numeric', hour: '2-digit', minute: '2-digit' });

export function EventsCard({ o, className }: { o: Overview; className?: string }) {
  const ev = deriveEvents(o);
  return (
    <Widget id="c-ev" title={t.events} icon={<FileText />} className={className} bodyClassName="py-2">
      {ev.length === 0 && <p className="py-2 text-sm text-muted-foreground">{t.noEvents}</p>}
      <ul className="divide-y divide-border/60">
        {ev.map((e) => (
          <li key={`${e.kind}-${e.label}-${e.at}`} className="flex items-center gap-3 py-2 text-sm">
            <span className={cn('h-2.5 w-2.5 shrink-0 rounded-full', evDot[e.kind] ?? dot.unknown)} aria-hidden />
            <time dateTime={new Date(e.at).toISOString()} className="w-28 shrink-0 tabular-nums text-muted-foreground">{evTime.format(e.at)}</time>
            <span className="min-w-0">{(t.event[e.kind] ?? t.event.unknown!)(e.label)}</span>
          </li>
        ))}
      </ul>
    </Widget>
  );
}

/* ---------- system information ---------- */

const Row = ({ k, children }: { k: string; children: ReactNode }) => (
  <div className="flex gap-4 border-b border-border/50 py-1.5 text-sm last:border-0">
    <dt className="w-24 shrink-0 text-muted-foreground">{k}</dt>
    <dd className="min-w-0 break-words font-medium">{children}</dd>
  </div>
);

export function SystemCard({ o, className }: { o: Overview; className?: string }) {
  const n = primaryInterface(o);
  return (
    <Widget id="c-sys" title={t.sysInfo} icon={<Info />} className={className} bodyClassName="py-2">
      <div className="grid gap-x-8 sm:grid-cols-2">
        <dl>
          <Row k={t.board}>{o.host.boardModel}</Row>
          <Row k={t.cpu}>{o.cpu.model} · {o.cpu.cores} {o.cpu.cores === 1 ? 'jádro' : 'jádra'}</Row>
          <Row k={t.os}>{o.host.os}</Row>
          <Row k={t.load}>{[o.cpu.load1, o.cpu.load5, o.cpu.load15].map((v) => formatNumber(v, 2)).join(' / ')}</Row>
        </dl>
        <dl>
          <Row k={t.uiVersion}>{cs.product} v{__APP_VERSION__}</Row>
          <Row k={t.kernel}>{o.host.kernel}</Row>
          <Row k={t.hostname}>{o.host.hostname}</Row>
          <Row k={t.ip}>{(n && ipv4(n)) ?? DASH}</Row>
        </dl>
      </div>
    </Widget>
  );
}
