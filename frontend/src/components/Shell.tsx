import { Box, ChevronDown, LayoutGrid, LogOut, Moon, Sun, User } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { cs } from '../i18n/cs';
import { useTheme } from '../hooks/useTheme';
import { cn } from '../lib/utils';
import { Launcher } from './Launcher';
import { NasArt } from './NasArt';
import { apps } from './nav';

export interface HostSummary { hostname: string; boardModel: string; os: string }

const iconBtn = 'flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-sm hover:bg-muted aria-expanded:bg-muted';

/** "Debian GNU/Linux 13 (trixie)" → "Debian" */
const osShort = (os: string) => os.split(/\s/)[0] ?? os;

function UserMenu({ username, onLogout, loggingOut }: { username: string | null; onLogout: () => void; loggingOut: boolean }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const item = useRef<HTMLButtonElement>(null);

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
        <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      </button>
      {open && (
        <div role="menu" className="pop glass absolute right-0 top-11 z-50 w-44 rounded-lg p-1 text-foreground">
          <button ref={item} role="menuitem" type="button" disabled={loggingOut} onClick={onLogout}
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

function Sidebar({ host }: { host: HostSummary | null }) {
  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col border-r border-border bg-sidebar/95 px-4 py-5 backdrop-blur lg:flex">
      <div className="px-1"><Brand host={host} /></div>
      <nav aria-label={cs.shell.mainNav} className="mt-7">
        <ul className="space-y-1">
          {apps.map(({ key, icon: Icon, ready }) => (
            <li key={key}>
              {ready ? (
                <a href="#main" aria-current="page"
                  className="flex items-center gap-3 rounded-lg bg-primary/15 px-3 py-2.5 text-sm font-semibold text-foreground ring-1 ring-inset ring-primary/30">
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

export function Shell({ host, username, onLogout, loggingOut, title, subtitle, children }: {
  host: HostSummary | null; username: string | null; onLogout: () => void; loggingOut: boolean;
  title: string; subtitle: string; children: ReactNode;
}) {
  const { theme, toggle } = useTheme();
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const close = () => { setMenu(false); menuBtn.current?.focus(); };

  const actions = (
    <>
      <button type="button" className={iconBtn} onClick={toggle} aria-label={theme === 'dark' ? cs.theme.toLight : cs.theme.toDark}>
        {theme === 'dark' ? <Sun className="h-5 w-5" aria-hidden /> : <Moon className="h-5 w-5" aria-hidden />}
      </button>
      <UserMenu username={username} onLogout={onLogout} loggingOut={loggingOut} />
    </>
  );

  return (
    <div className="min-h-screen lg:pl-64">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[60] focus:rounded focus:bg-card focus:p-2">
        {cs.common.skipToContent}
      </a>
      <Sidebar host={host} />
      {/* Phone / tablet: compact top bar with the app launcher instead of the sidebar. */}
      <header className="sticky top-0 z-50 flex h-14 items-center gap-2 border-b border-border bg-sidebar/90 px-3 backdrop-blur-md lg:hidden">
        <button ref={menuBtn} type="button" className={iconBtn} aria-haspopup="dialog" aria-expanded={menu}
          aria-label={cs.shell.menu} onClick={() => setMenu((v) => !v)}>
          <LayoutGrid className="h-5 w-5" aria-hidden />
        </button>
        <span className="min-w-0 truncate font-semibold">{cs.product}</span>
        <div className="ml-auto flex items-center gap-1">{actions}</div>
      </header>
      {menu && <Launcher onClose={close} />}
      <div className="mx-auto max-w-[1440px] px-4 pb-8 pt-5 sm:px-6 lg:pt-7">
        <div className="mb-5 flex items-start gap-4">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
            <p className="mt-0.5 text-sm text-muted-foreground sm:text-base">{subtitle}</p>
          </div>
          <div className={cn('ml-auto hidden items-center gap-1 lg:flex')}>{actions}</div>
        </div>
        <main id="main" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}
