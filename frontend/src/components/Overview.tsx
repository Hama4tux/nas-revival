import { AlertTriangle, Cpu, HardDrive, MemoryStick, Network, Server, ShieldCheck, Thermometer, Activity } from 'lucide-react';
import { useEffect } from 'react';
import { cs } from '../i18n/cs';
import { useOverview } from '../hooks/useOverview';
import { Button } from './ui/button';
import {
  CardSkeleton, CpuCard, MemoryCard, NetworkCard, SensorsCard, ServicesCard, SmartCard, StorageCard, SystemCard,
} from './cards/Cards';

const t = cs.overview;
const i = 'h-4 w-4';

export function Overview({ onHostname }: { onHostname: (h: string) => void }) {
  const { data, error, smart, smartError, cpuHistory, tempHistory, stale, reload } = useOverview();
  const hostname = data?.host.hostname;
  useEffect(() => { if (hostname) onHostname(hostname); }, [hostname, onHostname]);

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{t.title}</h1>
        <div role="status" aria-live="polite" className="contents">
          {stale && (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-warning/40 bg-warning/10 px-3 py-1 text-sm font-medium text-warning" title={t.staleDetail}>
              <AlertTriangle className={i} aria-hidden />{t.stale}
            </span>
          )}
        </div>
      </div>

      {error && (
        <div role="alert" className="mb-4 flex flex-wrap items-center gap-3 rounded-lg border border-danger/40 bg-danger/10 p-3 text-sm text-danger">
          <AlertTriangle className={i} aria-hidden />{t.error}
          <Button size="sm" variant="outline" onClick={() => void reload()}>{cs.common.retry}</Button>
        </div>
      )}

      {data ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <SystemCard o={data} />
          <CpuCard o={data} history={cpuHistory} />
          <MemoryCard o={data} />
          <SensorsCard o={data} history={tempHistory} />
          <NetworkCard o={data} />
          <StorageCard o={data} />
          <SmartCard smart={smart} error={smartError} />
          <ServicesCard o={data} />
        </div>
      ) : !error ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" aria-busy="true" aria-label={cs.common.loading}>
          <CardSkeleton title={t.system} icon={<Server className={i} />} />
          <CardSkeleton title={t.cpu} icon={<Cpu className={i} />} />
          <CardSkeleton title={t.memory} icon={<MemoryStick className={i} />} />
          <CardSkeleton title={t.sensors} icon={<Thermometer className={i} />} />
          <CardSkeleton title={t.network} icon={<Network className={i} />} />
          <CardSkeleton title={t.storage} icon={<HardDrive className={i} />} />
          <CardSkeleton title={t.smart} icon={<ShieldCheck className={i} />} />
          <CardSkeleton title={t.services} icon={<Activity className={i} />} />
        </div>
      ) : null}
    </div>
  );
}
