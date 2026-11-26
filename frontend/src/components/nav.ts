import { Activity, CalendarClock, Files, Gauge, FolderOpen, HardDrive, LayoutDashboard, Network, ScrollText, Settings, UserRound, Users, type LucideIcon } from 'lucide-react';
import type { Role } from '../api/types';
import type { cs } from '../i18n/cs';
import { canRead } from '../lib/roles';

export interface NavApp { key: keyof typeof cs.nav; icon: LucideIcon; ready: boolean; tone: string; href?: string }

/** Sections of the admin UI; all but logs are ready. Order follows the sidebar. */
export const apps: NavApp[] = [
  { key: 'overview', icon: LayoutDashboard, ready: true, href: '#/', tone: 'from-sky-400 to-blue-600' },
  { key: 'storage', icon: HardDrive, ready: true, href: '#/storage', tone: 'from-emerald-400 to-teal-600' },
  { key: 'services', icon: Activity, ready: true, href: '#/services', tone: 'from-rose-400 to-pink-600' },
  { key: 'files', icon: Files, ready: true, href: '#/files', tone: 'from-yellow-400 to-amber-600' },
  { key: 'shares', icon: FolderOpen, ready: true, href: '#/shares', tone: 'from-amber-400 to-orange-600' },
  { key: 'users', icon: Users, ready: true, href: '#/users', tone: 'from-violet-400 to-purple-600' },
  { key: 'network', icon: Network, ready: true, href: '#/network', tone: 'from-cyan-400 to-sky-600' },
  { key: 'monitor', icon: Gauge, ready: true, href: '#/monitor', tone: 'from-lime-400 to-green-600' },
  { key: 'tasks', icon: CalendarClock, ready: true, href: '#/tasks', tone: 'from-fuchsia-400 to-pink-600' },
  { key: 'system', icon: Settings, ready: true, href: '#/system', tone: 'from-indigo-400 to-blue-700' },
  { key: 'logs', icon: ScrollText, ready: false, tone: 'from-slate-400 to-slate-600' },
];

const accountApp: NavApp = { key: 'account', icon: UserRound, ready: true, href: '#/account', tone: 'from-violet-400 to-purple-600' };

const filesApp = apps.find((a) => a.key === 'files')!;

/** Administrators and viewers see every section (account is in the user menu); a standard user their files and account. */
export const appsFor = (role: Role): NavApp[] => (canRead(role) ? apps : [filesApp, accountApp]);
