import type { ServiceAction, ServiceInfo, ServicePort, UnitState } from '../api/types';

export type Tone = 'success' | 'warning' | 'danger' | 'neutral';

export function stateTone(s: UnitState): Tone {
  switch (s) {
    case 'active': case 'done': return 'success';
    case 'failed': return 'danger';
    case 'activating': case 'deactivating': return 'warning';
    default: return 'neutral';
  }
}

/** Actions that can cut something off ask first: stop, disable, and a restart of a protected unit (a broken SSH config would not come back). */
export function needsConfirm(action: ServiceAction, s: Pick<ServiceInfo, 'protected'>): boolean {
  return action === 'stop' || action === 'disable' || (s.protected && (action === 'restart' || action === 'reload'));
}

export const isLoopback = (a: string) => a.startsWith('127.') || a === '::1';

export interface PortGroup { proto: ServicePort['proto']; port: number; localOnly: boolean }

/** One entry per protocol and port (IPv4 and IPv6 sockets merge); local only when every socket is on loopback. */
export function groupPorts(ports: ServicePort[]): PortGroup[] {
  const m = new Map<string, PortGroup>();
  for (const p of ports) {
    const k = `${p.proto}/${p.port}`;
    const g = m.get(k);
    if (g) g.localOnly = g.localOnly && isLoopback(p.address);
    else m.set(k, { proto: p.proto, port: p.port, localOnly: isLoopback(p.address) });
  }
  return [...m.values()].sort((a, b) => a.port - b.port || a.proto.localeCompare(b.proto));
}

/** Installed units first (catalog order kept), units that are not installed at the end. */
export function sortServices(list: ServiceInfo[]): ServiceInfo[] {
  return [...list.filter((s) => s.state !== 'not-installed'), ...list.filter((s) => s.state === 'not-installed')];
}

/** Failed, or enabled at boot but not running (unexpectedly stopped). */
export const isProblem = (s: Pick<ServiceInfo, 'state' | 'enabled'>) =>
  s.state === 'failed' || (s.enabled === 'enabled' && s.state === 'inactive');

/** Failed units and enabled units that are not running (unexpectedly stopped). */
export function problems(list: ServiceInfo[]): ServiceInfo[] {
  return list.filter(isProblem);
}

/** A stopped unit keeps its last CPU counter; a share of CPU only means something while it runs. */
export const isRunning = (s: Pick<ServiceInfo, 'state'>) =>
  s.state === 'active' || s.state === 'activating' || s.state === 'deactivating';

/** Actions that cut something off are shown in red. */
export const isDangerAction = (a: ServiceAction) => a === 'stop' || a === 'disable';

/** Short port list for the compact row: "TCP 22, 80 · UDP 137"; loopback-only ports are left out (they are in the detail). */
export function portSummary(ports: ServicePort[]): string {
  const by = new Map<string, number[]>([['TCP', []], ['UDP', []]]);
  for (const g of groupPorts(ports)) {
    if (g.localOnly) continue;
    const k = g.proto.toUpperCase();
    by.set(k, [...(by.get(k) ?? []), g.port]);
  }
  return [...by.entries()].filter(([, ps]) => ps.length > 0).map(([proto, ps]) => `${proto} ${ps.join(', ')}`).join(' · ');
}

/** Journal priority → tone: 0–3 error, 4 warning. */
export const priorityTone = (p: number): Tone => (p <= 3 ? 'danger' : p === 4 ? 'warning' : 'neutral');
