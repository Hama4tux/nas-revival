import { Server } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { api, ApiError } from '../api/client';
import { cs } from '../i18n/cs';
import { Button } from './ui/button';
import { Input } from './ui/input';

const t = cs.login;

export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api('/session', { method: 'POST', body: { username, password }, quiet401: true });
      setPassword('');
      onSuccess();
    } catch (err) {
      if (err instanceof ApiError) {
        setError(
          err.status === 401 ? t.invalid
            : err.status === 429 ? t.rateLimited(err.retryAfter)
            : err.status === 403 ? t.forbidden
            : err.status === 0 ? t.network : t.generic,
        );
      } else setError(t.generic);
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <form onSubmit={submit} className="w-full max-w-sm rounded-xl border border-border bg-card p-6 shadow-sm">
        <div className="mb-5 flex items-center gap-3">
          <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Server className="h-5 w-5" aria-hidden />
          </span>
          <div>
            <h1 className="text-lg font-semibold leading-tight">{cs.product}</h1>
            <p className="text-sm text-muted-foreground">{t.subtitle}</p>
          </div>
        </div>
        <label htmlFor="u" className="mb-1 block text-sm font-medium">{t.username}</label>
        <Input id="u" autoComplete="username" autoFocus required value={username} onChange={(e) => setUsername(e.target.value)} className="mb-3" />
        <label htmlFor="p" className="mb-1 block text-sm font-medium">{t.password}</label>
        <Input id="p" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} className="mb-4" />
        <div role="alert" aria-live="assertive">
          {error && <p className="mb-3 rounded-md border border-danger/40 bg-danger/10 p-2 text-sm text-danger">{error}</p>}
        </div>
        <Button type="submit" className="w-full" disabled={busy}>{busy ? t.submitting : t.submit}</Button>
      </form>
    </main>
  );
}
