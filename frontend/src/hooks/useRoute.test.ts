import { describe, expect, it } from 'vitest';
import { parseRoute } from './useRoute';

describe('parseRoute', () => {
  it('maps known hashes', () => {
    expect(parseRoute('#/storage')).toBe('storage');
    expect(parseRoute('#/network')).toBe('network');
    expect(parseRoute('#/system')).toBe('system');
  });
  it('falls back to the overview', () => {
    expect(parseRoute('')).toBe('overview');
    expect(parseRoute('#/unknown')).toBe('overview');
  });
});
