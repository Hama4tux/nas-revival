import { cn } from '../../lib/utils';

export function Progress({ value, label, warn = 85, crit = 95 }: { value: number; label: string; warn?: number; crit?: number }) {
  const v = Math.min(100, Math.max(0, value));
  const color = v > crit ? 'bg-danger' : v > warn ? 'bg-warning' : 'bg-primary';
  return (
    <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(v)}
      className="h-2.5 w-full overflow-hidden rounded-full bg-muted">
      <div className={cn('h-full rounded-full transition-all', color)} style={{ width: `${v}%` }} />
    </div>
  );
}
