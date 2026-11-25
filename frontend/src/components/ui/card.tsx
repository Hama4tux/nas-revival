import * as React from 'react';
import { cn } from '../../lib/utils';

export const Card = ({ className, ...p }: React.HTMLAttributes<HTMLElement>) => (
  <section className={cn('rounded-xl border border-border bg-card p-4 shadow-sm', className)} {...p} />
);
export const CardTitle = ({ className, ...p }: React.HTMLAttributes<HTMLHeadingElement>) => (
  <h2 className={cn('mb-3 flex items-center gap-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground', className)} {...p} />
);
