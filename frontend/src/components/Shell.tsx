import { ChevronDown, LayoutGrid, LogOut, Moon, Sun, User } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { cs } from '../i18n/cs';
import { useTheme } from '../hooks/useTheme';
import { Launcher } from './Launcher';

const barBtn = 'flex h-8 items-center gap-1.5 rounded-md px-2 text-sm text-white hover:bg-white/15 aria-expanded:bg-white/20';

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
      <button ref={btn} type="button" className={barBtn} aria-haspopup="menu" aria-expanded={open}
        aria-label={cs.shell.userMenu} onClick={() => setOpen((v) => !v)}>
        <User className="h-4 w-4" aria-hidden />
        <span className="hidden sm:inline">{username ?? cs.shell.user}</span>
        <ChevronDown className="h-3 w-3" aria-hidden />
      </button>
      {open && (
        <div role="menu" className="pop glass absolute right-0 top-10 z-50 w-44 rounded-lg bg-card/95 p-1 text-foreground">
          <button ref={item} role="menuitem" type="button" disabled={loggingOut} onClick={onLogout}
            className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted disabled:opacity-50">
            <LogOut className="h-4 w-4" aria-hidden />{loggingOut ? cs.shell.loggingOut : cs.shell.logout}
          </button>
        </div>
      )}
    </div>
  );
}

export function Shell({ hostname, username, onLogout, loggingOut, children }: {
  hostname: string | null; username: string | null; onLogout: () => void; loggingOut: boolean; children: ReactNode;
}) {
  const { theme, toggle } = useTheme();
  const [menu, setMenu] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const close = () => { setMenu(false); menuBtn.current?.focus(); };

  return (
    <div className="min-h-screen">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[60] focus:rounded focus:bg-card focus:p-2">
        {cs.common.skipToContent}
      </a>
      <header className="fixed inset-x-0 top-0 z-50 flex h-11 items-center gap-2 border-b border-white/10 bg-slate-900/75 px-2 text-white backdrop-blur-md sm:px-3">
        <button ref={menuBtn} type="button" className={barBtn} aria-haspopup="dialog" aria-expanded={menu}
          aria-label={cs.shell.menu} onClick={() => setMenu((v) => !v)}>
          <LayoutGrid className="h-5 w-5" aria-hidden />
        </button>
        <span className="text-sm font-semibold">{cs.product}</span>
        <div className="ml-auto flex items-center gap-1">
          {hostname && <span className="mr-1 hidden max-w-[10rem] truncate text-sm text-white/80 sm:inline" title={cs.shell.host}>{hostname}</span>}
          <button type="button" className={barBtn} onClick={toggle} aria-label={theme === 'dark' ? cs.theme.toLight : cs.theme.toDark}>
            {theme === 'dark' ? <Sun className="h-4 w-4" aria-hidden /> : <Moon className="h-4 w-4" aria-hidden />}
          </button>
          <UserMenu username={username} onLogout={onLogout} loggingOut={loggingOut} />
        </div>
      </header>
      {menu && <Launcher onClose={close} />}
      <main id="main" tabIndex={-1} className="mx-auto max-w-7xl p-3 pt-16 sm:p-5 sm:pt-16">{children}</main>
    </div>
  );
}
