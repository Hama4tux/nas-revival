import { Cable, Route as RouteIcon } from 'lucide-react';
import { useCallback, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { NetInterface, NetRoute, NetworkInfo, NetworkSettings } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { DASH, formatSpeed } from '../lib/format';
import { hostnameError, hostnameLong, normalizeHostname } from '../lib/hostname';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.netInfo;
const dl = 'grid grid-cols-[minmax(7rem,auto)_1fr] gap-x-4 gap-y-1 text-sm';
/** More carrier changes since boot than a plug-in or two suggests a bad cable or port. */
const FLAP_WARN = 10;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

function InterfaceRow({ it }: { it: NetInterface }) {
  const linked = it.state === 'up' && it.carrier !== false;
  const speed = it.speedMbps != null ? `${formatSpeed(it.speedMbps)}${it.duplex ? `, ${t.duplex[it.duplex]}` : ''}` : null;
  return (
    <div className="space-y-2 border-t border-border pt-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono font-medium">{it.name}</span>
        <Badge variant={linked ? 'success' : 'danger'}>{linked ? t.state.up : (t.state[it.state] ?? t.state.unknown)}</Badge>
      </div>
      <dl className={dl}>
        <dt className="text-muted-foreground">{t.speed}</dt><dd>{speed ?? (linked ? DASH : t.noCable)}</dd>
        <dt className="text-muted-foreground">{t.addresses}</dt>
        <dd className="min-w-0 break-all font-mono">{it.addresses.length ? it.addresses.map((a) => <div key={a}>{a}</div>) : DASH}</dd>
        <dt className="text-muted-foreground">{t.mac}</dt><dd className="font-mono">{it.mac || DASH}</dd>
        <dt className="text-muted-foreground">{t.mtu}</dt><dd>{it.mtu}</dd>
        {it.linkChanges != null && <><dt className="text-muted-foreground">{t.linkChanges}</dt><dd>{it.linkChanges}</dd></>}
      </dl>
      {it.duplex === 'half' && <p role="note" className="text-sm text-warning">{t.halfDuplex}</p>}
      {it.linkChanges != null && it.linkChanges > FLAP_WARN && <p role="note" className="text-sm text-warning">{t.flapping(it.linkChanges)}</p>}
    </div>
  );
}

function RoutesTable({ routes }: { routes: NetRoute[] }) {
  if (!routes.length) return <p className="text-sm text-muted-foreground">{t.noRoutes}</p>;
  const metric = routes.some((r) => r.metric != null);
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="text-xs text-muted-foreground">
          <tr><th className="py-1 pr-3 font-medium">{t.routeDest}</th><th className="py-1 pr-3 font-medium">{t.routeVia}</th>
            <th className="hidden py-1 pr-3 font-medium sm:table-cell">{t.routeDev}</th>
            <th className="hidden py-1 pr-3 font-medium sm:table-cell">{t.routeProto}</th>
            {metric && <th className="py-1 font-medium">{t.routeMetric}</th>}</tr>
        </thead>
        <tbody>
          {routes.map((r) => (
            <tr key={`${r.family} ${r.destination} ${r.device ?? ''} ${r.metric ?? ''}`} className="border-t border-border">
              <td className="py-1 pr-3 font-mono">{r.destination === 'default' ? <span className="font-sans font-medium">{t.defaultRoute}</span> : r.destination}</td>
              <td className="py-1 pr-3 font-mono">{r.gateway ?? <span className="font-sans text-muted-foreground">{t.direct}</span>}</td>
              <td className="hidden py-1 pr-3 font-mono sm:table-cell">{r.device ?? DASH}</td>
              <td className="hidden py-1 pr-3 sm:table-cell">{r.protocol ? (t.proto[r.protocol] ?? r.protocol) : DASH}</td>
              {metric && <td className="py-1">{r.metric ?? DASH}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RenameDialog({ current, onClose, onDone }: { current: string; onClose: () => void; onDone: (i: NetworkInfo) => void }) {
  const [name, setName] = useState(current);
  const [busy, setBusy] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const code = hostnameError(name);
  const same = normalizeHostname(name) === current;
  const fieldErr = submitted && code ? t.nameErr[code] : err;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSubmitted(true);
    if (code || same) return;
    setBusy(true); setErr(null);
    try {
      onDone(await api<NetworkInfo>('/network/hostname', { method: 'PUT', body: { hostname: normalizeHostname(name) } }));
    } catch (ex) { setErr(apiMessage(ex)); } finally { setBusy(false); }
  }

  return (
    <Dialog title={t.renameTitle} onClose={onClose} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="submit" form="rename-form" disabled={busy || same}>{busy ? t.renaming : t.renameSave}</Button>
      </>}>
      <form id="rename-form" onSubmit={(e) => void save(e)} noValidate className="space-y-3">
        <Field label={t.newName} hint={same ? t.sameName : t.nameHint} error={fieldErr}>
          {(p) => <Input {...p} data-autofocus className={invalidCls} value={name} maxLength={70} autoComplete="off"
            autoCapitalize="none" spellCheck={false} onChange={(e) => { setName(e.target.value); setErr(null); }} />}
        </Field>
        {!code && hostnameLong(name) && <p role="note" className="text-sm text-warning">{t.nameLong}</p>}
        <p className="text-sm text-muted-foreground">{t.renameEffects}</p>
      </form>
    </Dialog>
  );
}

/** NET-10: host name, interfaces with link details and the routing table. */
export function NetworkInfoPanel({ readOnly, current }: { readOnly: boolean; current: NetworkSettings['current'] }) {
  const [info, setInfo] = useState<NetworkInfo | null>(null);
  const [error, setError] = useState(false);
  const [rename, setRename] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const load = useCallback(async () => {
    try { setInfo(await api<NetworkInfo>('/network')); setError(false); } catch { setError(true); }
  }, []);
  // Link state changes rarely; 15 s keeps the CPU of the NAS free.
  useVisiblePoll(() => void load(), 15000);

  if (!info) {
    return error
      ? <div role="alert" className="glass rounded-lg border-danger/40 p-3 text-sm text-danger lg:col-span-2">{t.loadError}</div>
      : <Skeleton className="h-64 rounded-xl lg:col-span-2" />;
  }
  const hasDefault = info.routes.some((r) => r.destination === 'default');
  return (
    <div className="min-w-0 space-y-4 lg:col-span-2">
      <Widget id="net-info" title={t.title} icon={<Cable />}
        right={!readOnly && <Button type="button" variant="outline" size="sm" onClick={() => { setRename(true); setMsg(null); }}>{t.rename}</Button>}>
        <div className="space-y-3">
          {msg && <p role="status" className="rounded-md bg-primary/10 p-2 text-sm">{msg}</p>}
          {error && <p role="alert" className="text-sm text-danger">{t.loadError}</p>}
          <dl className={dl}>
            <dt className="text-muted-foreground">{t.hostname}</dt><dd className="font-mono font-medium">{info.hostname}</dd>
            <dt className="text-muted-foreground">{t.gateway}</dt><dd className="font-mono">{current.gateway ?? DASH}</dd>
            <dt className="text-muted-foreground">{t.dns}</dt>
            <dd className="font-mono">{current.dns.length ? current.dns.map((d) => <div key={d}>{d}</div>) : DASH}</dd>
            {info.domains.length > 0 && <><dt className="text-muted-foreground">{t.domains}</dt>
              <dd className="font-mono">{info.domains.join(', ')}</dd></>}
          </dl>
          {info.interfaces.length ? info.interfaces.map((it) => <InterfaceRow key={it.name} it={it} />)
            : <p className="text-sm text-warning">{t.noInterfaces}</p>}
        </div>
      </Widget>
      <Widget id="net-routes" title={t.routes} icon={<RouteIcon />}>
        <div className="space-y-2">
          {!hasDefault && <p role="note" className="text-sm text-warning">{t.noDefault}</p>}
          <RoutesTable routes={info.routes} />
        </div>
      </Widget>
      {rename && (
        <RenameDialog current={info.hostname} onClose={() => setRename(false)}
          onDone={(i) => { setInfo(i); setRename(false); setMsg(t.renamed(i.hostname)); }} />
      )}
    </div>
  );
}
