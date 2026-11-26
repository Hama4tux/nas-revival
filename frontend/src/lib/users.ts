import type { UserInfo } from '../api/types';

/** Same rules as the helper (ValidName, ValidatePassword, ValidateFullName). */
export const NAME_RE = /^[a-z][a-z0-9_-]{0,31}$/;
export const MIN_PASSWORD = 12;

export type NameProblem = 'empty' | 'format' | 'reserved' | null;
export function nameProblem(name: string): NameProblem {
  if (!name) return 'empty';
  if (!NAME_RE.test(name)) return 'format';
  if (name.startsWith('nas-')) return 'reserved';
  return null;
}

export type PasswordProblem = 'short' | 'control' | 'long' | 'mismatch' | null;
export function passwordProblem(pw: string, repeat: string): PasswordProblem {
  if ([...pw].length < MIN_PASSWORD) return 'short';
  if (/[\u0000-\u001f\u007f-\u009f]/.test(pw)) return 'control';
  if (new TextEncoder().encode(pw).length > 256) return 'long';
  if (pw !== repeat) return 'mismatch';
  return null;
}

export function fullNameProblem(s: string): boolean {
  return [...s.trim()].length > 64 || /[:,\u0000-\u001f\u007f]/.test(s);
}

/** Something the administrator should fix about the user's passwords. */
export type PasswordIssue = 'outside' | 'noWeb' | 'noSamba' | 'noLinux';
export function passwordIssues(u: UserInfo): PasswordIssue[] {
  const out: PasswordIssue[] = [];
  if (!u.password.linux) out.push('noLinux');
  if (u.password.changedOutside) out.push('outside');
  if (!u.password.web) out.push('noWeb');
  if (u.password.samba === 'missing') out.push('noSamba');
  return out;
}

export function toggle(list: string[], item: string, on: boolean): string[] {
  const s = new Set(list);
  if (on) s.add(item); else s.delete(item);
  return [...s].sort();
}
