import { X } from 'lucide-react';
import { useEffect, useId, useRef, type ReactNode } from 'react';
import { cs } from '../../i18n/cs';

const FOCUSABLE = 'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Modal dialog: role=dialog, aria-modal, Esc closes, Tab trapped, focus returns to the opener. */
export function Dialog({ title, onClose, children, footer, busy }: {
  title: string; onClose: () => void; children: ReactNode; footer: ReactNode; busy?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const bodyId = useId();

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>('[data-autofocus]')?.focus()
      ?? ref.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    return () => { opener?.focus?.(); };
  }, []);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { e.stopPropagation(); if (!busy) onClose(); return; }
    if (e.key !== 'Tab') return;
    const els = Array.from(ref.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []);
    const first = els[0], last = els[els.length - 1];
    if (!first || !last) return;
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  return (
    <div className="fixed inset-0 z-[70] flex items-end justify-center bg-black/60 p-0 sm:items-center sm:p-4"
      onMouseDown={(e) => { if (e.target === e.currentTarget && !busy) onClose(); }}>
      <div ref={ref} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={bodyId} onKeyDown={onKeyDown}
        className="pop glass flex max-h-[92vh] w-full flex-col rounded-t-2xl bg-card sm:max-w-lg sm:rounded-2xl">
        <div className="flex items-center gap-3 border-b border-border/70 px-4 py-3">
          <h2 id={titleId} className="font-semibold">{title}</h2>
          <button type="button" onClick={onClose} disabled={busy} aria-label={cs.common.close}
            className="ml-auto flex h-8 w-8 items-center justify-center rounded-md hover:bg-muted disabled:opacity-50">
            <X className="h-5 w-5" aria-hidden />
          </button>
        </div>
        <div id={bodyId} className="space-y-3 overflow-y-auto p-4 text-sm">{children}</div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-border/70 px-4 py-3">{footer}</div>
      </div>
    </div>
  );
}
