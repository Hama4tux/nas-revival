import * as React from 'react';
import { cn } from '../../lib/utils';

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...p }, ref) => (
    <input ref={ref} className={cn('h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm placeholder:text-muted-foreground', className)} {...p} />
  ),
);
Input.displayName = 'Input';
