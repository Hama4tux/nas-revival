import { Activity, AlertTriangle, FileText, HardDrive, Info, Network, Thermometer } from 'lucide-react';
import { useEffect } from 'react';
import { cs } from '../i18n/cs';
import { useOverview } from '../hooks/useOverview';
import { HealthPanel } from './HealthPanel';
import type { HostSummary } from './Shell';
import { Button } from './ui/button';
import { Skeleton } from './ui/skeleton';
import {
  CardSkeleton, EventsCard, NetworkCard, ResourceTiles, SensorsCard, ServicesCard, StorageCard, SystemCard,
} from './cards/Cards';

const t = cs.overview;
const i = 'h-5 w-5';
const row = 'grid gap-4 lg:grid-cols-5';

export function Overview({ onHost }: { onHost: (h: HostSummary) => void }) {
  const { data, error, smart, smartError, cpuHistory, tempHistory, netRates, netHistory, stale, reload } = useOverview();
  const hostname = data?.host.hostname;
  const board = data?.host.boardModel;
  const os = data?.host.os;
  useEffect(() => {
    if (hostname && board != null && os != null) onHost({ hostname, boardModel: board, os });
  }, [hostname, board, os, onHost]);

  return (
    <div className="space-y-4">
      <div role="status" aria-live="polite" className="empty:hidden">
        {stale && (
          <span className="inline-flex items-center gap-1.5 rounded-full border border-warning/40 bg-card px-3 py-1 text-sm font-medium text-warning" title={t.staleDetail}>
            <AlertTriangle className="h-4 w-4" aria-hidden />{t.stale}
          </span>
        )}
      </div>

      {error && (
        <div role="alert" className="glass flex flex-wrap items-center gap-3 rounded-lg border-danger/40 p-3 text-sm text-danger">
          <AlertTriangle className="h-4 w-4" aria-hidden />{t.error}
          <Button size="sm" variant="outline" onClick={() => void reload()}>{cs.common.retry}</Button>
        </div>
      )}

      {data ? (
        <>
          <HealthPanel o={data} smart={smart} stale={stale} />
          <ResourceTiles o={data} history={cpuHistory} />
          <div className={row}>
            <StorageCard o={data} smart={smart} smartError={smartError} className="lg:col-span-3" />
            <ServicesCard o={data} className="lg:col-span-2" />
          </div>
          <div className={row}>
            <NetworkCard o={data} rates={netRates} history={netHistory} className="lg:col-span-3" />
            <SensorsCard o={data} history={tempHistory} className="lg:col-span-2" />
          </div>
          <div className={row}>
            <EventsCard o={data} className="lg:col-span-2" />
            <SystemCard o={data} className="lg:col-span-3" />
          </div>
        </>
      ) : !error ? (
        <div className="space-y-4" aria-busy="true" aria-label={cs.common.loading}>
          <Skeleton className="h-24 w-full rounded-xl" />
          <div className="grid gap-4 sm:grid-cols-3">
            <Skeleton className="h-24 rounded-xl" /><Skeleton className="h-24 rounded-xl" /><Skeleton className="h-24 rounded-xl" />
          </div>
          <div className={row}>
            <CardSkeleton id="s1" title={t.storage} icon={<HardDrive className={i} />} className="lg:col-span-3" />
            <CardSkeleton id="s2" title={t.services} icon={<Activity className={i} />} className="lg:col-span-2" />
          </div>
          <div className={row}>
            <CardSkeleton id="s3" title={t.network} icon={<Network className={i} />} className="lg:col-span-3" />
            <CardSkeleton id="s4" title={t.sensors} icon={<Thermometer className={i} />} className="lg:col-span-2" />
          </div>
          <div className={row}>
            <CardSkeleton id="s5" title={t.events} icon={<FileText className={i} />} className="lg:col-span-2" />
            <CardSkeleton id="s6" title={t.sysInfo} icon={<Info className={i} />} className="lg:col-span-3" />
          </div>
        </div>
      ) : null}
    </div>
  );
}
