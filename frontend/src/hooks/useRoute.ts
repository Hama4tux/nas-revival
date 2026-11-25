import { useEffect, useState } from 'react';

export type Route = 'overview' | 'network' | 'system';

export const parseRoute = (hash: string): Route =>
  hash === '#/network' ? 'network' : hash === '#/system' ? 'system' : 'overview';

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash));
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return route;
}
