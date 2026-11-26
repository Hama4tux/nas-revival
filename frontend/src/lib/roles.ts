import type { Role } from '../api/types';
import type { Route } from '../hooks/useRoute';

/** Same policy as the backend (requireSession): viewers read, only admins change anything. */
export const canRead = (r: Role) => r === 'admin' || r === 'viewer';
export const canWrite = (r: Role) => r === 'admin';

/** A standard user only has their own account page. */
export function routeFor(r: Role, route: Route): Route {
  return canRead(r) || route === 'account' ? route : 'account';
}
