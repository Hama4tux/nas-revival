import { describe, expect, it } from 'vitest';
import type { UserInfo } from '../api/types';
import { fullNameProblem, nameProblem, passwordIssues, passwordProblem, toggle } from './users';

const user = (p: Partial<UserInfo['password']>): UserInfo => ({
  name: 'jana', uid: 1001, fullName: '', groups: [], role: 'user', disabled: false,
  password: { linux: true, samba: 'unavailable', web: true, changedOutside: false, ...p },
});

describe('user form rules', () => {
  it('accepts Linux user names and rejects the rest', () => {
    expect(nameProblem('jana')).toBeNull();
    expect(nameProblem('petr_2-x')).toBeNull();
    expect(nameProblem('')).toBe('empty');
    for (const n of ['Jana', '2x', 'jána', 'a b', 'a'.repeat(33)]) expect(nameProblem(n)).toBe('format');
    expect(nameProblem('nas-admin')).toBe('reserved');
  });

  it('checks password length, control characters and repetition', () => {
    expect(passwordProblem('', '')).toBe('short');
    expect(passwordProblem('x', 'x')).toBeNull();
    expect(passwordProblem('dlouhé heslo\nroot', 'dlouhé heslo\nroot')).toBe('control');
    expect(passwordProblem('ž'.repeat(129), 'ž'.repeat(129))).toBe('long');
    expect(passwordProblem('správné heslo 1', 'správné heslo 2')).toBe('mismatch');
    expect(passwordProblem('správné heslo 1', 'správné heslo 1')).toBeNull();
  });

  it('rejects GECOS separators in the full name', () => {
    expect(fullNameProblem('Jana Nováková')).toBe(false);
    expect(fullNameProblem('Jana: admin')).toBe(true);
    expect(fullNameProblem('a,b')).toBe(true);
  });
});

describe('password state', () => {
  it('lists what needs a new password', () => {
    expect(passwordIssues(user({}))).toEqual([]);
    expect(passwordIssues(user({ changedOutside: true, samba: 'missing' }))).toEqual(['outside', 'noSamba']);
    expect(passwordIssues(user({ web: false, linux: false }))).toEqual(['noLinux', 'noWeb']);
  });

  it('toggles group membership sorted and without duplicates', () => {
    expect(toggle(['b'], 'a', true)).toEqual(['a', 'b']);
    expect(toggle(['a', 'b'], 'a', true)).toEqual(['a', 'b']);
    expect(toggle(['a', 'b'], 'a', false)).toEqual(['b']);
  });
});
