import { useEffect, useState } from 'react';

export type Route = 'overview' | 'storage' | 'network' | 'system' | 'users' | 'account';

const routes: Record<string, Route> = { '#/storage': 'storage', '#/network': 'network', '#/system': 'system', '#/users': 'users', '#/account': 'account' };

export const parseRoute = (hash: string): Route => routes[hash] ?? 'overview';

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash));
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return route;
}
