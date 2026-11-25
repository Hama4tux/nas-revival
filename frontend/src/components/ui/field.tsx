import { useId, type ReactNode } from 'react';
import { cn } from '../../lib/utils';

/** Label + control + hint/error wiring (aria-invalid, aria-describedby). `children` gets the props to spread. */
export function Field({ label, hint, error, className, children }: {
  label: string; hint?: ReactNode; error?: string | null; className?: string;
  children: (p: { id: string; 'aria-invalid': boolean; 'aria-describedby': string | undefined }) => ReactNode;
}) {
  const id = useId();
  const hid = `${id}-h`, eid = `${id}-e`;
  const desc = [hint ? hid : '', error ? eid : ''].filter(Boolean).join(' ') || undefined;
  return (
    <div className={cn('space-y-1', className)}>
      <label htmlFor={id} className="text-sm font-medium">{label}</label>
      {children({ id, 'aria-invalid': !!error, 'aria-describedby': desc })}
      {hint && <p id={hid} className="text-xs text-muted-foreground">{hint}</p>}
      {error && <p id={eid} className="text-xs font-medium text-danger">{error}</p>}
    </div>
  );
}

export const invalidCls = 'aria-[invalid=true]:border-danger';
