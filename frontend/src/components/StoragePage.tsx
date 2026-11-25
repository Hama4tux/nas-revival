import { AlertTriangle, HardDrive, RefreshCw, Usb } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { StorageDisk, StoragePartition, StorageReport } from '../api/types';
import { cs } from '../i18n/cs';
import { formatBytes, ratioPercent } from '../lib/format';
import { diskTitle, diskWarnings, unpartitionedBytes } from '../lib/storage';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Progress } from './ui/progress';
import { Skeleton } from './ui/skeleton';

const t = cs.storage;
const dash = '–';

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex justify-between gap-3 py-1 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className={`text-right font-medium break-all ${mono ? 'font-mono text-xs leading-5' : ''}`}>{value}</dd>
    </div>
  );
}

function PartitionItem({ p }: { p: StoragePartition }) {
  const fs = p.swap ? t.swap : p.fsType ?? t.noFs;
  const pct = p.usage ? ratioPercent(p.usage.usedBytes, p.usage.totalBytes) : null;
  return (
    <li className="rounded-lg border border-border/70 p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <p className="font-medium">
          {p.label ?? p.partLabel ?? t.partition(p.number)}
          <span className="ml-2 font-mono text-xs text-muted-foreground">{p.name}</span>
        </p>
        <p className="text-sm tabular-nums">{formatBytes(p.sizeBytes)} · {fs}</p>
      </div>
      {p.mounts.length > 0 ? (
        <ul className="mt-2 space-y-1 text-sm" aria-label={t.mounts}>
          {p.mounts.map((m) => (
            <li key={m.point} className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs break-all">{m.point}</span>
              {m.bind && <Badge title={t.bindHint(m.root)}>{t.bind}</Badge>}
              {m.readOnly && <Badge variant="warning">{t.readOnly}</Badge>}
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-2 text-sm text-muted-foreground">{p.swap ? t.swapActive : t.notMounted}</p>
      )}
      {p.usage && pct != null && (
        <div className="mt-2 space-y-1">
          <Progress value={pct} label={cs.overview.ariaUsage(p.mounts[0]?.point ?? p.name, pct)} />
          <p className="text-xs text-muted-foreground tabular-nums">
            {t.usage(formatBytes(p.usage.usedBytes), formatBytes(p.usage.totalBytes), formatBytes(p.usage.availableBytes))}
          </p>
        </div>
      )}
    </li>
  );
}

function DiskCard({ d }: { d: StorageDisk }) {
  const warnings = diskWarnings(d);
  const free = unpartitionedBytes(d);
  const Icon = d.bus === 'usb' ? Usb : HardDrive;
  return (
    <Widget id={`disk-${d.name}`} title={diskTitle(d)} icon={<Icon />}
      right={<div className="flex flex-wrap justify-end gap-1">
        {d.system && <Badge variant="success">{t.systemDisk}</Badge>}
        {d.roles.filter((r) => r !== 'system').map((r) => <Badge key={r}>{t.roles[r]}</Badge>)}
      </div>}>
      {warnings.length > 0 && (
        <ul className="mb-4 space-y-2">
          {warnings.map((w) => (
            <li key={w} role="note" className="flex gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
              <span>{t.warn[w]}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="grid gap-6 md:grid-cols-2">
        <dl className="divide-y divide-border">
          <Row label={t.type} value={`${t.types[d.type]} · ${t.buses[d.bus]}`} />
          <Row label={t.capacity} value={formatBytes(d.sizeBytes)} />
          {d.rotationRpm != null && <Row label={t.rpm} value={t.rpmValue(d.rotationRpm)} />}
          <Row label={t.serial} value={d.serial ?? dash} mono />
          <Row label={t.firmware} value={d.firmware ?? dash} />
          <Row label={t.table} value={d.partitionTable ? d.partitionTable.toUpperCase() : t.noTable} />
          <Row label={t.kernelName} value={d.name} mono />
          <Row label={t.id} value={d.id} mono />
        </dl>
        <div>
          <h3 className="mb-2 text-sm text-muted-foreground">{t.partitions}</h3>
          {d.partitions.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t.noPartitions}</p>
          ) : (
            <ul className="space-y-2">{d.partitions.map((p) => <PartitionItem key={p.name} p={p} />)}</ul>
          )}
          {free > 0 && <p className="mt-2 text-sm text-muted-foreground">{t.unpartitioned(formatBytes(free))}</p>}
        </div>
      </div>
    </Widget>
  );
}

export function StoragePage() {
  const [data, setData] = useState<StorageReport | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true); setFailed(false);
    try { setData(await api<StorageReport>('/storage')); } catch { setFailed(true); } finally { setBusy(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        <Button type="button" variant="outline" onClick={() => void load()} disabled={busy}>
          <RefreshCw className="h-4 w-4" aria-hidden />{t.refresh}
        </Button>
      </div>
      {failed && <p role="alert" className="text-sm text-danger">{t.loadError}</p>}
      {!data ? (
        !failed && <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-64 w-full" /></div>
      ) : data.disks.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t.noDisks}</p>
      ) : (
        data.disks.map((d) => <DiskCard key={d.id} d={d} />)
      )}
    </div>
  );
}
