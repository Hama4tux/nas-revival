import { useCallback, useEffect, useState } from 'react';
import { api, setUnauthorizedHandler } from './api/client';
import type { SessionInfo } from './api/types';
import { Login } from './components/Login';
import { Overview } from './components/Overview';
import { Shell, type HostSummary } from './components/Shell';
import { cs } from './i18n/cs';

type Auth = 'checking' | 'out' | 'in';

export function App() {
  const [auth, setAuth] = useState<Auth>('checking');
  const [loggingOut, setLoggingOut] = useState(false);
  const [host, setHost] = useState<HostSummary | null>(null);
  const [username, setUsername] = useState<string | null>(null);

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
  return (
    <Shell host={host} username={username} onLogout={() => void logout()} loggingOut={loggingOut}
      title={cs.overview.title} subtitle={cs.overview.subtitle}>
      <Overview onHost={setHost} />
    </Shell>
  );
}
