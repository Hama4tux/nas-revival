import { KeyRound, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SshKey, SshKeys } from '../api/types';
import { cs } from '../i18n/cs';
import { keyPasteProblem, keyTypeLabel } from '../lib/ssh';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field } from './ui/field';
import { Skeleton } from './ui/skeleton';

const t = cs.ssh.keys;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';
const taCls = 'min-h-24 w-full rounded-md border border-border bg-background/80 p-2 font-mono text-xs aria-[invalid=true]:border-danger';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

/** Public keys of NAS users (W2.7b). `reload` changes when the SSH settings were saved (SFTP groups). */
export function SshKeysPanel({ readOnly, reload }: { readOnly: boolean; reload: string }) {
  const [data, setData] = useState<SshKeys | null>(null);
  const [failed, setFailed] = useState(false);
  const [user, setUser] = useState('');
  const [text, setText] = useState('');
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [removing, setRemoving] = useState<SshKey | null>(null);

  const load = useCallback(async () => {
    try { setData(await api<SshKeys>('/services/ssh/keys')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load, reload]);
  useEffect(() => {
    if (data && !data.users.some((u) => u.name === user)) setUser(data.users[0]?.name ?? '');
  }, [data, user]);

  if (!data) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-32 w-full" /></div>;
  }

  const cur = data.users.find((u) => u.name === user);
  const problem = keyPasteProblem(text);
  const showProblem = touched && problem !== null;

  async function change(path: string, method: 'POST' | 'DELETE', body: object, done: string): Promise<boolean> {
    setBusy(true); setMsg(null); setErr(null);
    try {
      setData(await api<SshKeys>(path, { method, body }));
      setMsg(done);
      return true;
    } catch (ex) {
      setErr(apiMessage(ex));
      return false;
    } finally { setBusy(false); }
  }

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (problem || !cur) return;
    if (await change(`/services/ssh/keys/${encodeURIComponent(cur.name)}`, 'POST', { key: text.trim() }, t.added)) {
      setText(''); setTouched(false);
    }
  }

  async function remove() {
    if (!removing || !cur) return;
    if (await change(`/services/ssh/keys/${encodeURIComponent(cur.name)}`, 'DELETE', { fingerprint: removing.fingerprint }, t.removed)) {
      setRemoving(null);
    }
  }

  return (
    <Widget id="ssh-keys" title={t.title} icon={<KeyRound />}>
      <p className="text-sm text-muted-foreground">{t.hint}</p>
      {data.users.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">{t.noUsers}</p> : (
        <div className="mt-4 space-y-4">
          <Field label={t.user} className="sm:max-w-xs">
            {(p) => (
              <select {...p} className={selectCls} value={user} disabled={busy}
                onChange={(e) => { setUser(e.target.value); setMsg(null); setErr(null); }}>
                {data.users.map((u) => <option key={u.name} value={u.name}>{u.name} ({t.count(u.keys.length)})</option>)}
              </select>
            )}
          </Field>
          {cur && !cur.sftp && <p role="note" className="text-sm text-warning">{t.notSftp}</p>}
          {cur && (cur.invalid ?? 0) > 0 && <p role="note" className="text-sm text-warning">{t.invalid(cur.invalid ?? 0)}</p>}
          {cur && (cur.keys.length === 0 ? <p className="text-sm text-muted-foreground">{t.none}</p> : (
            <ul aria-label={t.list(cur.name)} className="divide-y divide-border/70 rounded-md border border-border/70">
              {cur.keys.map((k) => (
                <li key={k.fingerprint} className="flex items-start gap-3 p-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">{k.comment || t.noComment}
                      <span className="ml-2 text-xs font-normal text-muted-foreground">{keyTypeLabel(k.type, k.bits)}</span></p>
                    <p className="break-all font-mono text-xs text-muted-foreground">{k.fingerprint}</p>
                  </div>
                  {!readOnly && (
                    <Button type="button" variant="outline" size="sm" disabled={busy} aria-label={t.removeLabel(k.comment || k.fingerprint)}
                      onClick={() => { setRemoving(k); setMsg(null); setErr(null); }}>
                      <Trash2 className="h-4 w-4" aria-hidden /><span className="ml-1 hidden sm:inline">{t.remove}</span>
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          ))}
          {readOnly ? <p role="note" className="text-sm text-muted-foreground">{cs.ssh.readOnly}</p> : cur && (
            <form onSubmit={(e) => void add(e)} className="space-y-2" noValidate>
              <Field label={t.add} hint={t.addHint} error={showProblem ? t.paste[problem] : null}>
                {(p) => <textarea {...p} className={taCls} value={text} spellCheck={false} autoComplete="off" disabled={busy}
                  placeholder="ssh-ed25519 AAAA… jmeno@pocitac"
                  onChange={(e) => { setText(e.target.value); setMsg(null); setErr(null); }} onBlur={() => setTouched(text !== '')} />}
              </Field>
              <Button type="submit" disabled={busy || !text.trim()}>{busy && !removing ? t.adding : t.addBtn}</Button>
            </form>
          )}
          {msg && <p role="status" className="text-sm text-success">{msg}</p>}
          {err && !removing && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
        </div>
      )}
      {removing && cur && (
        <Dialog title={t.removeTitle} onClose={() => { setRemoving(null); setErr(null); }} busy={busy}
          footer={<>
            <Button type="button" variant="outline" disabled={busy} onClick={() => { setRemoving(null); setErr(null); }}>{t.cancel}</Button>
            <Button type="button" variant="destructive" disabled={busy} onClick={() => void remove()} data-autofocus>
              {busy ? t.removing : t.remove}</Button>
          </>}>
          <p>{t.removeText(removing.comment || t.noComment, cur.name)}</p>
          <p className="break-all font-mono text-xs text-muted-foreground">{removing.fingerprint}</p>
          {err && <p role="alert" className="font-medium text-danger">{err}</p>}
        </Dialog>
      )}
    </Widget>
  );
}
