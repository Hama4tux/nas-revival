import { describe, expect, it } from 'vitest';
import { canRead, canWrite, routeFor } from './roles';

describe('roles', () => {
  it('matches the backend policy', () => {
    expect([canRead('admin'), canRead('viewer'), canRead('user')]).toEqual([true, true, false]);
    expect([canWrite('admin'), canWrite('viewer'), canWrite('user')]).toEqual([true, false, false]);
  });
  it('sends a standard user to the account page', () => {
    expect(routeFor('user', 'overview')).toBe('account');
    expect(routeFor('user', 'users')).toBe('account');
    expect(routeFor('viewer', 'users')).toBe('users');
    expect(routeFor('admin', 'account')).toBe('account');
  });
});
