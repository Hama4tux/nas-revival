import * as React from 'react';
import type { ReactNode } from 'react';
import { cn } from '../../lib/utils';

/** DSM-like widget: frosted panel with a small title bar (icon + title). */
export function Widget({ id, title, icon, tone = 'bg-sky-600', right, className, children, ...p }: {
  id: string; title: string; icon: ReactNode; tone?: string; right?: ReactNode; className?: string; children: ReactNode;
} & Omit<React.HTMLAttributes<HTMLElement>, 'title'>) {
  return (
    <section aria-labelledby={id} className={cn('glass overflow-hidden rounded-xl', className)} {...p}>
      <header className="flex items-center gap-2 border-b border-border/60 bg-card/60 px-3 py-2">
        <span className={cn('flex h-6 w-6 items-center justify-center rounded-md text-white [&>svg]:h-3.5 [&>svg]:w-3.5', tone)} aria-hidden>{icon}</span>
        <h2 id={id} className="text-sm font-semibold">{title}</h2>
        {right && <div className="ml-auto">{right}</div>}
      </header>
      <div className="p-4">{children}</div>
    </section>
  );
}
