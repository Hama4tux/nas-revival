import { Activity, FolderOpen, HardDrive, LayoutDashboard, Network, ScrollText, Settings, Users, X, type LucideIcon } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { cs } from '../i18n/cs';
import { cn } from '../lib/utils';

const apps: { key: keyof typeof cs.nav; icon: LucideIcon; ready: boolean; tone: string }[] = [
  { key: 'overview', icon: LayoutDashboard, ready: true, tone: 'from-sky-400 to-blue-600' },
  { key: 'storage', icon: HardDrive, ready: false, tone: 'from-emerald-400 to-teal-600' },
  { key: 'shares', icon: FolderOpen, ready: false, tone: 'from-amber-400 to-orange-600' },
  { key: 'users', icon: Users, ready: false, tone: 'from-violet-400 to-purple-600' },
  { key: 'services', icon: Activity, ready: false, tone: 'from-rose-400 to-pink-600' },
  { key: 'network', icon: Network, ready: false, tone: 'from-cyan-400 to-sky-600' },
  { key: 'logs', icon: ScrollText, ready: false, tone: 'from-slate-400 to-slate-600' },
  { key: 'system', icon: Settings, ready: false, tone: 'from-indigo-400 to-blue-700' },
];

const FOCUSABLE = 'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

export function Launcher({ onClose }: { onClose: () => void }) {
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
    <div className="fixed inset-0 top-11 z-40" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
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
          {apps.map(({ key, icon: Icon, ready, tone }) => (
            <li key={key}>
              <button
                type="button" disabled={!ready} aria-current={ready ? 'page' : undefined} onClick={ready ? onClose : undefined}
                className={cn('relative flex w-full flex-col items-center gap-1.5 rounded-xl p-2 text-center text-xs font-medium',
                  ready ? 'hover:bg-muted' : 'cursor-not-allowed')}
              >
                <span className={cn('flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br text-white shadow-md', tone, !ready && 'opacity-45 saturate-50')}>
                  <Icon className="h-7 w-7" aria-hidden />
                </span>
                <span className={ready ? '' : 'text-muted-foreground'}>{cs.nav[key]}</span>
                {!ready && <span className="rounded-full bg-muted px-1.5 py-px text-[10px] leading-tight text-muted-foreground">{cs.shell.soon}</span>}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
