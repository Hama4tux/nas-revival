import { X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { cs } from '../i18n/cs';
import { cn } from '../lib/utils';
import { appsFor } from './nav';
import type { Route } from '../hooks/useRoute';
import type { Role } from '../api/types';

const FOCUSABLE = 'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

export function Launcher({ onClose, route, role }: { onClose: () => void; route: Route; role: Role }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ref.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
  }, []);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { e.stopPropagation(); onClose(); return; }
    if (e.key !== 'Tab') return;
    const els = Array.from(ref.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []);
    const first = els[0], last = els[els.length - 1];
    if (!first || !last) return;
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  return (
    <div className="fixed inset-0 top-14 z-40 lg:hidden" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div
        ref={ref} role="dialog" aria-modal="true" aria-label={cs.shell.menuTitle} onKeyDown={onKeyDown}
        className="pop glass h-full overflow-y-auto bg-card/90 p-5 backdrop-blur-xl sm:m-2 sm:h-auto sm:max-h-[calc(100vh-4rem)] sm:w-[26rem] sm:rounded-2xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">{cs.shell.menuTitle}</h2>
          <button type="button" onClick={onClose} aria-label={cs.shell.closeMenu}
            className="flex h-8 w-8 items-center justify-center rounded-md hover:bg-muted sm:hidden">
            <X className="h-5 w-5" aria-hidden />
          </button>
        </div>
        <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4">
          {appsFor(role).map(({ key, icon: Icon, ready, tone, href }) => {
            const inner = (
              <>
                <span className={cn('flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br text-white shadow-md', tone, !ready && 'opacity-45 saturate-50')}>
                  <Icon className="h-7 w-7" aria-hidden />
                </span>
                <span className={ready ? '' : 'text-muted-foreground'}>{cs.nav[key]}</span>
                {!ready && <span className="rounded-full bg-muted px-1.5 py-px text-[10px] leading-tight text-muted-foreground">{cs.shell.soon}</span>}
              </>
            );
            const cls = cn('relative flex w-full flex-col items-center gap-1.5 rounded-xl p-2 text-center text-xs font-medium',
              ready ? 'hover:bg-muted' : 'cursor-not-allowed', ready && key === route && 'bg-primary/15 ring-1 ring-inset ring-primary/30');
            return (
              <li key={key}>
                {ready ? (
                  <a href={href} aria-current={key === route ? 'page' : undefined} className={cls}
                    onClick={() => { if (key === route) onClose(); }}>{inner}</a>
                ) : (
                  <button type="button" disabled className={cls}>{inner}</button>
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
