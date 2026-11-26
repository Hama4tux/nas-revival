import { Activity, FolderOpen, HardDrive, LayoutDashboard, Network, ScrollText, Settings, Users, type LucideIcon } from 'lucide-react';
import type { cs } from '../i18n/cs';

export interface NavApp { key: keyof typeof cs.nav; icon: LucideIcon; ready: boolean; tone: string; href?: string }

/** Sections of the admin UI; overview, storage, users, network and system are ready. Order follows the sidebar. */
export const apps: NavApp[] = [
  { key: 'overview', icon: LayoutDashboard, ready: true, href: '#/', tone: 'from-sky-400 to-blue-600' },
  { key: 'storage', icon: HardDrive, ready: true, href: '#/storage', tone: 'from-emerald-400 to-teal-600' },
  { key: 'services', icon: Activity, ready: false, tone: 'from-rose-400 to-pink-600' },
  { key: 'shares', icon: FolderOpen, ready: false, tone: 'from-amber-400 to-orange-600' },
  { key: 'users', icon: Users, ready: true, href: '#/users', tone: 'from-violet-400 to-purple-600' },
  { key: 'network', icon: Network, ready: true, href: '#/network', tone: 'from-cyan-400 to-sky-600' },
  { key: 'system', icon: Settings, ready: true, href: '#/system', tone: 'from-indigo-400 to-blue-700' },
  { key: 'logs', icon: ScrollText, ready: false, tone: 'from-slate-400 to-slate-600' },
];
