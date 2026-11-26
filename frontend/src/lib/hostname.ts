/** Mirrors helper.ValidateHostname: one RFC 1123 label, lowercased. */
export type HostnameError = 'required' | 'tooLong' | 'chars' | 'numeric' | 'localhost';

export function normalizeHostname(s: string): string {
  return s.trim().toLowerCase();
}

export function hostnameError(s: string): HostnameError | null {
  const n = normalizeHostname(s);
  if (!n) return 'required';
  if (n.length > 63) return 'tooLong';
  if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(n)) return 'chars';
  if (/^\d+$/.test(n)) return 'numeric';
  if (n === 'localhost') return 'localhost';
  return null;
}

/** Windows shows SMB computer names cut to 15 characters (NetBIOS). */
export const hostnameLong = (s: string) => normalizeHostname(s).length > 15;

