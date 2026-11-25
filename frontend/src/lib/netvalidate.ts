// Client-side validation mirroring docs/api/v1.md "Settings (write operations)". The server stays authoritative.

export function parseIPv4(s: string): number | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s.trim());
  if (!m) return null;
  let n = 0;
  for (let i = 1; i <= 4; i++) {
    const p = m[i]!;
    if (p.length > 1 && p.startsWith('0')) return null; // no leading zeros (ambiguous octal)
    const v = Number(p);
    if (v > 255) return null;
    n = n * 256 + v;
  }
  return n;
}

export function formatIPv4(n: number): string {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}

export const parsePrefix = (s: string): number | null => {
  const t = s.trim();
  return /^\d{1,2}$/.test(t) ? Number(t) : null;
};

const maskOf = (prefix: number): number => (prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0);

export const prefixToMask = (prefix: number): string => formatIPv4(maskOf(prefix));

export function isValidIPv6(s: string): boolean {
  if (!/^[0-9a-fA-F:.]+$/.test(s) || !s.includes(':')) return false;
  const dbl = s.split('::');
  if (dbl.length > 2) return false;
  let tail4 = 0;
  const groups = (part: string, last: boolean): string[] | null => {
    if (part === '') return [];
    const g = part.split(':');
    if (last && g.length > 0 && g[g.length - 1]!.includes('.')) {
      if (parseIPv4(g[g.length - 1]!) == null) return null;
      tail4 = 1; g.pop();
    }
    return g.every((x) => /^[0-9a-fA-F]{1,4}$/.test(x)) ? g : null;
  };
  const head = groups(dbl[0]!, dbl.length === 1);
  if (!head) return false;
  const count = head.length + (tail4 ? 2 : 0);
  if (dbl.length === 1) return count === 8;
  const rest = groups(dbl[1]!, true);
  if (!rest) return false;
  return head.length + rest.length + (tail4 ? 2 : 0) <= 7;
}

export function isIP(s: string): boolean {
  return parseIPv4(s) != null || isValidIPv6(s);
}

export type AddrError = 'invalid' | 'unspecified' | 'loopback' | 'multicast' | 'linkLocal' | 'network' | 'broadcast';

/** Host address of a static configuration must be a usable unicast address inside its subnet. */
export function checkHostAddress(address: string, prefix: number | null): AddrError | null {
  const a = parseIPv4(address);
  if (a == null) return 'invalid';
  const first = a >>> 24;
  if (first === 0) return 'unspecified';
  if (first === 127) return 'loopback';
  if (first >= 224) return 'multicast'; // multicast and reserved ranges
  if (first === 169 && ((a >>> 16) & 255) === 254) return 'linkLocal';
  if (prefix != null) {
    const mask = maskOf(prefix);
    if (((a & ~mask) >>> 0) === 0) return 'network';
    if (((a & ~mask) >>> 0) === ((~mask) >>> 0)) return 'broadcast';
  }
  return null;
}

export type StaticFields = { address: string; prefix: string; gateway: string; dns: string[] };
export type FieldErrors = Partial<Record<'address' | 'prefixLength' | 'gateway' | 'dns', string>>;

export function validateStatic(v: StaticFields): FieldErrors {
  const e: FieldErrors = {};
  const prefix = parsePrefix(v.prefix);
  if (prefix == null || prefix < 8 || prefix > 30) e.prefixLength = 'prefix';
  const ae = checkHostAddress(v.address.trim(), prefix != null && prefix >= 8 && prefix <= 30 ? prefix : null);
  if (ae) e.address = ae;
  const g = v.gateway.trim();
  const gw = parseIPv4(g);
  if (gw == null) e.gateway = 'invalid';
  else if (!e.address && !e.prefixLength) {
    const a = parseIPv4(v.address.trim())!;
    const mask = maskOf(prefix!);
    if (g === v.address.trim()) e.gateway = 'sameAsAddress';
    else if (((gw & mask) >>> 0) !== ((a & mask) >>> 0)) e.gateway = 'outsideSubnet';
    else if (checkHostAddress(g, prefix) != null) e.gateway = 'invalid';
  }
  const dnsErr = validateDns(v.dns, true);
  if (dnsErr) e.dns = dnsErr;
  return e;
}

/** `needOne`: static mode (or DHCP without DHCP DNS) needs at least one server. */
export function validateDns(list: string[], needOne: boolean): string | null {
  const l = list.map((s) => s.trim()).filter(Boolean);
  if (needOne && l.length === 0) return 'required';
  if (l.length > 3) return 'tooMany';
  if (l.some((s) => !isIP(s))) return 'invalid';
  return null;
}

/** RFC 1123 host name, ≤ 253 chars; all-numeric dotted names are treated as (bad) IPs. */
export function isHostname(s: string): boolean {
  if (s.length === 0 || s.length > 253) return false;
  if (/^[0-9.]+$/.test(s)) return false;
  return s.split('.').every((l) => l.length >= 1 && l.length <= 63 && /^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$/.test(l));
}

export function validateNtp(list: string[]): string | null {
  const l = list.map((s) => s.trim());
  if (l.length > 4) return 'tooMany';
  if (l.some((s) => s === '')) return 'empty';
  if (l.some((s) => !isHostname(s) && !isIP(s))) return 'invalid';
  if (new Set(l.map((s) => s.toLowerCase())).size !== l.length) return 'duplicate';
  return null;
}
