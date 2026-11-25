import { AlertTriangle, Cpu, HardDrive, Network, Server, ShieldCheck, Thermometer, Activity } from 'lucide-react';
import { useEffect } from 'react';
import { cs } from '../i18n/cs';
import { useOverview } from '../hooks/useOverview';
import { HealthPanel } from './HealthPanel';
import { Button } from './ui/button';
import {
  CardSkeleton, ResourcesCard, NetworkCard, SensorsCard, ServicesCard, SmartCard, StorageCard, SystemCard,
} from './cards/Cards';

const t = cs.overview;
const i = 'h-4 w-4';

export function Overview({ onHostname }: { onHostname: (h: string) => void }) {
  const { data, error, smart, smartError, cpuHistory, tempHistory, netRates, netHistory, stale, reload } = useOverview();
  const hostname = data?.host.hostname;
  useEffect(() => { if (hostname) onHostname(hostname); }, [hostname, onHostname]);

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold drop-shadow-sm">{t.title}</h1>
        <div role="status" aria-live="polite" className="contents">
          {stale && (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-warning/40 bg-card/80 px-3 py-1 text-sm font-medium text-warning" title={t.staleDetail}>
              <AlertTriangle className={i} aria-hidden />{t.stale}
            </span>
          )}
        </div>
      </div>

      {error && (
        <div role="alert" className="mb-4 flex flex-wrap items-center gap-3 glass rounded-lg border-danger/40 bg-card/90 p-3 text-sm text-danger">
          <AlertTriangle className={i} aria-hidden />{t.error}
          <Button size="sm" variant="outline" onClick={() => void reload()}>{cs.common.retry}</Button>
        </div>
      )}

      {data ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <HealthPanel o={data} smart={smart} stale={stale} />
          <ResourcesCard o={data} history={cpuHistory} />
          <StorageCard o={data} />
          <SmartCard smart={smart} error={smartError} />
          <SensorsCard o={data} history={tempHistory} />
          <NetworkCard o={data} rates={netRates} history={netHistory} />
          <ServicesCard o={data} />
          <SystemCard o={data} />
        </div>
      ) : !error ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" aria-busy="true" aria-label={cs.common.loading}>
          <CardSkeleton id="s1" title={t.resources} icon={<Cpu className={i} />} />
          <CardSkeleton id="s2" title={t.storage} icon={<HardDrive className={i} />} />
          <CardSkeleton id="s3" title={t.smart} icon={<ShieldCheck className={i} />} />
          <CardSkeleton id="s4" title={t.sensors} icon={<Thermometer className={i} />} />
          <CardSkeleton id="s5" title={t.network} icon={<Network className={i} />} />
          <CardSkeleton id="s6" title={t.services} icon={<Activity className={i} />} />
          <CardSkeleton id="s7" title={t.system} icon={<Server className={i} />} />
        </div>
      ) : null}
    </div>
  );
}
