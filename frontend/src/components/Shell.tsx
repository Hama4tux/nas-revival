import { Box, ChevronDown, Eye, LayoutGrid, LogOut, Moon, Sun, User, UserRound } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { cs } from '../i18n/cs';
import { useTheme } from '../hooks/useTheme';
import { cn } from '../lib/utils';
import { Launcher } from './Launcher';
import { NasArt } from './NasArt';
import { PowerMenu } from './SystemPower';
import { appsFor } from './nav';
import type { Route } from '../hooks/useRoute';
import type { Role, SystemPowerAction } from '../api/types';
import { canWrite } from '../lib/roles';

export interface HostSummary { hostname: string; boardModel: string; os: string }

const iconBtn = 'flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-sm hover:bg-muted aria-expanded:bg-muted';

/** "Debian GNU/Linux 13 (trixie)" → "Debian" */
const osShort = (os: string) => os.split(/\s/)[0] ?? os;

function UserMenu({ username, role, onLogout, loggingOut }: { username: string | null; role: Role; onLogout: () => void; loggingOut: boolean }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const item = useRef<HTMLAnchorElement>(null);

  useEffect(() => {
    if (!open) return;
    item.current?.focus();
    const down = (e: MouseEvent) => { if (!wrap.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', down);
    return () => document.removeEventListener('mousedown', down);
  }, [open]);

  return (
    <div ref={wrap} className="relative"
      onKeyDown={(e) => { if (e.key === 'Escape' && open) { setOpen(false); btn.current?.focus(); } }}>
      <button ref={btn} type="button" className={iconBtn} aria-haspopup="menu" aria-expanded={open}
        aria-label={cs.shell.userMenu} onClick={() => setOpen((v) => !v)}>
        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-muted text-muted-foreground" aria-hidden>
          <User className="h-4 w-4" />
        </span>
        <span className="hidden font-medium sm:inline">{username ?? cs.shell.user}</span>
        <span className="sr-only">({cs.roles[role]})</span>
        <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      </button>
      {open && (
        <div role="menu" className="pop glass absolute right-0 top-11 z-50 w-48 rounded-lg p-1 text-foreground">
          <p className="px-3 pb-1 pt-2 text-xs text-muted-foreground">{cs.roles[role]}</p>
          <a ref={item} role="menuitem" href="#/account" onClick={() => setOpen(false)}
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted">
            <UserRound className="h-4 w-4" aria-hidden />{cs.shell.account}
          </a>
          <button role="menuitem" type="button" disabled={loggingOut} onClick={onLogout}
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted disabled:opacity-50">
            <LogOut className="h-4 w-4" aria-hidden />{loggingOut ? cs.shell.loggingOut : cs.shell.logout}
          </button>
        </div>
      )}
    </div>
  );
}

function Brand({ host }: { host: HostSummary | null }) {
  return (
    <div className="flex items-center gap-3">
      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl text-primary" aria-hidden>
        <Box className="h-8 w-8" strokeWidth={1.6} />
      </span>
      <div className="min-w-0">
        <div className="text-lg font-semibold leading-tight">{cs.product}</div>
        {host && <div className="truncate text-xs text-muted-foreground">{host.boardModel} · {osShort(host.os)}</div>}
      </div>
    </div>
  );
}

function Sidebar({ host, route, role }: { host: HostSummary | null; route: Route; role: Role }) {
  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col border-r border-border bg-sidebar/95 px-4 py-5 backdrop-blur lg:flex">
      <div className="px-1"><Brand host={host} /></div>
      <nav aria-label={cs.shell.mainNav} className="mt-7">
        <ul className="space-y-1">
          {appsFor(role).map(({ key, icon: Icon, ready, href }) => (
            <li key={key}>
              {ready ? (
                <a href={href} aria-current={key === route ? 'page' : undefined}
                  className={cn('flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm',
                    key === route ? 'bg-primary/15 font-semibold text-foreground ring-1 ring-inset ring-primary/30' : 'font-medium hover:bg-muted')}>
                  <Icon className="h-5 w-5 text-primary" aria-hidden />{cs.navLong[key]}
                </a>
              ) : (
                <span aria-disabled="true" title={cs.shell.soonTitle}
                  className="flex cursor-not-allowed items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground">
                  <Icon className="h-5 w-5 opacity-70" aria-hidden />
                  <span className="min-w-0 flex-1 truncate">{cs.navLong[key]}</span>
                  <span className="sr-only">({cs.shell.soon})</span>
                </span>
              )}
            </li>
          ))}
        </ul>
      </nav>
      {host && (
        <div className="mt-auto flex flex-col items-center pt-6 text-center">
          <NasArt className="h-28 w-24" />
          <div className="mt-2 font-semibold">{host.hostname}</div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="h-2 w-2 rounded-full bg-success" aria-hidden />{osShort(host.os)} · {cs.product}
          </div>
        </div>
      )}
    </aside>
  );
}

export function Shell({ host, username, role, onLogout, loggingOut, onPowerStarted, title, subtitle, route, banner, children }: {
  host: HostSummary | null; username: string | null; role: Role; onLogout: () => void; loggingOut: boolean;
  onPowerStarted: (a: SystemPowerAction) => void;
  title: string; subtitle: string; route: Route; banner?: ReactNode; children: ReactNode;
}) {
  const { theme, toggle } = useTheme();
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const close = () => { setMenu(false); menuBtn.current?.focus(); };
  const main = useRef<HTMLElement>(null);
  const first = useRef(true);

  // Move focus to the content after navigating (not on first load); close the launcher.
  useEffect(() => {
    setMenu(false);
    if (first.current) { first.current = false; return; }
    main.current?.focus();
    window.scrollTo(0, 0);
  }, [route]);

  const actions = (
    <>
      <button type="button" className={iconBtn} onClick={toggle} aria-label={theme === 'dark' ? cs.theme.toLight : cs.theme.toDark}>
        {theme === 'dark' ? <Sun className="h-5 w-5" aria-hidden /> : <Moon className="h-5 w-5" aria-hidden />}
      </button>
      {canWrite(role) && <PowerMenu onStarted={onPowerStarted} />}
      <UserMenu username={username} role={role} onLogout={onLogout} loggingOut={loggingOut} />
    </>
  );

  return (
    <div className="min-h-screen lg:pl-64">
      <a href="#main" onClick={(e) => { e.preventDefault(); main.current?.focus(); }} className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[60] focus:rounded focus:bg-card focus:p-2">
        {cs.common.skipToContent}
      </a>
      <Sidebar host={host} route={route} role={role} />
      {/* Phone / tablet: compact top bar with the app launcher instead of the sidebar. */}
      <header className="sticky top-0 z-50 flex h-14 items-center gap-2 border-b border-border bg-sidebar/90 px-3 backdrop-blur-md lg:hidden">
        <button ref={menuBtn} type="button" className={iconBtn} aria-haspopup="dialog" aria-expanded={menu}
          aria-label={cs.shell.menu} onClick={() => setMenu((v) => !v)}>
          <LayoutGrid className="h-5 w-5" aria-hidden />
        </button>
        <span className="min-w-0 truncate font-semibold">{cs.product}</span>
        <div className="ml-auto flex items-center gap-1">{actions}</div>
      </header>
      {menu && <Launcher route={route} role={role} onClose={close} />}
      {banner}
      {role === 'viewer' && (
        <p role="note" className="flex items-center gap-2 border-b border-border bg-muted/60 px-4 py-2 text-sm sm:px-6">
          <Eye className="h-4 w-4 shrink-0 text-primary" aria-hidden />{cs.shell.readOnly}
        </p>
      )}
      <div className="mx-auto max-w-[1440px] px-4 pb-8 pt-5 sm:px-6 lg:pt-7">
        <div className="mb-5 flex items-start gap-4">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
            <p className="mt-0.5 text-sm text-muted-foreground sm:text-base">{subtitle}</p>
          </div>
          <div className={cn('ml-auto hidden items-center gap-1 lg:flex')}>{actions}</div>
        </div>
        <main id="main" ref={main} tabIndex={-1} className="outline-none focus-visible:ring-0">{children}</main>
      </div>
    </div>
  );
}
