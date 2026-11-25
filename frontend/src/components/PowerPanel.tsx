import { AlertTriangle, Moon, RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { DiskPower, DiskStandbyResult, PowerSettings } from '../api/types';
import { cs } from '../i18n/cs';
import { apmKind, changed, effective, formOf, needsConfirm, parseApm, standbyLabel, type PowerForm } from '../lib/power';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.power;
const radio = 'flex cursor-pointer flex-col gap-0.5 rounded-md border border-border px-3 py-2 text-sm has-[:checked]:border-primary has-[:checked]:bg-primary/10';
const stateVariant = { active: 'success', standby: 'neutral', sleeping: 'neutral', unknown: 'warning' } as const;

function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 404 || e.status === 409 || e.status === 500) return e.message;
  }
  return cs.errors.generic;
}

export function PowerPanel({ diskId }: { diskId: string }) {
  const [data, setData] = useState<DiskPower | null>(null);
  const [form, setForm] = useState<PowerForm | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sending, setSending] = useState(false);
  const [dialog, setDialog] = useState<'confirm' | 'standby' | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const path = `/storage/disks/${encodeURIComponent(diskId)}/power`;

  const load = useCallback(async (resetForm: boolean) => {
    setBusy(true);
    try {
      const v = await api<DiskPower>(path);
      setData(v); setFailed(false);
      if (resetForm) setForm(formOf(v));
    } catch { setFailed(true); } finally { setBusy(false); }
  }, [path]);
  useEffect(() => { void load(true); }, [load]);

  async function save(s: PowerSettings, confirmSystemDisk: boolean) {
    setSending(true); setMsg(null);
    try {
      const v = await api<DiskPower>(path, { method: 'PUT', body: { ...s, confirmSystemDisk } });
      setData(v); setForm(formOf(v)); setDialog(null);
      setMsg({ ok: true, text: t.saved });
    } catch (e) {
      if (e instanceof ApiError && e.code === 'confirm_required') { setDialog('confirm'); return; }
      setDialog(null); setMsg({ ok: false, text: errorText(e) });
    } finally { setSending(false); }
  }

  async function standby() {
    setSending(true); setMsg(null);
    try {
      const r = await api<DiskStandbyResult>(`${path.replace(/\/power$/, '')}/standby`, { method: 'POST' });
      setMsg({ ok: true, text: r.wasStandby ? t.standbyAlready : t.standbyDone });
      await load(false);
    } catch (e) {
      setMsg({ ok: false, text: errorText(e) });
    } finally { setSending(false); setDialog(null); }
  }

  if (!data || !form) {
    return failed
      ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-40 w-full" /></div>;
  }

  const s = effective(data, form);
  const apmBad = form.profile === 'custom' && parseApm(form.apm) === 'invalid';
  const dirty = changed(data, s);
  const asleep = data.state === 'standby' || data.state === 'sleeping';
  const kind = apmKind(data.apmLevel);
  const apmText = data.apmSupported === false ? t.apmUnsupported
    : data.apmLevel == null ? t.apmUnknown : `${data.apmLevel}${kind ? ` – ${t.apmKind[kind]}` : ''}`;

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!s || !dirty) { setMsg({ ok: true, text: t.unchanged }); return; }
    if (needsConfirm(data!, s)) setDialog('confirm'); else void save(s, false);
  }

  return (
    <section aria-label={t.title} className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
        <Badge variant={stateVariant[data.state]}>{t.state[data.state]}</Badge>
        <span>{t.apm}: <b>{apmText}</b></span>
        <span className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
          {t.checkedAt(new Date(data.checkedAt).toLocaleTimeString('cs-CZ'))}
          <Button type="button" size="sm" variant="outline" onClick={() => void load(false)} disabled={busy} aria-label={t.refresh}>
            <RefreshCw className="h-4 w-4" aria-hidden />
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => setDialog('standby')} disabled={asleep || sending}>
            <Moon className="h-4 w-4" aria-hidden />{t.standbyNow}
          </Button>
        </span>
      </div>

      {!data.stored && <p role="note" className="rounded-lg border border-border p-3 text-sm text-muted-foreground">{t.defaultNote}</p>}

      <form onSubmit={submit} className="space-y-4">
        <fieldset className="space-y-2" disabled={sending}>
          <legend className="mb-1 text-sm font-medium">{t.profile}</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {data.profiles.map((p) => (
              <label key={p.profile} className={radio}>
                <span className="flex items-center gap-2 font-medium">
                  <input type="radio" name={`profile-${diskId}`} checked={form.profile === p.profile}
                    onChange={() => setForm({ ...form, profile: p.profile })} />
                  {t.profiles[p.profile]}
                </span>
                <span className="text-xs text-muted-foreground">{t.profileValues(standbyLabel(p.standbyMinutes), p.apm)}</span>
              </label>
            ))}
            <label className={radio}>
              <span className="flex items-center gap-2 font-medium">
                <input type="radio" name={`profile-${diskId}`} checked={form.profile === 'custom'}
                  onChange={() => setForm({ ...form, profile: 'custom' })} />
                {t.profiles.custom}
              </span>
            </label>
          </div>
          {data.systemDisk && form.profile === 'balanced' && <p className="text-xs text-muted-foreground">{t.systemBalanced}</p>}
        </fieldset>

        {form.profile === 'custom' && (
          <fieldset className="grid gap-4 sm:grid-cols-2" disabled={sending}>
            <Field label={t.standbyLabel}>
              {(p) => (
                <select {...p} value={form.standbyMinutes} onChange={(e) => setForm({ ...form, standbyMinutes: Number(e.target.value) })}
                  className="h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm">
                  {data.standbyChoices.map((m) => <option key={m} value={m}>{standbyLabel(m)}</option>)}
                </select>
              )}
            </Field>
            <Field label={t.apmLabel} hint={t.apmHint} error={apmBad ? t.apmInvalid : null}>
              {(p) => (
                <Input {...p} inputMode="numeric" value={form.apm} className={invalidCls}
                  onChange={(e) => setForm({ ...form, apm: e.target.value })} />
              )}
            </Field>
          </fieldset>
        )}

        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={sending || apmBad || !dirty}>{sending && !dialog ? t.saving : t.save}</Button>
          {msg && <p role={msg.ok ? 'status' : 'alert'} className={`text-sm ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</p>}
        </div>
      </form>

      {dialog === 'confirm' && s && (
        <Dialog title={t.confirmTitle} onClose={() => setDialog(null)} busy={sending}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setDialog(null)} disabled={sending}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void save(s, true)} disabled={sending}>
              {sending ? t.saving : t.confirm}
            </Button>
          </>}>
          <p className="flex gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{t.confirmBody(standbyLabel(s.standbyMinutes))}</span>
          </p>
        </Dialog>
      )}
      {dialog === 'standby' && (
        <Dialog title={t.standbyTitle} onClose={() => setDialog(null)} busy={sending}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setDialog(null)} disabled={sending}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void standby()} disabled={sending}>
              {sending ? t.sending : t.standbyNow}
            </Button>
          </>}>
          <p>{t.standbyBody(data.systemDisk)}</p>
        </Dialog>
      )}
    </section>
  );
}
