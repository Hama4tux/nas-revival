import { useCallback, useEffect, useState } from 'react';
import { api, setUnauthorizedHandler } from './api/client';
import type { NetworkSettings, Role, SessionInfo, SystemPowerAction } from './api/types';
import { AccountPage } from './components/AccountPage';
import { GoingDown } from './components/GoingDown';
import { Login } from './components/Login';
import { Overview } from './components/Overview';
import { NetworkPage } from './components/NetworkPage';
import { PendingBanner } from './components/PendingBanner';
import { Shell, type HostSummary } from './components/Shell';
import { StoragePage } from './components/StoragePage';
import { SystemPage } from './components/SystemPage';
import { UsersPage } from './components/UsersPage';
import { useNetworkSettings } from './hooks/useNetworkSettings';
import { useRoute } from './hooks/useRoute';
import { cs } from './i18n/cs';
import { canRead, routeFor } from './lib/roles';

type Auth = 'checking' | 'out' | 'in';

export function App() {
  const [auth, setAuth] = useState<Auth>('checking');
  const [loggingOut, setLoggingOut] = useState(false);
  const [host, setHost] = useState<HostSummary | null>(null);
  const [username, setUsername] = useState<string | null>(null);
  const [role, setRole] = useState<Role>('user');
  const [powering, setPowering] = useState<SystemPowerAction | null>(null);
  const route = routeFor(role, useRoute());
  // Standard users cannot read the settings; do not poll for them.
  const net = useNetworkSettings(auth === 'in' && !powering && canRead(role));

  useEffect(() => {
    setUnauthorizedHandler(() => setAuth('out'));
  }, []);

  const check = useCallback(async () => {
    try {
      const s = await api<SessionInfo>('/session', { quiet401: true });
      setUsername(s.username);
      setRole(s.role);
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

  // After a restart the session is gone; a full reload starts from the login screen.
  if (powering) return <GoingDown action={powering} onBack={() => location.reload()} />;
  if (auth === 'checking') {
    return <p role="status" className="p-8 text-center text-muted-foreground">{cs.login.checking}</p>;
  }
  if (auth === 'out') return <Login onSuccess={() => void check()} />;
  const page = route === 'overview' ? { title: cs.overview.title, subtitle: cs.overview.subtitle } : cs.pages[route];
  // Only an administrator can confirm a pending network change.
  const pending = role === 'admin' ? net.data?.pending ?? null : null;
  async function confirmPending() {
    net.setData(await api<NetworkSettings>('/settings/network/confirm', { method: 'POST' }));
  }
  return (
    <Shell host={host} username={username} role={role} onLogout={() => void logout()} loggingOut={loggingOut}
      onPowerStarted={setPowering} title={page.title} subtitle={page.subtitle} route={route}
      banner={pending && <PendingBanner pending={pending} onConfirm={confirmPending} />}>
      {route === 'overview' && <Overview onHost={setHost} />}
      {route === 'storage' && <StoragePage />}
      {route === 'network' && <NetworkPage net={net} />}
      {route === 'system' && <SystemPage />}
      {route === 'users' && <UsersPage readOnly={role !== 'admin'} self={username} />}
      {route === 'account' && <AccountPage username={username ?? ''} role={role} />}
    </Shell>
  );
}
