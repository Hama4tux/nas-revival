import type { FirewallSettings } from '../api/types';

export type FwService = 'web' | 'ssh' | 'smb' | 'nfs';
export const FW_SERVICES: FwService[] = ['web', 'ssh', 'smb', 'nfs'];
/** Services the client must stay able to reach (web UI and SSH recovery path), as on the server. */
export const FW_ANTI_LOCKOUT: FwService[] = ['web', 'ssh'];
export const FW_MAX_ENTRIES = 16;

/** Dotted-quad IPv4 to an unsigned 32-bit number; null when not a strict IPv4 address. */
export function parseIPv4(s: string): number | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s.trim());
  if (!m) return null;
  let n = 0;
  for (let i = 1; i <= 4; i++) {
    const p = m[i]!;
    if ((p.length > 1 && p.startsWith('0')) || Number(p) > 255) return null;
    n = n * 256 + Number(p);
  }
  return n;
}

export interface Cidr { addr: number; prefix: number }

/** "a.b.c.d" (= /32) or "a.b.c.d/n" (n 0-32); null when invalid. */
export function parseCidr(s: string): Cidr | null {
  const t = s.trim();
  const i = t.indexOf('/');
  const addr = parseIPv4(i < 0 ? t : t.slice(0, i));
  if (addr == null) return null;
  if (i < 0) return { addr, prefix: 32 };
  const ps = t.slice(i + 1);
  if (!/^\d{1,2}$/.test(ps) || Number(ps) > 32) return null;
  return { addr, prefix: Number(ps) };
}

const maskOf = (prefix: number) => (prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0);

/** True when `ip` lies inside the network `c`. */
export function cidrContains(c: Cidr, ip: number): boolean {
  const m = maskOf(c.prefix);
  return ((c.addr & m) >>> 0) === ((ip & m) >>> 0);
}

/** True when any entry in `list` is the client address or a network containing it. Invalid entries are ignored. */
export function allowsClient(list: string[], client: string): boolean {
  const ip = parseIPv4(client);
  if (ip == null) return false;
  return list.some((e) => { const c = parseCidr(e); return c != null && cidrContains(c, ip); });
}

/** Entries that are not valid IPv4 addresses or networks (blank rows are dropped before this, see cleanList). */
export const invalidEntries = (list: string[]): string[] => list.filter((e) => parseCidr(e) == null);

export const cleanList = (list: string[]): string[] => list.map((e) => e.trim()).filter(Boolean);

export const isAnywhere = (list: string[]): boolean => list.some((e) => { const c = parseCidr(e); return c != null && c.prefix === 0; });

export const sameFirewall = (a: FirewallSettings, b: FirewallSettings): boolean =>
  a.enabled === b.enabled && FW_SERVICES.every((k) => {
    const x = cleanList(a.allow[k]), y = cleanList(b.allow[k]);
    return x.length === y.length && x.every((v, i) => v === y[i]);
  });

/** Same rule as the server: an enabled firewall must let the client reach web and ssh. Returns the services that would lock it out. */
export function lockoutServices(s: FirewallSettings, client: string): FwService[] {
  if (!s.enabled) return [];
  return FW_ANTI_LOCKOUT.filter((k) => !allowsClient(cleanList(s.allow[k]), client));
}
