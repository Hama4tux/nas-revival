import { describe, expect, it } from 'vitest';
import { hostnameError, hostnameLong, normalizeHostname } from './hostname';

describe('hostname', () => {
  it('accepts single labels and lowercases them', () => {
    expect(normalizeHostname(' NSA310 ')).toBe('nsa310');
    for (const ok of ['nas', 'NAS-1', 'a'.repeat(63)]) expect(hostnameError(ok)).toBeNull();
  });
  it('rejects what the helper rejects', () => {
    expect(hostnameError('  ')).toBe('required');
    expect(hostnameError('a'.repeat(64))).toBe('tooLong');
    for (const bad of ['-nas', 'nas-', 'nas.home', 'nas_1', 'náš', 'a b']) expect(hostnameError(bad)).toBe('chars');
    expect(hostnameError('123')).toBe('numeric');
    expect(hostnameError('localhost')).toBe('localhost');
  });
  it('warns over 15 characters', () => {
    expect(hostnameLong('a'.repeat(15))).toBe(false);
    expect(hostnameLong('a'.repeat(16))).toBe(true);
  });
});
