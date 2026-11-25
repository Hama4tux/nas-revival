import { useCallback, useEffect, useState } from 'react';
import { api, setUnauthorizedHandler } from './api/client';
import type { NetworkSettings, SessionInfo } from './api/types';
import { Login } from './components/Login';
import { Overview } from './components/Overview';
import { NetworkPage } from './components/NetworkPage';
import { PendingBanner } from './components/PendingBanner';
import { Shell, type HostSummary } from './components/Shell';
import { StoragePage } from './components/StoragePage';
import { SystemPage } from './components/SystemPage';
import { useNetworkSettings } from './hooks/useNetworkSettings';
import { useRoute } from './hooks/useRoute';
import { cs } from './i18n/cs';

type Auth = 'checking' | 'out' | 'in';

export function App() {
  const [auth, setAuth] = useState<Auth>('checking');
  const [loggingOut, setLoggingOut] = useState(false);
  const [host, setHost] = useState<HostSummary | null>(null);
  const [username, setUsername] = useState<string | null>(null);
  const route = useRoute();
  const net = useNetworkSettings(auth === 'in');

  useEffect(() => {
    setUnauthorizedHandler(() => setAuth('out'));
  }, []);

  const check = useCallback(async () => {
    try {
      const s = await api<SessionInfo>('/session', { quiet401: true });
      setUsername(s.username);
      setAuth('in');
    } catch {
      setAuth('out');
    }
  }, []);

  useEffect(() => { void check(); }, [check]);

  async function logout() {
    setLoggingOut(true);
    try { await api('/session', { method: 'DELETE' }); } catch { /* cookie may already be gone */ }
    setLoggingOut(false);
    setHost(null);
    setAuth('out');
  }

  if (auth === 'checking') {
    return <p role="status" className="p-8 text-center text-muted-foreground">{cs.login.checking}</p>;
  }
  if (auth === 'out') return <Login onSuccess={() => void check()} />;
  const page = route === 'overview' ? { title: cs.overview.title, subtitle: cs.overview.subtitle } : cs.pages[route];
  const pending = net.data?.pending ?? null;
  async function confirmPending() {
    net.setData(await api<NetworkSettings>('/settings/network/confirm', { method: 'POST' }));
  }
  return (
    <Shell host={host} username={username} onLogout={() => void logout()} loggingOut={loggingOut}
      title={page.title} subtitle={page.subtitle} route={route}
      banner={pending && <PendingBanner pending={pending} onConfirm={confirmPending} />}>
      {route === 'overview' && <Overview onHost={setHost} />}
      {route === 'storage' && <StoragePage />}
      {route === 'network' && <NetworkPage net={net} />}
      {route === 'system' && <SystemPage />}
    </Shell>
  );
}
