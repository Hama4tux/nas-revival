import { AlertTriangle, ArrowDown, ArrowUp, Flame, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { FirewallRule, FirewallSettings, FirewallState } from '../api/types';
import { formatCountdown, useCountdown } from '../hooks/useCountdown';
import { cs } from '../i18n/cs';
import {
  FW_MAX_ENTRIES, FW_MAX_RULES, FW_NOTE_MAX, FW_SERVICES, allowsClient, cleanList, cleanRule, invalidEntries, isAnywhere, lockoutDetail, parseCidr, ruleErrors, sameFirewall, type FwService,
} from '../lib/firewall';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.fw;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 409) return t.pendingOther;
    if ([400, 404, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const clone = (s: FirewallSettings): FirewallSettings =>
  ({ enabled: s.enabled, allow: { web: [...s.allow.web], ssh: [...s.allow.ssh], smb: [...s.allow.smb], nfs: [...s.allow.nfs] },
    rules: s.rules.map((r) => ({ ...r })) });

export function FirewallPendingBox({ deadline, onConfirmed, onExpired }: { deadline: string; onConfirmed: (s: FirewallState) => void; onExpired: () => void }) {
  const left = useCountdown(deadline);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const expired = left === 0;
  useEffect(() => {
    if (!expired) return;
    const id = setTimeout(onExpired, 2000);
    return () => clearTimeout(id);
  }, [expired, onExpired]);

  async function confirm() {
    setBusy(true); setErr(null);
    try { onConfirmed(await api<FirewallState>('/security/firewall/confirm', { method: 'POST' })); }
    catch (e) {
      if (e instanceof ApiError && e.status === 409) onExpired();
      else setErr(apiMessage(e));
    } finally { setBusy(false); }
  }

  return (
    <section aria-labelledby="fw-pending" className="mb-4 rounded-lg border-2 border-warning/60 bg-warning/10 p-4">
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning" aria-hidden />
        <div className="min-w-0 flex-1 space-y-2 text-sm">
          <h3 id="fw-pending" className="font-semibold">{t.pendingTitle}</h3>
          <p>{expired ? t.expired : t.pendingBody(left == null ? '–' : formatCountdown(left))}</p>
          <p className="text-muted-foreground">{t.pendingTest}</p>
          <p aria-live="off" className="text-2xl font-semibold tabular-nums">{left == null ? '–' : formatCountdown(left)}</p>
          <div role="alert">{err && <p className="font-medium text-danger">{err}</p>}</div>
          <Button type="button" disabled={busy || expired} onClick={() => void confirm()}>{busy ? t.confirming : t.confirm}</Button>
        </div>
      </div>
    </section>
  );
}

function AddressList({ label, list, lan, client, enabled, error, hid, onChange }: {
  label: string; list: string[]; lan: string | null; client: string; enabled: boolean; error: boolean; hid: string;
  onChange: (v: string[]) => void;
}) {
  const clean = cleanList(list);
  const hasLan = lan != null && clean.includes(lan);
  return (
    <>
      {list.map((v, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input value={v} placeholder={t.placeholder} autoComplete="off" spellCheck={false} inputMode="decimal"
            aria-label={t.item(label, i + 1)} aria-invalid={error && (v.trim() === '' || parseCidr(v) == null)}
            className="aria-[invalid=true]:border-danger"
            onChange={(e) => onChange(list.map((x, j) => (j === i ? e.target.value : x)))} />
          <Button type="button" variant="outline" size="icon" aria-label={`${cs.common.remove}: ${t.item(label, i + 1)}`}
            onClick={() => onChange(list.filter((_, j) => j !== i))}>
            <Trash2 className="h-4 w-4" aria-hidden />
          </Button>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        {list.length < FW_MAX_ENTRIES && (
          <Button type="button" variant="outline" size="sm" onClick={() => onChange([...list, ''])}>
            <Plus className="h-4 w-4" aria-hidden />{t.add}
          </Button>
        )}
        {lan && (
          <Button type="button" variant="outline" size="sm" disabled={hasLan || list.length >= FW_MAX_ENTRIES}
            onClick={() => onChange([...list.filter((x) => x.trim() !== ''), lan])}>
            {t.lan(lan)}
          </Button>
        )}
      </div>
      <div id={hid} className="space-y-1 text-xs">
        <p className="text-muted-foreground">{t.yourPc(client || '?')}{client && clean.length > 0 && allowsClient(clean, client) ? ' ✓' : ''}</p>
        {clean.length === 0 && <p className="font-medium text-warning">{enabled ? t.blocked : `${t.blocked} ${t.off}`}</p>}
        {isAnywhere(clean) && <p className="font-medium text-warning">{t.anywhere}</p>}
      </div>
    </>
  );
}

function ServiceEditor({ k, ports, list, lan, client, enabled, locked, error, onChange }: {
  k: FwService; ports: string; list: string[]; lan: string | null; client: string; enabled: boolean; locked: boolean;
  error: string | null; onChange: (v: string[]) => void;
}) {
  const name = t.services[k] ?? k;
  const hid = `fw-${k}-h`, eid = `fw-${k}-e`;
  return (
    <fieldset disabled={locked} className="min-w-0 space-y-2 rounded-lg border border-border/70 p-3" aria-describedby={`${hid}${error ? ` ${eid}` : ''}`}>
      <legend className="px-1 text-sm font-semibold">{name}</legend>
      <p className="text-xs text-muted-foreground">{t.ports(ports)}</p>
      <AddressList label={name} list={list} lan={lan} client={client} enabled={enabled} error={!!error} hid={hid} onChange={onChange} />
      {error && <p id={eid} role="alert" className="text-xs font-medium text-danger">{error}</p>}
    </fieldset>
  );
}

interface RuleErr { source: string | null; ports: string | null; note: string | null; action: string | null }

const selectCls = 'h-10 w-full rounded-md border border-input bg-background px-3 text-sm';

function RuleRow({ idx, count, rule, locked, err, onChange, onMove, onRemove }: {
  idx: number; count: number; rule: FirewallRule; locked: boolean; err: RuleErr;
  onChange: (r: FirewallRule) => void; onMove: (d: -1 | 1) => void; onRemove: () => void;
}) {
  const id = `fw-rule-${idx}`, n = idx + 1;
  const lab = (what: string) => t.rl(n, what);
  const field = (key: string, label: string, e: string | null, node: React.ReactNode) => (
    <div className="space-y-1">
      <label htmlFor={`${id}-${key}`} className="text-xs font-medium">{label}</label>
      {node}
      {e && <p id={`${id}-${key}-e`} role="alert" className="text-xs font-medium text-danger">{e}</p>}
    </div>
  );
  const inp = (key: string, label: string, value: string, e: string | null, ph: string, set: (v: string) => void) =>
    field(key, label, e, (
      <Input id={`${id}-${key}`} value={value} placeholder={ph} autoComplete="off" spellCheck={false} aria-label={lab(label.toLowerCase())}
        aria-invalid={!!e} aria-describedby={e ? `${id}-${key}-e` : undefined} className="aria-[invalid=true]:border-danger"
        onChange={(ev) => set(ev.target.value)} />
    ));
  return (
    <li className="min-w-0 rounded-lg border border-border/70 p-3">
      <fieldset disabled={locked} className="min-w-0 space-y-2">
        <legend className="px-1 text-sm font-semibold">{t.rule(n)}</legend>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-[1.4fr_1fr_1fr_1fr_1.6fr]">
          {inp('s', t.colSource, rule.source, err.source, t.sourcePlaceholder, (v) => onChange({ ...rule, source: v }))}
          {inp('p', t.colNasPort, rule.ports, err.ports, t.portPlaceholder, (v) => onChange({ ...rule, ports: v }))}
          {field('t', t.colProto, null, (
            <select id={`${id}-t`} className={selectCls} aria-label={lab(t.colProto.toLowerCase())} value={rule.proto}
              onChange={(e) => onChange({ ...rule, proto: e.target.value as FirewallRule['proto'] })}>
              {(['tcp', 'udp', 'both'] as const).map((p) => <option key={p} value={p}>{t.protos[p]}</option>)}
            </select>
          ))}
          {field('a', t.colAction, err.action, (
            <select id={`${id}-a`} className={selectCls} aria-label={lab(t.colAction.toLowerCase())} value={rule.action}
              aria-invalid={!!err.action} aria-describedby={err.action ? `${id}-a-e` : undefined}
              onChange={(e) => onChange({ ...rule, action: e.target.value as FirewallRule['action'] })}>
              {(['accept', 'drop'] as const).map((a) => <option key={a} value={a}>{t.actions[a]}</option>)}
            </select>
          ))}
          {inp('n', t.colNote, rule.note, err.note, '', (v) => onChange({ ...rule, note: v }))}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="icon" disabled={idx === 0} aria-label={lab(t.up)} onClick={() => onMove(-1)}>
            <ArrowUp className="h-4 w-4" aria-hidden />
          </Button>
          <Button type="button" variant="outline" size="icon" disabled={idx === count - 1} aria-label={lab(t.down)} onClick={() => onMove(1)}>
            <ArrowDown className="h-4 w-4" aria-hidden />
          </Button>
          <Button type="button" variant="outline" size="icon" aria-label={lab(t.removeRule.toLowerCase())} onClick={onRemove}>
            <Trash2 className="h-4 w-4" aria-hidden />
          </Button>
        </div>
      </fieldset>
    </li>
  );
}

export function FirewallPanel({ readOnly }: { readOnly: boolean }) {
  const [state, setState] = useState<FirewallState | null>(null);
  const [form, setForm] = useState<FirewallSettings | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [fieldErr, setFieldErr] = useState<{ field: string; text: string } | null>(null);
  const [showLocal, setShowLocal] = useState(false);
  const [expertOn, setExpertOn] = useState(() => { try { return localStorage.getItem('fw-expert') === '1'; } catch { return false; } });
  const setExpert = (v: boolean) => { setExpertOn(v); try { localStorage.setItem('fw-expert', v ? '1' : '0'); } catch { /* convenience only */ } };

  const load = useCallback(async () => {
    try {
      const s = await api<FirewallState>('/security/firewall');
      setState(s); setForm(clone(s.pending?.next ?? s.settings)); setFailed(false);
      if ((s.pending?.next ?? s.settings).rules.length > 0) setExpertOn(true);
    } catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const onExpired = useCallback(() => { setMsg({ ok: false, text: t.reverted }); void load(); }, [load]);
  const onConfirmed = useCallback((s: FirewallState) => {
    setState(s); setForm(clone(s.settings)); setMsg({ ok: true, text: t.confirmed });
  }, []);

  if (!state || !form) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }

  const refresh = (
    <Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => { setMsg(null); void load(); }}>
      <RefreshCw className="h-4 w-4" aria-hidden />
    </Button>
  );
  if (!state.supported) {
    return (
      <Widget id="sec-firewall" title={t.title} icon={<Flame />} right={refresh}>
        <p className="text-sm">{t.unsupported}</p>
      </Widget>
    );
  }

  const locked = readOnly || state.pending != null;
  const changed = !sameFirewall(form, state.pending?.next ?? state.settings);
  const portsOf = (k: FwService) => (state.services.find((s) => s.key === k)?.ports ?? []).map((p) => `${p.port}/${p.proto.toUpperCase()}`).join(', ') || '–';

  // Client-side checks mirror the server: syntax, entry limit and the anti-lockout rule.
  const localErr = (k: FwService): string | null => {
    const l = cleanList(form.allow[k]);
    const bad = invalidEntries(l)[0];
    if (bad != null) return t.syntax(bad);
    if (l.length > FW_MAX_ENTRIES) return t.tooMany(FW_MAX_ENTRIES);
    return null;
  };
  const lockout = lockoutDetail(form, state.clientAddress);
  const lockSvc = lockout.filter((d) => d.rule == null).map((d) => d.service);
  const errFor = (k: FwService): string | null =>
    (showLocal && (localErr(k) ?? (lockSvc.includes(k) ? t.lockout(t.lockoutService[k] ?? k) : null))) ||
    (fieldErr?.field === `allow.${k}` ? fieldErr.text : null);
  const ruleErr = (i: number): RuleErr => {
    const r = form.rules[i]!;
    const le = showLocal ? ruleErrors(r) : {};
    const fe = (f: string) => (fieldErr?.field === `rules.${i}.${f}` ? fieldErr.text : null);
    return {
      source: (le.source ? t.sourceErr : null) ?? fe('source'),
      ports: (le.ports ? t.portsErr : null) ?? fe('ports'),
      note: (le.note ? t.noteErr(FW_NOTE_MAX) : null) ?? fe('note'),
      action: (showLocal && lockout.some((d) => d.rule === i) ? t.ruleLockout : null) ?? fe('action') ?? fe('proto'),
    };
  };
  const anyLocalErr = FW_SERVICES.some((k) => localErr(k) != null) || lockout.length > 0 ||
    form.rules.some((r) => Object.keys(ruleErrors(r)).length > 0);

  const edit = (p: Partial<FirewallSettings>) => { setForm({ ...form, ...p }); setMsg(null); setFieldErr(null); setShowLocal(false); };

  const addRule = (r: FirewallRule) => {
    if (form.rules.length >= FW_MAX_RULES) { setMsg({ ok: false, text: t.maxRules(FW_MAX_RULES) }); return; }
    edit({ rules: [...form.rules, r] });
    setExpert(true);
    setMsg({ ok: true, text: t.ruleAdded });
  };

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!form || locked) return;
    if (anyLocalErr) { setShowLocal(true); return; }
    const body: FirewallSettings = { enabled: form.enabled, allow: {
      web: cleanList(form.allow.web), ssh: cleanList(form.allow.ssh), smb: cleanList(form.allow.smb), nfs: cleanList(form.allow.nfs) },
      rules: form.rules.map(cleanRule) };
    setBusy(true); setMsg(null); setFieldErr(null);
    try {
      const s = await api<FirewallState>('/security/firewall', { method: 'PUT', body });
      setState(s); setForm(clone(s.pending?.next ?? s.settings)); setMsg({ ok: true, text: t.saved });
    } catch (ex) {
      if (ex instanceof ApiError && ex.status === 400 && ex.field) setFieldErr({ field: ex.field, text: ex.message });
      else { setMsg({ ok: false, text: apiMessage(ex) }); if (ex instanceof ApiError && ex.status === 409) void load(); }
    } finally { setBusy(false); }
  }

  return (
    <Widget id="sec-firewall" title={t.title} icon={<Flame />} right={refresh}>
      <p className="text-sm text-muted-foreground">{t.hint}</p>
      <p role="status" className="mt-2 text-sm font-medium">
        <span className={state.settings.enabled ? 'text-success' : 'text-warning'}>{state.settings.enabled ? t.statusOn : t.statusOff}</span>
        {' · '}<span className={state.active ? 'text-success' : 'text-muted-foreground'}>{state.active ? t.rulesActive : t.rulesInactive}</span>
      </p>
      {state.pending && (
        <div className="mt-4">
          <FirewallPendingBox deadline={state.pending.deadline} onConfirmed={onConfirmed} onExpired={onExpired} />
        </div>
      )}
      <form onSubmit={(e) => void save(e)} className="mt-4 space-y-4" noValidate>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-1 h-4 w-4" checked={form.enabled} disabled={locked}
            onChange={(e) => edit({ enabled: e.target.checked })} />
          <span><span className="font-medium">{t.enable}</span><span className="block text-xs text-muted-foreground">{t.enableHint}</span></span>
        </label>
        <div className="grid gap-4 md:grid-cols-2">
          {FW_SERVICES.map((k) => (
            <ServiceEditor key={k} k={k} ports={portsOf(k)} list={form.allow[k]} lan={state.lanNetwork} client={state.clientAddress}
              enabled={form.enabled} locked={locked} error={errFor(k)}
              onChange={(v) => edit({ allow: { ...form.allow, [k]: v } })} />
          ))}
        </div>
        <section aria-labelledby="fw-expert" className="space-y-3">
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" role="switch" className="mt-1 h-4 w-4" checked={expertOn}
              onChange={(e) => setExpert(e.target.checked)} />
            <span><span id="fw-expert" className="font-medium">{t.expert}</span></span>
          </label>
          {!expertOn && form.rules.length > 0 && <p className="text-sm">{t.expertActive(form.rules.length)}</p>}
          {expertOn && (
            <>
              <p className="text-xs text-muted-foreground">{t.expertHint}</p>
              {form.rules.length === 0 && <p className="text-sm text-muted-foreground">{t.rulesEmpty}</p>}
              <ol className="space-y-3">
                {form.rules.map((r, i) => (
                  <RuleRow key={i} idx={i} count={form.rules.length} rule={r} locked={locked} err={ruleErr(i)}
                    onChange={(nr) => edit({ rules: form.rules.map((x, j) => (j === i ? nr : x)) })}
                    onMove={(d) => {
                      const a = [...form.rules]; const o = a[i + d]!; a[i + d] = a[i]!; a[i] = o; edit({ rules: a });
                    }}
                    onRemove={() => edit({ rules: form.rules.filter((_, j) => j !== i) })} />
                ))}
              </ol>
              {!locked && form.rules.length < FW_MAX_RULES && (
                <Button type="button" variant="outline" size="sm"
                  onClick={() => edit({ rules: [...form.rules, { source: '', ports: '', proto: 'both', action: 'accept', note: '' }] })}>
                  <Plus className="h-4 w-4" aria-hidden />{t.addRule}
                </Button>
              )}
            </>
          )}
        </section>
        <section aria-labelledby="fw-listen" className="space-y-2">
          <h3 id="fw-listen" className="text-sm font-semibold">{t.listeningTitle}</h3>
          <p className="text-xs text-muted-foreground">{t.listeningHint}</p>
          {state.listening.length === 0 ? <p className="text-sm text-muted-foreground">{t.listeningEmpty}</p> : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <caption className="sr-only">{t.listeningTitle}</caption>
                <thead className="text-xs text-muted-foreground">
                  <tr><th scope="col" className="py-1 pr-3 font-medium">{t.colPort}</th><th scope="col" className="py-1 pr-3 font-medium">{t.colProcess}</th><th scope="col" className="py-1 font-medium">{t.colState}</th></tr>
                </thead>
                <tbody>
                  {state.listening.map((l) => {
                    const pt = `${l.port}/${l.proto.toUpperCase()}`;
                    return (
                      <tr key={pt} className={`border-t border-border/50 ${l.allowed ? '' : 'bg-warning/10'}`}>
                        <td className="py-1.5 pr-3 tabular-nums">{pt}</td>
                        <td className="py-1.5 pr-3 break-all">{l.process}</td>
                        <td className="py-1.5">
                          {l.allowed ? <span className="text-success">{t.stateAllowed}</span> : (
                            <span className="flex flex-wrap items-center gap-2">
                              <span className="font-medium text-warning">{(state.pending?.next ?? state.settings).enabled ? t.stateBlocked : t.stateWouldBlock}</span>
                              {!locked && (
                                <Button type="button" variant="outline" size="sm" aria-label={t.allowLabel(pt, l.process)}
                                  onClick={() => addRule({ source: state.lanNetwork ?? '', ports: String(l.port), proto: l.proto, action: 'accept', note: l.process.slice(0, FW_NOTE_MAX) })}>
                                  {t.allow}
                                </Button>
                              )}
                            </span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>
        {readOnly ? <p className="text-sm text-muted-foreground">{t.readOnly}</p> : (
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={busy || locked || !changed}>{busy ? t.saving : t.save}</Button>
            <Button type="button" variant="outline" disabled={busy || locked || !changed}
              onClick={() => { setForm(clone(state.settings)); setMsg(null); setFieldErr(null); setShowLocal(false); }}>{t.reset}</Button>
            {!changed && !msg && !state.pending && <span className="text-sm text-muted-foreground">{t.unchanged}</span>}
          </div>
        )}
        {msg && <p role={msg.ok ? 'status' : 'alert'} className={`text-sm font-medium ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</p>}
      </form>
    </Widget>
  );
}
