import * as React from 'react';
import type { ReactNode } from 'react';
import { cn } from '../../lib/utils';

/** Dashboard panel with a title bar (accent icon + title, optional action on the right). */
export function Widget({ id, title, icon, right, className, bodyClassName, children, ...p }: {
  id: string; title: string; icon: ReactNode; right?: ReactNode; className?: string; bodyClassName?: string; children: ReactNode;
} & Omit<React.HTMLAttributes<HTMLElement>, 'title'>) {
  return (
    <section aria-labelledby={id} className={cn('glass flex flex-col overflow-hidden rounded-xl', className)} {...p}>
      <header className="flex items-center gap-3 border-b border-border/70 px-4 py-3">
        <span className="text-primary [&>svg]:h-5 [&>svg]:w-5" aria-hidden>{icon}</span>
        <h2 id={id} className="font-semibold">{title}</h2>
        {right && <div className="ml-auto text-sm">{right}</div>}
      </header>
      <div className={cn('flex-1 p-4', bodyClassName)}>{children}</div>
    </section>
  );
}
