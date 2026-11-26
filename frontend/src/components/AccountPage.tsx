import { KeyRound, UserRound } from 'lucide-react';
import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Role } from '../api/types';
import { cs } from '../i18n/cs';
import { passwordProblem } from '../lib/users';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';

const t = cs.account;
const pwErr = cs.users.form.pwErr;

function message(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 429) return cs.login.rateLimited(e.retryAfter);
    if (e.status === 400 || e.status === 409 || e.status === 500) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

/** Own account (W2.2): every role may change its own password here. */
export function AccountPage({ username, role }: { username: string; role: Role }) {
  const [current, setCurrent] = useState('');
  const [pw, setPw] = useState('');
  const [repeat, setRepeat] = useState('');
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<{ msg: string; field: string | null; code: string } | null>(null);
  const [done, setDone] = useState(false);

  const problem = passwordProblem(pw, repeat);
  const newErr = touched && problem && problem !== 'mismatch' ? pwErr[problem] : err?.field === 'password' ? err.msg : null;
  const repErr = touched && repeat && problem === 'mismatch' ? pwErr.mismatch : null;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (problem || !current) return;
    setBusy(true); setErr(null);
    try {
      await api('/session/password', { method: 'PUT', body: { current, password: pw } });
      setDone(true);
    } catch (x) {
      setErr({ msg: message(x), field: x instanceof ApiError ? x.field : null, code: x instanceof ApiError ? x.code : '' });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[2fr_3fr]">
      <Widget id="account-info" title={t.info} icon={<UserRound />}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-muted-foreground">{t.name}</dt><dd className="break-all font-medium">{username}</dd>
          <dt className="text-muted-foreground">{t.role}</dt><dd className="font-medium">{cs.roles[role]}</dd>
        </dl>
        <p className="mt-3 text-sm text-muted-foreground">{role === 'user' ? t.userHint : cs.roleHints[role]}</p>
      </Widget>

      <Widget id="account-password" title={t.pwTitle} icon={<KeyRound />}>
        {done ? (
          <div className="space-y-3">
            <p role="status" className="text-success">{t.done}</p>
            <Button type="button" onClick={() => location.reload()}>{t.relogin}</Button>
          </div>
        ) : (
          <form onSubmit={(e) => void submit(e)} className="max-w-md space-y-3" noValidate>
            <p className="text-sm text-muted-foreground">{t.body}</p>
            <Field label={t.current} error={err?.field === 'current' ? err.msg : null}>
              {(p) => <Input {...p} type="password" autoComplete="current-password" value={current} className={invalidCls}
                onChange={(e) => setCurrent(e.target.value)} />}
            </Field>
            <Field label={t.newPw} hint={cs.users.form.passwordHint} error={newErr}>
              {(p) => <Input {...p} type="password" autoComplete="new-password" value={pw} className={invalidCls}
                onChange={(e) => setPw(e.target.value)} onBlur={() => setTouched(true)} />}
            </Field>
            <Field label={cs.users.form.repeat} error={repErr}>
              {(p) => <Input {...p} type="password" autoComplete="new-password" value={repeat} className={invalidCls}
                onChange={(e) => { setRepeat(e.target.value); setTouched(true); }} />}
            </Field>
            {err && err.field !== 'current' && err.field !== 'password' && (
              <p role="alert" className="font-medium text-danger">{err.code === 'recovery_admin' ? t.recovery : err.msg}</p>
            )}
            <Button type="submit" disabled={busy || !current || problem !== null}>{busy ? t.saving : t.save}</Button>
          </form>
        )}
      </Widget>
    </div>
  );
}
