import { FolderSync, RefreshCw, Users } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SmbService, SmbSettings, SmbStatus, UserList } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { descriptionOk, disconnects, sameSettings, shareAddress, smbHealth, workgroupOk } from '../lib/smb';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.smb;
const STATUS_POLL_MS = 10_000;
const PROTOCOLS = ['SMB2_10', 'SMB3_00', 'SMB3_11'] as const;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const dateTime = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '–' : d.toLocaleString('cs-CZ');
};

const healthTone: Record<string, string> = {
  ok: 'text-success', off: 'text-muted-foreground', notInstalled: 'text-warning',
  notRunning: 'text-danger', runningWhileOff: 'text-warning', unmanaged: 'text-warning',
};

function Check({ label, hint, checked, disabled, onChange }: {
  label: string; hint: string; checked: boolean; disabled: boolean; onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-start gap-2 text-sm">
      <input type="checkbox" className="mt-1 h-4 w-4" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span><span className="font-medium">{label}</span><span className="block text-xs text-muted-foreground">{hint}</span></span>
    </label>
  );
}

function SettingsForm({ svc, readOnly, onSaved }: { svc: SmbService; readOnly: boolean; onSaved: (s: SmbService) => void }) {
  const [form, setForm] = useState<SmbSettings>(svc.settings);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [fieldErr, setFieldErr] = useState<{ field: string; msg: string } | null>(null);
  useEffect(() => { setForm(svc.settings); }, [svc.settings]);

  const wgBad = !workgroupOk(form.workgroup);
  const descBad = !descriptionOk(form.description);
  const changed = !sameSettings(form, svc.settings) || (form.enabled && !svc.managed);
  const locked = readOnly || busy || !svc.installed;
  const set = (p: Partial<SmbSettings>) => { setForm((f) => ({ ...f, ...p })); setMsg(null); setFieldErr(null); };

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (wgBad || descBad) return;
    setBusy(true); setMsg(null); setErr(null); setFieldErr(null);
    try {
      const r = await api<SmbService>('/services/smb', { method: 'PUT', body: form });
      onSaved(r);
      setMsg(t.saved);
    } catch (ex) {
      if (ex instanceof ApiError && ex.field) setFieldErr({ field: ex.field, msg: ex.message });
      else setErr(apiMessage(ex));
    } finally { setBusy(false); }
  }

  const fe = (f: string) => (fieldErr?.field === f ? fieldErr.msg : null);
  return (
    <form onSubmit={(e) => void save(e)} className="space-y-4" noValidate>
      <Check label={t.enabled} hint={t.enabledHint} checked={form.enabled} disabled={locked} onChange={(v) => set({ enabled: v })} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t.workgroup} hint={t.workgroupHint} error={wgBad ? t.workgroupErr : fe('workgroup')}>
          {(p) => <Input {...p} className={invalidCls} value={form.workgroup} maxLength={15} disabled={locked}
            autoCapitalize="characters" spellCheck={false} onChange={(e) => set({ workgroup: e.target.value })} />}
        </Field>
        <Field label={t.description} hint={t.descriptionHint} error={descBad ? t.descriptionErr : fe('description')}>
          {(p) => <Input {...p} className={invalidCls} value={form.description} maxLength={64} disabled={locked}
            onChange={(e) => set({ description: e.target.value })} />}
        </Field>
      </div>
      <Field label={t.minProtocol} hint={t.maxProtocol} error={fe('minProtocol')}>
        {(p) => (
          <select {...p} className="h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm" value={form.minProtocol}
            disabled={locked} onChange={(e) => set({ minProtocol: e.target.value as SmbSettings['minProtocol'] })}>
            {PROTOCOLS.map((v) => <option key={v} value={v}>{t.protocols[v]}</option>)}
          </select>
        )}
      </Field>
      <Check label={t.netbios} hint={t.netbiosHint} checked={form.netbios} disabled={locked} onChange={(v) => set({ netbios: v })} />
      {readOnly && <p role="note" className="text-sm text-muted-foreground">{t.readOnly}</p>}
      {!readOnly && svc.installed && (
        <div className="space-y-2">
          {changed && disconnects(svc, form) && <p role="note" className="text-sm text-warning">{t.disconnect}</p>}
          <Button type="submit" disabled={busy || !changed || wgBad || descBad}>{busy ? t.saving : t.save}</Button>
          {!changed && !msg && <span className="ml-3 text-sm text-muted-foreground">{t.unchanged}</span>}
          {msg && <p role="status" className="text-sm text-success">{msg}</p>}
          {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
        </div>
      )}
    </form>
  );
}

function StatusPanel({ running }: { running: boolean }) {
  const [st, setSt] = useState<SmbStatus | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const loading = useRef(false);
  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try { setSt(await api<SmbStatus>('/services/smb/status')); setErr(null); }
    catch (e) { setErr(e instanceof ApiError && e.status === 403 ? t.status.adminOnly : `${t.status.loadError} ${apiMessage(e)}`); }
    finally { loading.current = false; }
  }, []);
  useVisiblePoll(() => { if (running) void load(); }, STATUS_POLL_MS);
  useEffect(() => { if (running) void load(); }, [running, load]);
  const s = t.status;

  return (
    <Widget id="smb-status" title={s.title} icon={<Users />}
      right={running ? <Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}>
        <RefreshCw className="h-4 w-4" aria-hidden />
      </Button> : undefined}>
      {!running ? <p className="text-sm text-muted-foreground">{s.off}</p> : err ? <p role="alert" className="text-sm text-danger">{err}</p>
        : !st ? <Skeleton className="h-24 w-full" /> : (
          <div className="space-y-4 text-sm">
            <section>
              <h3 className="mb-1 font-semibold">{s.sessions}</h3>
              {st.sessions.length === 0 ? <p className="text-muted-foreground">{s.noSessions}</p> : (
                <ul className="divide-y divide-border">
                  {st.sessions.map((x) => (
                    <li key={x.id} className="grid grid-cols-2 gap-x-4 gap-y-1 py-2 sm:grid-cols-5">
                      <span><span className="block text-xs text-muted-foreground">{s.user}</span><span className="font-medium">{x.user || '–'}</span></span>
                      <span className="break-all"><span className="block text-xs text-muted-foreground">{s.machine}</span>{x.machine || '–'}</span>
                      <span><span className="block text-xs text-muted-foreground">{s.dialect}</span>{x.dialect || '–'}
                        <span className="block text-xs text-muted-foreground">{x.encryption ? s.encrypted(x.encryption) : s.plain}</span></span>
                      <span className="break-words"><span className="block text-xs text-muted-foreground">{s.shares}</span>{x.shares.join(', ') || '–'}</span>
                      <span><span className="block text-xs text-muted-foreground">{s.since}</span>{dateTime(x.since)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <section>
              <h3 className="mb-1 font-semibold">{s.openFiles}</h3>
              {st.openFiles.length === 0 ? <p className="text-muted-foreground">{s.noFiles}</p> : (
                <ul className="divide-y divide-border">
                  {st.openFiles.map((f, i) => (
                    <li key={`${f.share}/${f.path}/${f.user}/${i}`} className="flex flex-wrap gap-x-4 gap-y-1 py-2">
                      <span className="min-w-0 flex-1 break-all font-medium">{f.share}/{f.path}</span>
                      <span>{f.user || '–'}</span>
                      <span className="text-muted-foreground">{s.accessKind[f.access] ?? '–'}</span>
                      <span className="text-muted-foreground">{dateTime(f.since)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            {st.truncated && <p role="note" className="text-warning">{s.truncated}</p>}
          </div>
        )}
    </Widget>
  );
}

export function SmbPanel({ readOnly }: { readOnly: boolean }) {
  const [svc, setSvc] = useState<SmbService | null>(null);
  const [failed, setFailed] = useState(false);
  const [noPw, setNoPw] = useState<string[]>([]);

  const load = useCallback(async () => {
    try { setSvc(await api<SmbService>('/services/smb')); setFailed(false); }
    catch { setFailed(true); }
    try {
      const u = await api<UserList>('/users');
      setNoPw(u.users.filter((x) => !x.disabled && x.password.samba === 'missing').map((x) => x.name));
    } catch { setNoPw([]); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  if (!svc) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }
  const h = smbHealth(svc);
  const host = window.location.hostname;
  const addr = shareAddress(host, '…');
  return (
    <div className="space-y-4">
      <Widget id="smb-settings" title={t.title} icon={<FolderSync />}
        right={<Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}>
          <RefreshCw className="h-4 w-4" aria-hidden />
        </Button>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        <p role="status" className={`mt-2 text-sm font-medium ${healthTone[h] ?? ''}`}>{t.health[h]}</p>
        {svc.running && (
          <p className="mt-1 text-sm"><span className="text-muted-foreground">{t.address}: </span>
            <span className="break-all font-mono text-xs">{t.addressHint(addr.unc, addr.url)}</span></p>
        )}
        {svc.installed && noPw.length > 0 && <p role="note" className="mt-2 text-sm text-warning">{t.noPassword(noPw.join(', '))}</p>}
        <div className="mt-4"><SettingsForm svc={svc} readOnly={readOnly} onSaved={setSvc} /></div>
      </Widget>
      {!readOnly && <StatusPanel running={svc.running} />}
    </div>
  );
}
