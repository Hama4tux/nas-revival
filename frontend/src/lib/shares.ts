import type { Share, SharePermission, ShareAccess } from '../api/types';

/** Same rules as the backend (package shares). */
export const SHARE_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/;
export const RESERVED_NAMES = ['global', 'homes', 'printers', 'print$', 'ipc$', 'netlogon', 'profiles'];
export const MAX_PERMISSIONS = 64;

export type ShareNameProblem = 'empty' | 'format' | 'reserved' | 'exists' | null;
export function shareNameProblem(name: string, existing: string[] = []): ShareNameProblem {
  if (!name) return 'empty';
  if (!SHARE_NAME_RE.test(name)) return 'format';
  const l = name.toLowerCase();
  if (RESERVED_NAMES.includes(l)) return 'reserved';
  if (existing.some((e) => e.toLowerCase() === l)) return 'exists';
  return null;
}

export function descriptionProblem(s: string): boolean {
  return [...s.trim()].length > 64 || /[\u0000-\u001f\u007f-\u009f\\]/.test(s);
}

export function daysProblem(days: number): boolean {
  return !Number.isInteger(days) || days < 1 || days > 365;
}

export type PermissionProblem = 'groupNone' | 'duplicate' | 'tooMany' | null;
export function permissionsProblem(list: SharePermission[]): PermissionProblem {
  if (list.length > MAX_PERMISSIONS) return 'tooMany';
  const seen = new Set<string>();
  for (const p of list) {
    if (p.kind === 'group' && p.access === 'none') return 'groupNone';
    const k = `${p.kind}:${p.name}`;
    if (seen.has(k)) return 'duplicate';
    seen.add(k);
  }
  return null;
}

/** Access choices of one principal kind (POSIX groups cannot deny). */
export const accessChoices = (kind: SharePermission['kind']): ShareAccess[] => (kind === 'group' ? ['rw', 'ro'] : ['rw', 'ro', 'none']);

/** True when the form can be sent. */
export function shareValid(s: Share, creating: boolean, existing: string[]): boolean {
  return (!creating || shareNameProblem(s.name, existing) === null)
    && !descriptionProblem(s.description)
    && (!s.recycle.enabled || !daysProblem(s.recycle.days))
    && permissionsProblem(s.permissions) === null
    && (!creating || s.volume !== '');
}

/** Number of NAS users who can open the share (any access but none). */
export const usersWithAccess = (effective: { access: ShareAccess }[]): number => effective.filter((e) => e.access !== 'none').length;

export function addPermission(list: SharePermission[], p: SharePermission): SharePermission[] {
  if (list.some((x) => x.kind === p.kind && x.name === p.name)) return list;
  return [...list, p];
}
export function removePermission(list: SharePermission[], kind: string, name: string): SharePermission[] {
  return list.filter((x) => !(x.kind === kind && x.name === name));
}
