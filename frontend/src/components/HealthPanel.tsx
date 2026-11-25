import { AlertTriangle, Check, Clock, Thermometer, X } from 'lucide-react';
import type { Overview, Smart } from '../api/types';
import { cs } from '../i18n/cs';
import { formatTemp, formatUptime } from '../lib/format';
import { deriveHealth, type HealthLevel } from '../lib/health';
import { cn } from '../lib/utils';
import { NasArt } from './NasArt';

const t = cs.overview;
const look: Record<HealthLevel, { icon: typeof Check; tile: string; label: string; band: string }> = {
  ok: { icon: Check, tile: 'bg-success text-white dark:text-slate-900', label: t.heroOk, band: 'from-success/15' },
  warning: { icon: AlertTriangle, tile: 'bg-warning text-white dark:text-slate-900', label: t.heroWarning, band: 'from-warning/15' },
  problem: { icon: X, tile: 'bg-danger text-white dark:text-slate-900', label: t.heroProblem, band: 'from-danger/15' },
};

/** Highest temperature reported by the board sensors, in milli-°C. */
function hottest(o: Overview): number | null {
  if (o.sensors.status !== 'ok') return null;
  const v = o.sensors.temperatures.map((x) => x.milliCelsius).filter((x): x is number => x != null);
  return v.length ? Math.max(...v) : null;
}

function Stat({ icon: Icon, k, v }: { icon: typeof Clock; k: string; v: string }) {
  return (
    <div className="flex items-center gap-3">
      <Icon className="h-6 w-6 shrink-0 text-muted-foreground" aria-hidden />
      <div>
        <div className="text-xs text-muted-foreground">{k}</div>
        <div className="font-semibold tabular-nums">{v}</div>
      </div>
    </div>
  );
}

export function HealthPanel({ o, smart, stale }: { o: Overview; smart: Smart | null; stale: boolean }) {
  const h = deriveHealth(o, smart, stale);
  const l = look[h.level];
  const Icon = l.icon;
  return (
    <section aria-labelledby="c-health"
      className={cn('glass relative overflow-hidden rounded-xl bg-gradient-to-r to-transparent', l.band)}>
      <div className="relative z-10 flex flex-col gap-5 p-5 md:flex-row md:items-center md:pr-48">
        <div className="flex min-w-0 items-center gap-4">
          <span className={cn('flex h-14 w-14 shrink-0 items-center justify-center rounded-xl shadow-lg', l.tile)} aria-hidden>
            <Icon className="h-8 w-8" strokeWidth={3} />
          </span>
          <div className="min-w-0">
            <h2 id="c-health" className="sr-only">{t.healthTitle}</h2>
            <p role="status" className="text-xl font-bold sm:text-2xl">{l.label}</p>
            {h.reasons.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t.heroOkDetail}</p>
            ) : (
              <ul className="mt-0.5 space-y-0.5 text-sm">
                {h.reasons.map((r) => (
                  <li key={`${r.code}-${r.arg ?? ''}`} className="flex items-center gap-1.5">
                    <span className={cn('h-2 w-2 shrink-0 rounded-full', r.level === 'problem' ? 'bg-danger' : 'bg-warning')} aria-hidden />
                    {t.reasons[r.code]?.(r.arg)}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
        <div className="flex gap-8 border-border/70 md:ml-auto md:border-l md:pl-8">
          <Stat icon={Clock} k={t.uptime} v={formatUptime(o.host.uptimeSeconds)} />
          <Stat icon={Thermometer} k={t.hottest} v={formatTemp(hottest(o))} />
        </div>
      </div>
      <NasArt className="pointer-events-none absolute -bottom-3 right-6 hidden h-36 w-32 md:block" />
    </section>
  );
}
