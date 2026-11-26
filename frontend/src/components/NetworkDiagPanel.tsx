import { Activity, CheckCircle2, XCircle } from 'lucide-react';
import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { DiagResult, DiagTest } from '../api/types';
import { cs } from '../i18n/cs';
import { DASH } from '../lib/format';
import { diagTargetError, listenScope } from '../lib/netdiag';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';

const t = cs.netDiag;
const QUICK: DiagTest[] = ['gateway', 'internet', 'ports', 'connections'];
const select = 'h-10 rounded-md border border-border bg-background px-3 text-sm';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 409, 429, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

const ms = (v: number | null) => (v == null ? DASH : v < 10 ? v.toFixed(2) : v.toFixed(0));

function heading(r: DiagResult): string {
  switch (r.test) {
    case 'ping': return t.head.ping(r.target ?? '');
    case 'dns': return t.head.dns(r.target ?? '');
    default: return t.head[r.test];
  }
}

function Status({ ok }: { ok: boolean }) {
  return ok
    ? <span className="inline-flex items-center gap-1 text-success"><CheckCircle2 className="h-4 w-4" aria-hidden />{t.ok}</span>
    : <span className="inline-flex items-center gap-1 text-danger"><XCircle className="h-4 w-4" aria-hidden />{t.failed}</span>;
}

const th = 'py-1 pr-3 font-medium';
const td = 'py-1 pr-3';

function ResultView({ r }: { r: DiagResult }) {
  const hasTable = r.test === 'ports' || r.test === 'connections';
  return (
    <div className="space-y-2 rounded-md border border-border p-3 text-sm" aria-live="polite">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="font-medium">{heading(r)}</span>
        {!hasTable && <Status ok={r.ok} />}
        <span className="text-xs text-muted-foreground">{t.took(r.durationMs)}</span>
      </div>
      {r.message && <p className={r.ok ? 'text-muted-foreground' : 'text-danger'}>{r.message}</p>}
      {r.ping && (
        <div>
          {r.ping.address && <div className="font-mono text-xs text-muted-foreground">{t.address}: {r.ping.address}</div>}
          <div>{t.pingSummary(r.ping.received, r.ping.sent, Math.round(r.ping.lossPercent))}</div>
          {r.ping.avgMs != null && <div>{t.rtt(ms(r.ping.minMs), ms(r.ping.avgMs), ms(r.ping.maxMs))}</div>}
        </div>
      )}
      {r.test === 'dns' && r.addresses && (
        <div><span className="text-muted-foreground">{t.addresses}: </span>
          <span className="break-all font-mono">{r.addresses.join(', ')}</span></div>
      )}
      {r.steps && (
        <ul className="space-y-1">
          {r.steps.map((s) => (
            <li key={s.name} className="flex flex-wrap items-center gap-x-3">
              <Status ok={s.ok} /><span>{t.step[s.name]}</span>
              <span className="break-all font-mono text-xs text-muted-foreground">{s.detail}</span>
              <span className="text-xs text-muted-foreground">{t.took(s.durationMs)}</span>
            </li>
          ))}
        </ul>
      )}
      {r.ports && (r.ports.length === 0 ? <p className="text-muted-foreground">{t.noPorts}</p> : (
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead className="text-xs text-muted-foreground"><tr>
              <th className={th}>{t.port}</th><th className={th}>{t.proto}</th><th className={th}>{t.listen}</th><th className={th}>{t.process}</th>
            </tr></thead>
            <tbody>
              {r.ports.map((p) => {
                const scope = listenScope(p.address);
                return (
                  <tr key={`${p.proto} ${p.address} ${p.port}`} className="border-t border-border">
                    <td className={`${td} font-mono`}>{p.port}</td><td className={`${td} uppercase`}>{p.proto}</td>
                    <td className={td}>{scope === 'all' ? t.allIfaces : scope === 'local' ? t.localOnly : <span className="font-mono">{p.address}</span>}</td>
                    <td className={`${td} font-mono`}>{p.process || <span className="font-sans text-muted-foreground">{t.kernel}</span>}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ))}
      {r.connections && (r.connections.length === 0 ? <p className="text-muted-foreground">{t.noConns}</p> : (
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead className="text-xs text-muted-foreground"><tr>
              <th className={th}>{t.localPort}</th><th className={th}>{t.peer}</th><th className={th}>{t.process}</th>
            </tr></thead>
            <tbody>
              {r.connections.map((c) => (
                <tr key={`${c.localPort} ${c.peer} ${c.peerPort}`} className="border-t border-border">
                  <td className={`${td} font-mono`}>{c.localPort}</td>
                  <td className={`${td} break-all font-mono`}>{c.peer}:{c.peerPort}</td>
                  <td className={`${td} font-mono`}>{c.process || <span className="font-sans text-muted-foreground">{t.kernel}</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {r.truncated && <p className="text-xs text-muted-foreground">{t.truncated(r.connections.length)}</p>}
        </div>
      ))}
    </div>
  );
}

/** NET-11: fixed network tests run on the NAS (administrators only). */
export function NetworkDiagPanel() {
  const [kind, setKind] = useState<'ping' | 'dns'>('ping');
  const [target, setTarget] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const [busy, setBusy] = useState<DiagTest | null>(null);
  const [result, setResult] = useState<DiagResult | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [fieldErr, setFieldErr] = useState<string | null>(null);

  const code = diagTargetError(kind, target);
  const shownErr = fieldErr ?? (submitted && code ? (code === 'dnsNeedsName' ? t.dnsNeedsName : t.targetErr) : null);

  async function run(test: DiagTest, tgt = '') {
    setBusy(test); setErr(null); setFieldErr(null);
    try {
      setResult(await api<DiagResult>('/network/diagnostics', { method: 'POST', body: { test, target: tgt } }));
    } catch (e) {
      if (e instanceof ApiError && e.status === 400 && e.field === 'target') setFieldErr(e.message);
      else setErr(apiMessage(e));
    } finally { setBusy(null); }
  }

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitted(true);
    if (!code) void run(kind, target.trim());
  }

  return (
    <Widget id="net-diag" title={t.title} icon={<Activity />} className="lg:col-span-5">
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{t.intro}</p>
        <div className="flex flex-wrap gap-2">
          {QUICK.map((q) => (
            <Button key={q} type="button" variant="outline" disabled={busy != null} onClick={() => void run(q)}>
              {busy === q ? t.running : t.quick[q]}
            </Button>
          ))}
        </div>
        <form onSubmit={submit} noValidate className="flex flex-wrap items-start gap-3">
          <label className="flex flex-col gap-1 text-sm font-medium">{t.test}
            <select className={select} value={kind} disabled={busy != null}
              onChange={(e) => { setKind(e.target.value as 'ping' | 'dns'); setFieldErr(null); }}>
              <option value="ping">{t.kinds.ping}</option><option value="dns">{t.kinds.dns}</option>
            </select>
          </label>
          <div className="min-w-0 flex-1 basis-56">
            <Field label={t.target} hint={t.targetHint[kind]} error={shownErr}>
              {(p) => <Input {...p} className={invalidCls} value={target} maxLength={253} autoComplete="off" autoCapitalize="none"
                spellCheck={false} disabled={busy != null} onChange={(e) => { setTarget(e.target.value); setFieldErr(null); }} />}
            </Field>
          </div>
          <Button type="submit" className="sm:mt-6" disabled={busy != null}>{busy === kind ? t.running : t.run}</Button>
        </form>
        {err && <p role="alert" className="rounded-md border border-danger/40 p-2 text-sm text-danger">{err}</p>}
        {result && <ResultView r={result} />}
      </div>
    </Widget>
  );
}
