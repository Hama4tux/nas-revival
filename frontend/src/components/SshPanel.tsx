import { KeyRound, RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SshService, SshSettings } from '../api/types';
import { cs } from '../i18n/cs';
import { portOk, sameSettings, sftpCommand, sshHealth, sshWarnings } from '../lib/ssh';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.ssh;
const CHROOT = '/srv';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const healthTone: Record<string, string> = { ok: 'text-success', notRunning: 'text-danger', unmanaged: 'text-warning' };

function SettingsForm({ svc, readOnly, onSaved }: { svc: SshService; readOnly: boolean; onSaved: (s: SshService) => void }) {
  const [form, setForm] = useState<SshSettings>(svc.settings);
  const [portText, setPortText] = useState(String(svc.settings.port));
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [fieldErr, setFieldErr] = useState<{ field: string; msg: string } | null>(null);
  useEffect(() => { setForm(svc.settings); setPortText(String(svc.settings.port)); }, [svc.settings]);

  const portBad = !/^\d{1,5}$/.test(portText.trim()) || !portOk(form.port);
  const changed = !sameSettings(form, svc.settings) || !svc.managed;
  const locked = readOnly || busy;
  const set = (p: Partial<SshSettings>) => { setForm((f) => ({ ...f, ...p })); setMsg(null); setFieldErr(null); };
  const toggleGroup = (g: string, on: boolean) =>
    set({ sftpGroups: on ? [...form.sftpGroups, g].sort() : form.sftpGroups.filter((x) => x !== g) });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (portBad) return;
    setBusy(true); setMsg(null); setErr(null); setFieldErr(null);
    try {
      const r = await api<SshService>('/services/ssh', { method: 'PUT', body: form });
      onSaved(r);
      setMsg(t.saved);
    } catch (ex) {
      if (ex instanceof ApiError && ex.field) setFieldErr({ field: ex.field, msg: ex.message });
      else setErr(apiMessage(ex));
    } finally { setBusy(false); }
  }

  const fe = (f: string) => (fieldErr?.field === f ? fieldErr.msg : null);
  const warnings = changed ? sshWarnings(svc, form) : [];
  return (
    <form onSubmit={(e) => void save(e)} className="space-y-4" noValidate>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t.port} hint={t.portHint} error={portBad ? t.portErr : fe('port')}>
          {(p) => <Input {...p} className={invalidCls} value={portText} inputMode="numeric" maxLength={5} disabled={locked}
            onChange={(e) => { setPortText(e.target.value); set({ port: Number(e.target.value.trim()) }); }} />}
        </Field>
      </div>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1 h-4 w-4" checked={form.passwordAuth} disabled={locked}
          onChange={(e) => set({ passwordAuth: e.target.checked })} />
        <span><span className="font-medium">{t.passwordAuth}</span>
          <span className="block text-xs text-muted-foreground">{t.passwordAuthHint}</span></span>
      </label>
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t.sftp}</legend>
        <p className="text-xs text-muted-foreground">{t.sftpHint(CHROOT)}</p>
        {!svc.chrootOk && <p role="note" className="text-sm text-warning">{t.chrootBad(CHROOT)}</p>}
        {svc.groups.length === 0 ? <p className="text-sm text-muted-foreground">{t.noGroups}</p> : (
          <div className="flex flex-wrap gap-x-4 gap-y-2">
            {svc.groups.map((g) => (
              <label key={g} className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="h-4 w-4" checked={form.sftpGroups.includes(g)}
                  disabled={locked || (!svc.chrootOk && !form.sftpGroups.includes(g))} onChange={(e) => toggleGroup(g, e.target.checked)} />
                {g}
              </label>
            ))}
          </div>
        )}
        {fe('sftpGroups') && <p role="alert" className="text-sm text-danger">{fe('sftpGroups')}</p>}
      </fieldset>
      {readOnly && <p role="note" className="text-sm text-muted-foreground">{t.readOnly}</p>}
      {!readOnly && (
        <div className="space-y-2">
          {warnings.map((w) => <p key={w} role="note" className="text-sm text-warning">{t.warn[w]}</p>)}
          <Button type="submit" disabled={busy || !changed || portBad}>{busy ? t.saving : t.save}</Button>
          {!changed && !msg && <span className="ml-3 text-sm text-muted-foreground">{t.unchanged}</span>}
          {msg && <p role="status" className="text-sm text-success">{msg}</p>}
          {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
        </div>
      )}
    </form>
  );
}

export function SshPanel({ readOnly }: { readOnly: boolean }) {
  const [svc, setSvc] = useState<SshService | null>(null);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async () => {
    try { setSvc(await api<SshService>('/services/ssh')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  if (!svc) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }
  const h = sshHealth(svc);
  const ports = svc.effective.ports.length ? svc.effective.ports.join(', ') : '–';
  return (
    <div className="space-y-4">
      <Widget id="ssh-settings" title={t.title} icon={<KeyRound />}
        right={<Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}>
          <RefreshCw className="h-4 w-4" aria-hidden />
        </Button>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        <p role="status" className={`mt-2 text-sm font-medium ${healthTone[h] ?? ''}`}>{t.health[h]}</p>
        <p className="mt-1 text-xs text-muted-foreground">{t.effective(ports, svc.effective.passwordAuth)}</p>
        <div className="mt-4"><SettingsForm svc={svc} readOnly={readOnly} onSaved={setSvc} /></div>
      </Widget>
      {svc.managed && svc.settings.sftpGroups.length > 0 && (
        <Widget id="ssh-connect" title={t.connect} icon={<KeyRound />}>
          <p className="text-sm text-muted-foreground">{t.connectHint}</p>
          <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted p-2 font-mono text-xs">
            {sftpCommand(window.location.hostname, svc.settings.port)}
          </pre>
        </Widget>
      )}
    </div>
  );
}
