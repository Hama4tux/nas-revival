import { Activity, FolderOpen, HardDrive, LayoutDashboard, LogOut, Moon, Network, ScrollText, Settings, Sun, Users, type LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cs } from '../i18n/cs';
import { useTheme } from '../hooks/useTheme';
import { cn } from '../lib/utils';
import { Button } from './ui/button';

const items: { key: keyof typeof cs.nav; icon: LucideIcon; ready: boolean }[] = [
  { key: 'overview', icon: LayoutDashboard, ready: true },
  { key: 'storage', icon: HardDrive, ready: false },
  { key: 'shares', icon: FolderOpen, ready: false },
  { key: 'users', icon: Users, ready: false },
  { key: 'services', icon: Activity, ready: false },
  { key: 'network', icon: Network, ready: false },
  { key: 'logs', icon: ScrollText, ready: false },
  { key: 'system', icon: Settings, ready: false },
];

export function Shell({ hostname, onLogout, loggingOut, children }: {
  hostname: string | null; onLogout: () => void; loggingOut: boolean; children: ReactNode;
}) {
  const { theme, toggle } = useTheme();
  return (
    <div className="min-h-screen md:flex">
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:m-2 focus:rounded focus:bg-card focus:p-2">
        {cs.common.skipToContent}
      </a>
      <nav aria-label={cs.shell.mainNav}
        className="fixed inset-x-0 bottom-0 z-20 flex overflow-x-auto border-t border-border bg-card md:static md:w-56 md:shrink-0 md:flex-col md:overflow-visible md:border-r md:border-t-0 md:pt-16">
        <ul className="flex w-full md:flex-col md:gap-1 md:p-2">
          {items.map(({ key, icon: Icon, ready }) => (
            <li key={key} className="min-w-[4.5rem] flex-1 md:min-w-0 md:flex-none">
              <a
                href={ready ? '#main' : undefined}
                aria-current={ready ? 'page' : undefined}
                aria-disabled={ready ? undefined : true}
                tabIndex={ready ? 0 : -1}
                title={ready ? undefined : cs.shell.soon}
                className={cn(
                  'flex flex-col items-center gap-0.5 px-2 py-2 text-xs md:flex-row md:gap-3 md:rounded-md md:px-3 md:text-sm',
                  ready ? 'bg-primary/10 font-semibold text-primary' : 'cursor-not-allowed text-muted-foreground opacity-70',
                )}
              >
                <Icon className="h-5 w-5 shrink-0" aria-hidden />
                <span>{cs.nav[key]}</span>
                {!ready && <span className="sr-only">({cs.shell.soon})</span>}
                {!ready && <span className="hidden rounded bg-muted px-1.5 text-[10px] md:ml-auto md:inline">{cs.shell.soon}</span>}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-10 flex h-14 items-center gap-3 border-b border-border bg-card px-4 md:fixed md:inset-x-0 md:z-30">
          <span className="flex h-8 w-8 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground" aria-hidden>N</span>
          <span className="font-semibold">{cs.product}</span>
          {hostname && <span className="truncate text-sm text-muted-foreground" title={cs.shell.host}>{hostname}</span>}
          <div className="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="icon" onClick={toggle} aria-label={theme === 'dark' ? cs.theme.toLight : cs.theme.toDark}>
              {theme === 'dark' ? <Sun className="h-5 w-5" aria-hidden /> : <Moon className="h-5 w-5" aria-hidden />}
            </Button>
            <Button variant="outline" size="sm" onClick={onLogout} disabled={loggingOut}>
              <LogOut className="h-4 w-4" aria-hidden />{loggingOut ? cs.shell.loggingOut : cs.shell.logout}
            </Button>
          </div>
        </header>
        <main id="main" tabIndex={-1} className="flex-1 p-4 pb-24 md:pb-6 md:pt-20">{children}</main>
      </div>
    </div>
  );
}
