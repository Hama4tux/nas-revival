import { Clock, FileUp, RefreshCw, ShieldCheck } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { TimeSettings, TlsInfo } from '../api/types';
import { cs } from '../i18n/cs';
import { validateNtp } from '../lib/netvalidate';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field } from './ui/field';
import { ListEditor } from './ui/list-editor';
import { Skeleton } from './ui/skeleton';

const t = cs.sys;
const dash = '–';
const dt = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? dash : d.toLocaleString('cs-CZ');
};

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 409) return e.message;
  }
  return cs.errors.generic;
}

const LoadError = () => <p role="alert" className="text-sm text-danger">{t.loadError}</p>;

/* ---------- time and NTP ---------- */

function TimeWidget() {
  const [data, setData] = useState<TimeSettings | null>(null);
  const [failed, setFailed] = useState(false);
  const [list, setList] = useState<string[]>([]);
  const [offset, setOffset] = useState(0);
  const [tick, setTick] = useState(Date.now());
  const [submitted, setSubmitted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [serverErr, setServerErr] = useState<string | null>(null);
  const [ok, setOk] = useState(false);

  const load = useCallback((d: TimeSettings) => {
    setData(d); setList(d.defaultServers ? [] : d.ntpServers);
    setOffset(Date.parse(d.now) - Date.now());
  }, []);
  useEffect(() => {
    let alive = true;
    api<TimeSettings>('/settings/time').then((d) => { if (alive) load(d); }).catch(() => { if (alive) setFailed(true); });
    return () => { alive = false; };
  }, [load]);
  useEffect(() => { const id = setInterval(() => setTick(Date.now()), 1000); return () => clearInterval(id); }, []);

  if (!data) {
    return (
      <Widget id="sys-time" title={t.time} icon={<Clock />} className="lg:col-span-3">
        {failed ? <LoadError /> : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-32 w-full" /></div>}
      </Widget>
    );
  }

  const code = validateNtp(list);
  const msg = serverErr ?? (submitted && code ? t.ntpErr[code] ?? code : null);
  const changed = JSON.stringify(list) !== JSON.stringify(data.defaultServers ? [] : data.ntpServers);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSubmitted(true); setOk(false); setServerErr(null);
    if (code) return;
    setBusy(true);
    try {
      load(await api<TimeSettings>('/settings/time', { method: 'PUT', body: { ntpServers: list.map((s) => s.trim()) } }));
      setSubmitted(false); setOk(true);
    } catch (err) { setServerErr(apiMessage(err)); }
    finally { setBusy(false); }
  }

  return (
    <Widget id="sys-time" title={t.time} icon={<Clock />} className="lg:col-span-3"
      right={<Badge variant={data.synchronized ? 'success' : 'warning'}>{data.synchronized ? t.synced : t.notSynced}</Badge>}>
      <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-2 text-sm">
        <dt className="text-muted-foreground">{t.server}</dt>
        <dd className="break-all">{data.serverName ?? dash}{data.serverAddress && <span className="font-mono text-muted-foreground"> ({data.serverAddress})</span>}</dd>
        <dt className="text-muted-foreground">{t.timezone}</dt><dd>{data.timezone}</dd>
        <dt className="text-muted-foreground">{t.nasTime}</dt>
        <dd className="tabular-nums">{dt(new Date(tick + offset).toISOString())}</dd>
      </dl>
      <form onSubmit={(e) => void save(e)} noValidate className="mt-4 space-y-3 border-t border-border/70 pt-4">
        <ListEditor legend={t.ntpServers} hint={t.ntpHint}
          error={msg} values={list} max={4} itemLabel={t.ntpItem} addLabel={t.addNtp} placeholder="tik.cesnet.cz"
          onChange={(v) => { setList(v); setServerErr(null); setOk(false); }} />
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={busy || !changed}>{busy ? cs.common.saving : cs.common.save}</Button>
          <Button type="button" variant="outline" disabled={busy || list.length === 0}
            onClick={() => { setList([]); setServerErr(null); setOk(false); }}>{t.useDefaults}</Button>
          {ok && <span role="status" className="text-sm text-success">{t.savedTime}</span>}
        </div>
      </form>
    </Widget>
  );
}

/* ---------- certificate ---------- */

const MAX = 64 * 1024;
const looksPem = (s: string, kind: 'CERTIFICATE' | 'KEY') =>
  kind === 'CERTIFICATE' ? /-----BEGIN CERTIFICATE-----/.test(s) : /-----BEGIN [A-Z ]*PRIVATE KEY-----/.test(s);

function UploadDialog({ onClose, onDone }: { onClose: () => void; onDone: (i: TlsInfo) => void }) {
  const [cert, setCert] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<{ field: string; message: string } | null>(null);
  const [fileErr, setFileErr] = useState<string | null>(null);

  const close = () => { setKey(''); setCert(''); onClose(); };

  async function readFile(f: File | undefined, set: (s: string) => void) {
    setFileErr(null); setErr(null);
    if (!f) return;
    if (f.size > MAX) { setFileErr(t.tooBig); return; }
    try { set(await f.text()); } catch { setFileErr(t.fileRead); }
  }

  function local(field: 'certificate' | 'privateKey'): string | null {
    if (err?.field === field) return err.message;
    return null;
  }

  async function submit() {
    setErr(null); setFileErr(null);
    if (!cert.trim() || !key.trim()) { setFileErr(t.needBoth); return; }
    if (!looksPem(cert, 'CERTIFICATE')) { setErr({ field: 'certificate', message: t.badPem }); return; }
    if (!looksPem(key, 'KEY')) { setErr({ field: 'privateKey', message: t.badPem }); return; }
    if (cert.length > MAX || key.length > MAX) { setFileErr(t.tooBig); return; }
    setBusy(true);
    try {
      const info = await api<TlsInfo>('/settings/tls', { method: 'PUT', body: { certificate: cert, privateKey: key } });
      setKey(''); setCert('');
      onDone(info);
    } catch (e) {
      if (e instanceof ApiError && e.status === 400 && (e.field === 'certificate' || e.field === 'privateKey')) setErr({ field: e.field, message: e.message });
      else setFileErr(apiMessage(e));
    } finally { setBusy(false); }
  }

  const ta = 'min-h-24 w-full rounded-md border border-border bg-background/80 p-2 font-mono text-xs aria-[invalid=true]:border-danger';
  return (
    <Dialog title={t.uploadTitle} onClose={close} busy={busy}
      footer={<>
        <Button type="button" variant="outline" onClick={close} disabled={busy}>{cs.common.cancel}</Button>
        <Button type="button" onClick={() => void submit()} disabled={busy}>{busy ? cs.common.saving : t.upload}</Button>
      </>}>
      <p className="text-muted-foreground">{t.uploadHint}</p>
      {fileErr && <p role="alert" className="font-medium text-danger">{fileErr}</p>}
      <Field label={t.certFile} error={local('certificate')}>
        {(p) => <input {...p} type="file" accept=".pem,.crt,.cer,text/plain" data-autofocus className="block w-full text-sm"
          onChange={(e) => void readFile(e.target.files?.[0], setCert)} />}
      </Field>
      <Field label={`${t.certText} (${t.orPaste})`}>
        {(p) => <textarea {...p} className={ta} value={cert} spellCheck={false} autoComplete="off" onChange={(e) => setCert(e.target.value)} />}
      </Field>
      <Field label={t.keyFile} error={local('privateKey')}>
        {(p) => <input {...p} type="file" accept=".key,.pem,text/plain" className="block w-full text-sm"
          onChange={(e) => void readFile(e.target.files?.[0], setKey)} />}
      </Field>
      <Field label={`${t.keyText} (${t.orPaste})`}>
        {(p) => <textarea {...p} className={ta} value={key} spellCheck={false} autoComplete="off" onChange={(e) => setKey(e.target.value)} />}
      </Field>
    </Dialog>
  );
}

function CertWidget() {
  const [info, setInfo] = useState<TlsInfo | null>(null);
  const [failed, setFailed] = useState(false);
  const [upload, setUpload] = useState(false);
  const [gen, setGen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [genErr, setGenErr] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api<TlsInfo>('/settings/tls').then((d) => { if (alive) setInfo(d); }).catch(() => { if (alive) setFailed(true); });
    return () => { alive = false; };
  }, []);

  async function generate() {
    setBusy(true); setGenErr(null);
    try {
      setInfo(await api<TlsInfo>('/settings/tls/generate', { method: 'POST' }));
      setGen(false); setMsg({ ok: true, text: t.generated });
      setTimeout(() => location.reload(), 2000);
    } catch (e) { setGenErr(apiMessage(e)); }
    finally { setBusy(false); }
  }

  if (!info) {
    return (
      <Widget id="sys-cert" title={t.cert} icon={<ShieldCheck />} className="lg:col-span-2">
        {failed ? <LoadError /> : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-32 w-full" /></div>}
      </Widget>
    );
  }

  const days = Math.floor((Date.parse(info.notAfter) - Date.now()) / 86400000);
  const expired = Date.parse(info.notAfter) < Date.now();
  const soon = !expired && days < 30;
  const san = [...info.dnsNames, ...info.ipAddresses];

  return (
    <Widget id="sys-cert" title={t.cert} icon={<ShieldCheck />} className="lg:col-span-2"
      right={<Badge variant={info.selfSigned ? 'warning' : 'success'}>{info.selfSigned ? t.selfSigned : t.trusted}</Badge>}>
      <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 text-sm">
        <dt className="text-muted-foreground">{t.subject}</dt><dd className="break-all">{info.subject}</dd>
        <dt className="text-muted-foreground">{t.issuer}</dt><dd className="break-all">{info.issuer}</dd>
        <dt className="text-muted-foreground">{t.san}</dt>
        <dd className="break-all font-mono text-xs">{san.length ? san.join(', ') : t.noSan}</dd>
        <dt className="text-muted-foreground">{t.validity}</dt>
        <dd>
          {dt(info.notBefore)} – {dt(info.notAfter)}
          <div className="mt-1">
            {expired ? <Badge variant="danger">{t.expired}</Badge>
              : soon ? <Badge variant="warning">{t.expiresSoon(days)}</Badge>
                : <span className="text-xs text-muted-foreground">{t.daysLeft(days)}</span>}
          </div>
        </dd>
        <dt className="text-muted-foreground">{t.keyType}</dt><dd>{info.keyType}</dd>
        <dt className="text-muted-foreground">{t.fingerprint}</dt><dd className="break-all font-mono text-xs">{info.sha256}</dd>
      </dl>
      {msg && <p role="status" className={`mt-3 text-sm ${msg.ok ? 'text-success' : 'text-danger'}`}>{msg.text}</p>}
      <div className="mt-4 flex flex-wrap gap-2">
        <Button type="button" onClick={() => { setMsg(null); setUpload(true); }}><FileUp className="h-4 w-4" aria-hidden />{t.upload}</Button>
        <Button type="button" variant="outline" onClick={() => { setMsg(null); setGenErr(null); setGen(true); }}>
          <RefreshCw className="h-4 w-4" aria-hidden />{t.generate}
        </Button>
      </div>
      {upload && <UploadDialog onClose={() => setUpload(false)} onDone={(i) => { setInfo(i); setUpload(false); setMsg({ ok: true, text: t.uploadDone }); }} />}
      {gen && (
        <Dialog title={t.generateTitle} onClose={() => setGen(false)} busy={busy}
          footer={<>
            <Button type="button" variant="outline" onClick={() => setGen(false)} disabled={busy}>{cs.common.cancel}</Button>
            <Button type="button" data-autofocus onClick={() => void generate()} disabled={busy}>{busy ? t.generating : t.confirmGenerate}</Button>
          </>}>
          <p>{t.generateBody}</p>
          {genErr && <p role="alert" className="font-medium text-danger">{genErr}</p>}
        </Dialog>
      )}
    </Widget>
  );
}

export function SystemPage() {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-5">
      <TimeWidget />
      <CertWidget />
    </div>
  );
}
