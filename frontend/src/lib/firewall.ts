import type { FirewallRule, FirewallSettings } from '../api/types';

export type FwService = 'web' | 'ssh' | 'smb' | 'nfs';
export const FW_SERVICES: FwService[] = ['web', 'ssh', 'smb', 'nfs'];
/** Services the client must stay able to reach (web UI and SSH recovery path), as on the server. */
export const FW_ANTI_LOCKOUT: FwService[] = ['web', 'ssh'];
export const FW_MAX_ENTRIES = 16;
export const FW_MAX_RULES = 64;
export const FW_NOTE_MAX = 40;
const FW_SERVICE_PORT: Record<FwService, number> = { web: 8443, ssh: 22, smb: 445, nfs: 2049 };

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

const sameList = (a: string[], b: string[]): boolean => {
  const x = cleanList(a), y = cleanList(b);
  return x.length === y.length && x.every((v, i) => v === y[i]);
};

export const sameFirewall = (a: FirewallSettings, b: FirewallSettings): boolean =>
  a.enabled === b.enabled && FW_SERVICES.every((k) => sameList(a.allow[k], b.allow[k])) &&
  a.rules.length === b.rules.length &&
  a.rules.every((r, i) => {
    const o = b.rules[i]!;
    return r.source.trim() === o.source.trim() && r.ports.trim() === o.ports.trim() && r.proto === o.proto &&
      r.action === o.action && r.note.trim() === o.note.trim();
  });

/** "8080" or "6000-6010" (1-65535, lo < hi); null when invalid. */
export function parsePorts(s: string): { lo: number; hi: number } | null {
  const m = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(s.trim());
  if (!m) return null;
  const lo = Number(m[1]), hi = m[2] == null ? lo : Number(m[2]);
  if (lo < 1 || hi > 65535 || (m[2] != null && lo >= hi)) return null;
  return { lo, hi };
}

export interface RuleErrors { source?: true; ports?: true; note?: true }

/** Client-side check of one expert rule: one IPv4/CIDR source (required), ports empty or port/range, note length. */
export function ruleErrors(r: FirewallRule): RuleErrors {
  const e: RuleErrors = {};
  if (parseCidr(r.source) == null) e.source = true;
  if (r.ports.trim() !== '' && parsePorts(r.ports) == null) e.ports = true;
  if (r.note.trim().length > FW_NOTE_MAX) e.note = true;
  return e;
}

export const cleanRule = (r: FirewallRule): FirewallRule =>
  ({ source: r.source.trim(), ports: r.ports.trim(), proto: r.proto, action: r.action, note: r.note.trim() });

/** True when the rule matches traffic from `ip` to `port`/`proto` (empty ports = all, "both" = tcp and udp). */
export function ruleMatches(r: FirewallRule, ip: number, port: number, proto: 'tcp' | 'udp'): boolean {
  const c = parseCidr(r.source);
  if (c == null || !cidrContains(c, ip)) return false;
  if (r.proto !== 'both' && r.proto !== proto) return false;
  if (r.ports.trim() === '') return true;
  const p = parsePorts(r.ports);
  return p != null && port >= p.lo && port <= p.hi;
}

/** First matching rule decides: 'accept' / 'drop' with its index, or null when no rule matches. */
export function firstMatch(rules: FirewallRule[], ip: number, port: number, proto: 'tcp' | 'udp'): { index: number; action: 'accept' | 'drop' } | null {
  const i = rules.findIndex((r) => ruleMatches(r, ip, port, proto));
  return i < 0 ? null : { index: i, action: rules[i]!.action };
}

/** Services (and the rule index when a drop rule is the cause) that would lock the client out of web or ssh once enabled. */
export function lockoutDetail(s: FirewallSettings, client: string): { service: FwService; rule: number | null }[] {
  if (!s.enabled) return [];
  const ip = parseIPv4(client);
  const out: { service: FwService; rule: number | null }[] = [];
  for (const k of FW_ANTI_LOCKOUT) {
    const m = ip == null ? null : firstMatch(s.rules, ip, FW_SERVICE_PORT[k], 'tcp');
    if (m?.action === 'accept') continue;
    if (m?.action === 'drop') out.push({ service: k, rule: m.index });
    else if (!allowsClient(cleanList(s.allow[k]), client)) out.push({ service: k, rule: null });
  }
  return out;
}

/** Same rule as the server: an enabled firewall must let the client reach web and ssh. Returns the services that would lock it out. */
export const lockoutServices = (s: FirewallSettings, client: string): FwService[] => lockoutDetail(s, client).map((d) => d.service);
