import { isValidIPv6, parseIPv4 } from './netvalidate';

const label = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/;
const isName = (s: string) => s.length <= 253 && s.split('.').every((l) => label.test(l));

/** Mirrors helper.ValidateDiag for the tests with a target. */
export function diagTargetError(test: 'ping' | 'dns', target: string): 'required' | 'invalid' | 'dnsNeedsName' | null {
  const s = target.trim();
  if (!s) return 'required';
  const v4 = parseIPv4(s);
  const v6 = v4 == null && isValidIPv6(s);
  if (test === 'dns' && (v4 != null || v6)) return 'dnsNeedsName';
  if (v4 != null) {
    const first = v4 >>> 24;
    return v4 === 0 || v4 === 0xffffffff || (first >= 224 && first <= 239) ? 'invalid' : null;
  }
  if (v6) return s.toLowerCase().startsWith('ff') || s === '::' ? 'invalid' : null;
  return isName(s) ? null : 'invalid';
}

/** Who can reach a listening socket: every interface, or only the NAS itself. */
export function listenScope(address: string): 'all' | 'local' | 'one' {
  if (address === '0.0.0.0' || address === '::' || address === '*') return 'all';
  if (address.startsWith('127.') || address === '::1') return 'local';
  return 'one';
}
