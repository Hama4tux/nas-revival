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

/** Failed units and enabled units that are not running (unexpectedly stopped). */
export function problems(list: ServiceInfo[]): ServiceInfo[] {
  return list.filter((s) => s.state === 'failed' || (s.enabled === 'enabled' && s.state === 'inactive'));
}

/** Journal priority → tone: 0–3 error, 4 warning. */
export const priorityTone = (p: number): Tone => (p <= 3 ? 'danger' : p === 4 ? 'warning' : 'neutral');
