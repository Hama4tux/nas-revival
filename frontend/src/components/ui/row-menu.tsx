import { MoreHorizontal } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { cn } from '../../lib/utils';

export interface RowMenuItem {
  key: string; label: string; onSelect: () => void; icon?: ReactNode; danger?: boolean; disabled?: boolean;
}

/** "…" menu at the end of a compact list row: arrow keys move, Escape closes and returns focus to the button. */
export function RowMenu({ label, items, disabled }: { label: string; items: RowMenuItem[]; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);

  const entries = () => [...(list.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])];

  useEffect(() => {
    if (!open) return;
    entries()[0]?.focus();
    const down = (e: MouseEvent) => { if (!wrap.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', down);
    return () => document.removeEventListener('mousedown', down);
  }, [open]);

  function onKey(e: React.KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') { e.stopPropagation(); setOpen(false); btn.current?.focus(); return; }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp' && e.key !== 'Home' && e.key !== 'End') return;
    e.preventDefault();
    const all = entries();
    const i = all.indexOf(document.activeElement as HTMLButtonElement);
    const n = e.key === 'Home' ? 0 : e.key === 'End' ? all.length - 1
      : (i + (e.key === 'ArrowDown' ? 1 : all.length - 1)) % all.length;
    all[n]?.focus();
  }

  return (
    <div ref={wrap} className="relative" onKeyDown={onKey}>
      <button ref={btn} type="button" disabled={disabled || items.length === 0}
        className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40 aria-expanded:bg-muted"
        aria-haspopup="menu" aria-expanded={open} aria-label={label} title={label} onClick={() => setOpen((v) => !v)}>
        <MoreHorizontal className="h-5 w-5" aria-hidden />
      </button>
      {open && (
        <div ref={list} role="menu" aria-label={label} className="pop glass absolute right-0 top-9 z-40 min-w-48 rounded-lg p-1 text-foreground">
          {items.map((it) => (
            <button key={it.key} role="menuitem" type="button" disabled={it.disabled}
              className={cn('flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-muted disabled:opacity-50', it.danger && 'text-danger')}
              onClick={() => { setOpen(false); btn.current?.focus(); it.onSelect(); }}>
              {it.icon}{it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
