import { Plus, RefreshCw, Server, Trash2, Users } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { NfsExport, NfsService, NfsSettings, NfsShare, NfsStatus } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { disconnects, exportOk, hostOk, nfsHealth, proxmoxCommand, sameSettings } from '../lib/nfs';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.nfs;
const STATUS_POLL_MS = 10_000;
const SQUASHES = ['root', 'all'] as const;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const healthTone: Record<string, string> = {
  ok: 'text-success', off: 'text-muted-foreground', notInstalled: 'text-warning',
  notRunning: 'text-danger', runningWhileOff: 'text-warning', unmanaged: 'text-warning',
};

function ExportEditor({ exp, shares, used, locked, onChange, onRemove }: {
  exp: NfsExport; shares: NfsShare[]; used: Set<string>; locked: boolean;
  onChange: (e: NfsExport) => void; onRemove: () => void;
}) {
  const share = shares.find((s) => s.name === exp.share);
  const setClient = (i: number, p: Partial<NfsExport['clients'][number]>) =>
    onChange({ ...exp, clients: exp.clients.map((c, j) => (j === i ? { ...c, ...p } : c)) });
  return (
    <li className="space-y-3 rounded-md border border-border p-3">
      <div className="flex items-end gap-2">
        <Field label={t.share} className="min-w-0 flex-1">
          {(p) => (
            <select {...p} className={selectCls} value={exp.share} disabled={locked} onChange={(e) => onChange({ ...exp, share: e.target.value })}>
              {exp.share === '' && <option value="">{t.chooseShare}</option>}
              {shares.filter((s) => s.name === exp.share || !used.has(s.name)).map((s) => (
                <option key={s.name} value={s.name}>{s.name} ({s.path})</option>
              ))}
            </select>
          )}
        </Field>
        {!locked && (
          <Button type="button" variant="outline" size="icon" aria-label={t.removeExport(exp.share)} onClick={onRemove}>
            <Trash2 className="h-4 w-4" aria-hidden />
          </Button>
        )}
      </div>
      {share?.readOnly && <p role="note" className="text-xs text-warning">{t.shareReadOnly}</p>}
      {share && !share.readOnly && !share.groupWrite && exp.clients.some((c) => !c.readOnly) &&
        <p role="note" className="text-xs text-warning">{t.noGroupWrite}</p>}
      <Field label={t.squash} hint={t.squashHint}>
        {(p) => (
          <select {...p} className={selectCls} value={exp.squash} disabled={locked}
            onChange={(e) => onChange({ ...exp, squash: e.target.value as NfsExport['squash'] })}>
            {SQUASHES.map((v) => <option key={v} value={v}>{t.squashes[v]}</option>)}
          </select>
        )}
      </Field>
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t.clients}</legend>
        <p className="text-xs text-muted-foreground">{t.clientsHint}</p>
        {exp.clients.length === 0 && <p className="text-xs font-medium text-danger">{t.needClient}</p>}
        <ul className="space-y-2">
          {exp.clients.map((c, i) => {
            const bad = c.host.trim() !== '' && !hostOk(c.host);
            return (
              <li key={i} className="flex flex-wrap items-start gap-2">
                <div className="min-w-0 flex-1 basis-48">
                  <Input aria-label={t.host} aria-invalid={bad || c.host.trim() === ''} className={invalidCls} value={c.host} disabled={locked}
                    inputMode="decimal" spellCheck={false} autoCapitalize="none" placeholder="192.168.1.20"
                    onChange={(e) => setClient(i, { host: e.target.value })} />
                  {bad && <p className="mt-1 text-xs font-medium text-danger">{t.hostErr}</p>}
                </div>
                <label className="flex h-10 items-center gap-2 text-sm">
                  <input type="checkbox" className="h-4 w-4" checked={c.readOnly || !!share?.readOnly} disabled={locked || !!share?.readOnly}
                    onChange={(e) => setClient(i, { readOnly: e.target.checked })} />
                  {t.clientReadOnly}
                </label>
                {!locked && (
                  <Button type="button" variant="ghost" size="icon" aria-label={t.removeClient(c.host)}
                    onClick={() => onChange({ ...exp, clients: exp.clients.filter((_, j) => j !== i) })}>
                    <Trash2 className="h-4 w-4" aria-hidden />
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
        {!locked && exp.clients.length < 16 && (
          <Button type="button" variant="outline" size="sm" onClick={() => onChange({ ...exp, clients: [...exp.clients, { host: '', readOnly: false }] })}>
            <Plus className="mr-1 h-4 w-4" aria-hidden />{t.addClient}
          </Button>
        )}
      </fieldset>
    </li>
  );
}

function SettingsForm({ svc, readOnly, onSaved }: { svc: NfsService; readOnly: boolean; onSaved: (s: NfsService) => void }) {
  const [form, setForm] = useState<NfsSettings>(svc.settings);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => { setForm(svc.settings); }, [svc.settings]);

  const valid = form.exports.every(exportOk);
  const changed = !sameSettings(form, svc.settings) || (form.enabled && !svc.managed);
  const locked = readOnly || busy || !svc.installed;
  const used = new Set(form.exports.map((e) => e.share));
  const free = svc.shares.filter((s) => !used.has(s.name));
  const set = (p: Partial<NfsSettings>) => { setForm((f) => ({ ...f, ...p })); setMsg(null); setErr(null); };
  const setExport = (i: number, e: NfsExport) => set({ exports: form.exports.map((x, j) => (j === i ? e : x)) });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!valid) return;
    setBusy(true); setMsg(null); setErr(null);
    const body: NfsSettings = { ...form, exports: form.exports.map((x) => ({ ...x, clients: x.clients.map((c) => ({ ...c, host: c.host.trim() })) })) };
    try {
      onSaved(await api<NfsService>('/services/nfs', { method: 'PUT', body }));
      setMsg(t.saved);
    } catch (ex) { setErr(apiMessage(ex)); } finally { setBusy(false); }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="space-y-4" noValidate>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1 h-4 w-4" checked={form.enabled} disabled={locked} onChange={(e) => set({ enabled: e.target.checked })} />
        <span><span className="font-medium">{t.enabled}</span><span className="block text-xs text-muted-foreground">{t.enabledHint}</span></span>
      </label>
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t.exports}</h3>
        {form.exports.length === 0 && <p className="text-sm text-muted-foreground">{t.noExports}</p>}
        <ul className="space-y-3">
          {form.exports.map((x, i) => (
            <ExportEditor key={i} exp={x} shares={svc.shares} used={used} locked={locked}
              onChange={(n) => setExport(i, n)} onRemove={() => set({ exports: form.exports.filter((_, j) => j !== i) })} />
          ))}
        </ul>
        {!locked && (svc.shares.length === 0 ? <p className="text-sm text-muted-foreground">{t.noShares}</p>
          : free.length === 0 ? <p className="text-sm text-muted-foreground">{t.allExported}</p> : (
            <Button type="button" variant="outline" size="sm"
              onClick={() => set({ exports: [...form.exports, { share: free[0]?.name ?? '', squash: 'root', clients: [{ host: '', readOnly: false }] }] })}>
              <Plus className="mr-1 h-4 w-4" aria-hidden />{t.addExport}
            </Button>
          ))}
      </section>
      {readOnly && <p role="note" className="text-sm text-muted-foreground">{t.readOnly}</p>}
      {!readOnly && svc.installed && (
        <div className="space-y-2">
          {changed && disconnects(svc, form) && <p role="note" className="text-sm text-warning">{t.disconnect}</p>}
          <Button type="submit" disabled={busy || !changed || !valid}>{busy ? t.saving : t.save}</Button>
          {!changed && !msg && <span className="ml-3 text-sm text-muted-foreground">{t.unchanged}</span>}
          {msg && <p role="status" className="text-sm text-success">{msg}</p>}
          {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
        </div>
      )}
    </form>
  );
}

function ProxmoxHelp({ svc }: { svc: NfsService }) {
  const host = window.location.hostname;
  const rows = svc.settings.exports
    .map((e) => svc.shares.find((s) => s.name === e.share))
    .filter((s): s is NfsShare => !!s);
  if (!svc.running || rows.length === 0) return null;
  return (
    <Widget id="nfs-proxmox" title={t.proxmox.title} icon={<Server />}>
      <p className="text-sm text-muted-foreground">{t.proxmox.hint}</p>
      <ul className="mt-2 space-y-2">
        {rows.map((s) => (
          <li key={s.name} className="text-sm">
            <span className="text-muted-foreground">{t.proxmox.path}: </span><span className="font-mono text-xs">{s.path}</span>
            <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted p-2 font-mono text-xs">{proxmoxCommand(host, s.path, `nas-${s.name}`)}</pre>
          </li>
        ))}
      </ul>
    </Widget>
  );
}

function StatusPanel({ running }: { running: boolean }) {
  const [st, setSt] = useState<NfsStatus | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const loading = useRef(false);
  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try { setSt(await api<NfsStatus>('/services/nfs/status')); setErr(null); }
    catch (e) { setErr(e instanceof ApiError && e.status === 403 ? t.status.adminOnly : `${t.status.loadError} ${apiMessage(e)}`); }
    finally { loading.current = false; }
  }, []);
  useVisiblePoll(() => { if (running) void load(); }, STATUS_POLL_MS);
  useEffect(() => { if (running) void load(); }, [running, load]);
  const s = t.status;
  return (
    <Widget id="nfs-status" title={s.title} icon={<Users />}
      right={running ? <Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}>
        <RefreshCw className="h-4 w-4" aria-hidden />
      </Button> : undefined}>
      {!running ? <p className="text-sm text-muted-foreground">{s.off}</p> : err ? <p role="alert" className="text-sm text-danger">{err}</p>
        : !st ? <Skeleton className="h-16 w-full" /> : st.clients.length === 0 ? <p className="text-sm text-muted-foreground">{s.none}</p> : (
          <ul className="divide-y divide-border text-sm">
            {st.clients.map((c) => (
              <li key={`${c.address}/${c.name}`} className="grid grid-cols-2 gap-x-4 gap-y-1 py-2 sm:grid-cols-3">
                <span className="break-all"><span className="block text-xs text-muted-foreground">{s.address}</span><span className="font-medium">{c.address}</span></span>
                <span className="break-words"><span className="block text-xs text-muted-foreground">{s.name}</span>{c.name || '–'}</span>
                <span><span className="block text-xs text-muted-foreground">{s.version}</span>NFS {c.version || '4'}
                  {!c.confirmed && <span className="block text-xs text-warning">{s.pending}</span>}</span>
              </li>
            ))}
          </ul>
        )}
    </Widget>
  );
}

export function NfsPanel({ readOnly }: { readOnly: boolean }) {
  const [svc, setSvc] = useState<NfsService | null>(null);
  const [failed, setFailed] = useState(false);
  const load = useCallback(async () => {
    try { setSvc(await api<NfsService>('/services/nfs')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  if (!svc) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }
  const h = nfsHealth(svc);
  return (
    <div className="space-y-4">
      <Widget id="nfs-settings" title={t.title} icon={<Server />}
        right={<Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => void load()}>
          <RefreshCw className="h-4 w-4" aria-hidden />
        </Button>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        <p role="status" className={`mt-2 text-sm font-medium ${healthTone[h] ?? ''}`}>{t.health[h]}</p>
        {svc.missing.map((n) => <p key={n} role="note" className="mt-1 text-sm text-warning">{t.missing(n)}</p>)}
        <div className="mt-4"><SettingsForm svc={svc} readOnly={readOnly} onSaved={setSvc} /></div>
      </Widget>
      <ProxmoxHelp svc={svc} />
      {!readOnly && <StatusPanel running={svc.running} />}
    </div>
  );
}
