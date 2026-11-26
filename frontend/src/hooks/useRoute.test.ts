import { describe, expect, it } from 'vitest';
import { parseRoute } from './useRoute';

describe('parseRoute', () => {
  it('maps known hashes', () => {
    expect(parseRoute('#/storage')).toBe('storage');
    expect(parseRoute('#/tasks')).toBe('tasks');
    expect(parseRoute('#/network')).toBe('network');
    expect(parseRoute('#/monitor')).toBe('monitor');
    expect(parseRoute('#/system')).toBe('system');
    expect(parseRoute('#/users')).toBe('users');
    expect(parseRoute('#/shares')).toBe('shares');
    expect(parseRoute('#/services')).toBe('services');
    expect(parseRoute('#/account')).toBe('account');
    expect(parseRoute('#/files')).toBe('files');
  });
  it('falls back to the overview', () => {
    expect(parseRoute('')).toBe('overview');
    expect(parseRoute('#/unknown')).toBe('overview');
  });
});
