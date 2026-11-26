import { Network, Settings2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { NetConfig, NetworkSettings } from '../api/types';
import type { useNetworkSettings } from '../hooks/useNetworkSettings';
import { cs } from '../i18n/cs';
import { parsePrefix, prefixToMask, validateDns, validateStatic, type FieldErrors } from '../lib/netvalidate';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field } from './ui/field';
import { Input } from './ui/input';
import { ListEditor } from './ui/list-editor';
import { NetworkDiagPanel } from './NetworkDiagPanel';
import { NetworkInfoPanel } from './NetworkInfoPanel';
import { Skeleton } from './ui/skeleton';

const t = cs.net;
const dash = '–';
const radio = 'flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-2 text-sm has-[:checked]:border-primary has-[:checked]:bg-primary/10';

interface Form { mode: 'dhcp' | 'static'; address: string; prefix: string; gateway: string; dns: string[]; useDhcpDns: boolean }

const fromConfig = (c: NetConfig): Form => ({
  mode: c.mode, address: c.address ?? '', prefix: c.prefixLength != null ? String(c.prefixLength) : '24', gateway: c.gateway ?? '',
  dns: c.dns, useDhcpDns: c.useDhcpDns,
});

function toConfig(f: Form): NetConfig {
  const dns = f.dns.map((s) => s.trim()).filter(Boolean);
  if (f.mode === 'dhcp') return { mode: 'dhcp', address: null, prefixLength: null, gateway: null, dns: f.useDhcpDns ? [] : dns, useDhcpDns: f.useDhcpDns };
  return { mode: 'static', address: f.address.trim(), prefixLength: parsePrefix(f.prefix), gateway: f.gateway.trim(), dns, useDhcpDns: false };
}

function dnsMsg(code: string): string {
  return code === 'invalid' ? t.err.dnsInvalid! : (t.err[code] ?? code);
}

const List = ({ items }: { items: string[] }) =>
  items.length ? <>{items.map((x) => <div key={x} className="font-mono">{x}</div>)}</> : <span className="text-muted-foreground">{dash}</span>;

export function NetworkPage({ net, readOnly }: { net: ReturnType<typeof useNetworkSettings>; readOnly: boolean }) {
  const { data, error, setData } = net;
  const [form, setForm] = useState<Form | null>(null);
  const [server, setServer] = useState<{ field: string; message: string } | null>(null);
  const [general, setGeneral] = useState<string | null>(null);
  const [submitted, setSubmitted] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);

  // (Re)load the form whenever the stored configuration changes (first load, confirm, rollback).
  const cfgKey = data ? JSON.stringify(data.config) : null;
  useEffect(() => {
    if (data) { setForm(fromConfig(data.config)); setServer(null); setSubmitted(false); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfgKey]);
  useEffect(() => { if (!data?.pending) setSent(false); }, [data?.pending]);

  const errors: FieldErrors = useMemo(() => {
    if (!form) return {};
    if (form.mode === 'static') return validateStatic({ address: form.address, prefix: form.prefix, gateway: form.gateway, dns: form.dns });
    const d = validateDns(form.dns, !form.useDhcpDns);
    return d ? { dns: d } : {};
  }, [form]);

  if (!data || !form) {
    return error ? (
      <div role="alert" className="glass rounded-lg border-danger/40 p-3 text-sm text-danger">
        {error.status === 503 ? t.helper : t.loadError}
      </div>
    ) : (
      <div className="grid gap-4 lg:grid-cols-5" aria-busy="true" aria-label={cs.common.loading}>
        <Skeleton className="h-48 rounded-xl lg:col-span-2" /><Skeleton className="h-80 rounded-xl lg:col-span-3" />
      </div>
    );
  }

  const set = (p: Partial<Form>) => { setForm({ ...form, ...p }); setServer(null); setGeneral(null); };
  const fe = (f: keyof FieldErrors, show = submitted): string | null => {
    if (server?.field === f) return server.message;
    const c = errors[f];
    if (!c || !show) return null;
    return f === 'dns' ? dnsMsg(c) : (t.err[c] ?? c);
  };
  const locked = data.pending != null;
  const prefixNum = parsePrefix(form.prefix);
  const maskHint = prefixNum != null && prefixNum >= 8 && prefixNum <= 30 ? t.prefixHint(prefixToMask(prefixNum)) : t.prefixHintBad;
  const cfg = toConfig(form);
  const unchanged = JSON.stringify(cfg) === JSON.stringify(data.config);
  const hasErrors = Object.keys(errors).length > 0;

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitted(true);
    if (hasErrors || locked) return;
    setConfirm(true);
  }

  async function apply() {
    setBusy(true); setGeneral(null);
    try {
      const d = await api<NetworkSettings>('/settings/network', { method: 'PUT', body: cfg });
      setData(d); setSent(true); setConfirm(false); setServer(null);
    } catch (e) {
      setConfirm(false);
      if (e instanceof ApiError && e.status === 400) {
        const f = e.field;
        if (f) setServer({ field: f === 'dnsServers' ? 'dns' : f, message: e.message }); else setGeneral(e.message);
      } else if (e instanceof ApiError && e.status === 409) { setGeneral(cs.errors.conflictPending); void net.reload(); }
      else if (e instanceof ApiError && e.status === 503) setGeneral(cs.errors.helperDown);
      else if (e instanceof ApiError && e.status === 0) setGeneral(cs.errors.network);
      else setGeneral(cs.errors.generic);
    } finally { setBusy(false); }
  }

  return (
    <div className="grid items-start gap-4 lg:grid-cols-5">
      <NetworkInfoPanel readOnly={readOnly} />
      <Widget id="net-current" title={t.current} icon={<Network />} className="lg:col-span-2"
        right={<Badge variant="neutral" className="font-mono">{data.interface}</Badge>}>
        <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 text-sm">
          <dt className="text-muted-foreground">{t.addresses}</dt><dd><List items={data.current.addresses} /></dd>
          <dt className="text-muted-foreground">{t.gateway}</dt><dd className="font-mono">{data.current.gateway ?? dash}</dd>
          <dt className="text-muted-foreground">{t.dns}</dt><dd><List items={data.current.dns} /></dd>
        </dl>
      </Widget>

      <Widget id="net-settings" title={t.settings} icon={<Settings2 />} className="lg:col-span-3">
        <form onSubmit={submit} noValidate className="space-y-4">
          {sent && locked && <p role="status" className="rounded-md bg-primary/10 p-2 text-sm">{t.sent}</p>}
          {locked && <p className="text-sm text-warning">{t.pendingLock}</p>}
          {general && <p role="alert" className="rounded-md border border-danger/40 p-2 text-sm font-medium text-danger">{general}</p>}

          <fieldset className="space-y-2" disabled={locked}>
            <legend className="mb-1 text-sm font-medium">{t.mode}</legend>
            <div className="grid gap-2 sm:grid-cols-2">
              <label className={radio}><input type="radio" name="mode" checked={form.mode === 'dhcp'} onChange={() => set({ mode: 'dhcp' })} />{t.dhcp}</label>
              <label className={radio}><input type="radio" name="mode" checked={form.mode === 'static'} onChange={() => set({ mode: 'static', dns: form.dns.length ? form.dns : [''] })} />{t.static}</label>
            </div>
          </fieldset>

          <fieldset className="space-y-4" disabled={locked}>
            {form.mode === 'static' && (
              <>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t.address} error={fe('address')}>
                    {(p) => <Input {...p} value={form.address} inputMode="decimal" autoComplete="off" placeholder="192.168.1.51"
                      className="aria-[invalid=true]:border-danger" onChange={(e) => set({ address: e.target.value })} />}
                  </Field>
                  <Field label={t.prefix} hint={maskHint} error={fe('prefixLength')}>
                    {(p) => <Input {...p} value={form.prefix} inputMode="numeric" autoComplete="off" placeholder="24"
                      className="aria-[invalid=true]:border-danger" onChange={(e) => set({ prefix: e.target.value })} />}
                  </Field>
                </div>
                <Field label={t.gatewayLabel} error={fe('gateway')}>
                  {(p) => <Input {...p} value={form.gateway} inputMode="decimal" autoComplete="off" placeholder="192.168.1.1"
                    className="aria-[invalid=true]:border-danger" onChange={(e) => set({ gateway: e.target.value })} />}
                </Field>
              </>
            )}
            {form.mode === 'dhcp' && (
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={form.useDhcpDns} onChange={(e) => set({ useDhcpDns: e.target.checked })} />{t.useDhcpDns}
              </label>
            )}
            {(form.mode === 'static' || !form.useDhcpDns) && (
              <ListEditor legend={t.dnsServers} hint={t.dnsHint} error={fe('dns')} values={form.dns} max={3}
                onChange={(dns) => set({ dns })} itemLabel={t.dnsItem} addLabel={t.addDns} placeholder="192.168.1.1" />
            )}
          </fieldset>

          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={locked || busy || unchanged}>{busy ? t.applying : t.apply}</Button>
            {!locked && unchanged && <span className="text-xs text-muted-foreground">{t.unchanged}</span>}
            {!unchanged && !locked && (
              <Button type="button" variant="ghost" onClick={() => { setForm(fromConfig(data.config)); setServer(null); setSubmitted(false); setGeneral(null); }}>
                {cs.common.reset}
              </Button>
            )}
          </div>
        </form>
      </Widget>

      {!readOnly && <NetworkDiagPanel />}

      {confirm && (
        <Dialog title={t.confirmTitle} onClose={() => setConfirm(false)} busy={busy}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setConfirm(false)} disabled={busy}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void apply()} disabled={busy}>{busy ? t.applying : t.apply}</Button>
          </>}>
          <p className="font-medium">{t.confirmWarn}</p>
          <div className="rounded-md bg-muted p-2">
            <div className="text-xs text-muted-foreground">{t.summary}</div>
            {cfg.mode === 'static'
              ? <div className="font-mono">{cfg.address}/{cfg.prefixLength} → {cfg.gateway}</div>
              : <div>{t.dhcp}</div>}
            <div className="font-mono text-xs">{cfg.mode === 'dhcp' && cfg.useDhcpDns ? t.useDhcpDns : cfg.dns.join(', ')}</div>
          </div>
          {cfg.mode === 'dhcp' ? <p>{t.confirmDhcp}</p>
            : cfg.address !== (data.config.address ?? '') || data.config.mode !== 'static'
              ? <p>{t.confirmAddr(cfg.address ?? '')}</p> : <p>{t.confirmSame}</p>}
        </Dialog>
      )}
    </div>
  );
}
