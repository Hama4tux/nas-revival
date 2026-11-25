import { AlertTriangle, CheckCircle2, XCircle } from 'lucide-react';
import type { Overview, Smart } from '../api/types';
import { cs } from '../i18n/cs';
import { deriveHealth, type HealthLevel } from '../lib/health';
import { cn } from '../lib/utils';

const t = cs.overview;
const look: Record<HealthLevel, { icon: typeof CheckCircle2; text: string; ring: string; label: string }> = {
  ok: { icon: CheckCircle2, text: 'text-success', ring: 'bg-success/15', label: t.healthOk },
  warning: { icon: AlertTriangle, text: 'text-warning', ring: 'bg-warning/15', label: t.healthWarning },
  problem: { icon: XCircle, text: 'text-danger', ring: 'bg-danger/15', label: t.healthProblem },
};

export function HealthPanel({ o, smart, stale }: { o: Overview; smart: Smart | null; stale: boolean }) {
  const h = deriveHealth(o, smart, stale);
  const l = look[h.level];
  const Icon = l.icon;
  return (
    <section aria-labelledby="c-health" className="glass col-span-full flex flex-col gap-4 rounded-xl p-5 sm:flex-row sm:items-center">
      <span className={cn('flex h-20 w-20 shrink-0 items-center justify-center rounded-full', l.ring, l.text)} aria-hidden>
        <Icon className="h-12 w-12" />
      </span>
      <div className="min-w-0">
        <h2 id="c-health" className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">{t.healthTitle}</h2>
        <p role="status" className={cn('text-3xl font-semibold', l.text)}>{l.label}</p>
        {h.reasons.length === 0 ? (
          <p className="mt-1 text-sm text-muted-foreground">{t.healthOkDetail}</p>
        ) : (
          <ul className="mt-1 space-y-0.5 text-sm">
            {h.reasons.map((r) => (
              <li key={`${r.code}-${r.arg ?? ''}`} className="flex items-center gap-1.5">
                <span className={cn('h-2 w-2 shrink-0 rounded-full', r.level === 'problem' ? 'bg-danger' : 'bg-warning')} aria-hidden />
                {t.reasons[r.code]?.(r.arg)}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
