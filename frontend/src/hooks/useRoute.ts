import { useEffect, useState } from 'react';

export type Route = 'overview' | 'storage' | 'tasks' | 'network' | 'monitor' | 'system' | 'users' | 'shares' | 'services' | 'files' | 'account' | 'logs';

const routes: Record<string, Route> = { '#/storage': 'storage', '#/tasks': 'tasks', '#/network': 'network', '#/monitor': 'monitor', '#/system': 'system', '#/users': 'users', '#/shares': 'shares', '#/services': 'services', '#/files': 'files', '#/account': 'account', '#/logs': 'logs' };

export const parseRoute = (hash: string): Route => routes[hash.replace(/^(#\/[^/]+)\/.*$/, '$1')] ?? 'overview';

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash));
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return route;
}
