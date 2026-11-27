import { Copy, KeyRound, Loader2, PlugZap, ShieldCheck } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { BackupKey, BackupParams, BackupScan, BackupTestResult, ShareList } from '../api/types';
import { cs } from '../i18n/cs';
import { DEFAULT_BACKUP_ROOT, authorizedLine, hostOk, portOk, relPathOk, userOk, type TaskErrors } from '../lib/tasks';
import { Button } from './ui/button';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { ListEditor } from './ui/list-editor';

const b = cs.tasks.backup;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';
const textareaCls = 'w-full break-all rounded-md border border-border bg-background/80 p-2 font-mono text-xs';

function msgOf(e: unknown, fallback: string): string {
  return e instanceof ApiError && e.status !== 0 && e.status !== 503 && e.message ? e.message : fallback;
}

/** Read-only text with a copy button; falls back to selecting the text when the clipboard is unavailable. */
function CopyBox({ label, text }: { label: string; text: string }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [note, setNote] = useState<string | null>(null);
  async function copy() {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('no clipboard');
      await navigator.clipboard.writeText(text);
      setNote(b.copied);
    } catch {
      ref.current?.focus(); ref.current?.select();
      setNote(b.copySelected);
    }
  }
  return (
    <div className="space-y-1">
      <Field label={label}>
        {(p) => <textarea {...p} ref={ref} readOnly rows={4} className={textareaCls} value={text} onFocus={(e) => e.currentTarget.select()} />}
      </Field>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => void copy()}><Copy className="h-4 w-4" aria-hidden />{b.copy}</Button>
        {note && <span role="status" className="text-xs text-muted-foreground">{note}</span>}
      </div>
    </div>
  );
}

function KeyPanel() {
  const [key, setKey] = useState<BackupKey | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [root, setRoot] = useState(DEFAULT_BACKUP_ROOT);
  async function load() {
    setBusy(true); setErr(null);
    try { setKey(await api<BackupKey>('/backup/key')); } catch (e) { setErr(msgOf(e, b.keyError)); }
    setBusy(false);
  }
  const line = key ? authorizedLine(key.authorizedKeysHint, root) : null;
  return (
    <section className="min-w-0 space-y-3 rounded-lg border border-border p-3" aria-label={b.keyTitle}>
      <h4 className="flex items-center gap-2 text-sm font-semibold"><KeyRound className="h-4 w-4" aria-hidden />{b.keyTitle}</h4>
      <p className="text-xs text-muted-foreground">{b.keyIntro}</p>
      {!key && (
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void load()}>
          {busy ? <><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{b.keyLoading}</> : b.keyLoad}
        </Button>
      )}
      {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
      {key && (
        <div className="space-y-3">
          <p className="break-all text-xs"><span className="font-medium">{b.keyFingerprint}: </span><span className="font-mono">{key.fingerprint}</span></p>
          <Field label={b.keyRoot} hint={b.keyRootHint} error={line === null ? b.keyRootBad : null}>
            {(p) => <Input {...p} className={invalidCls} value={root} autoComplete="off" spellCheck={false} onChange={(e) => setRoot(e.target.value)} />}
          </Field>
          {line !== null && <CopyBox label={b.keyLine} text={line} />}
        </div>
      )}
    </section>
  );
}

function ServerCheck({ bk }: { bk: BackupParams }) {
  const host = bk.host.trim();
  const port = bk.port || 22;
  const path = bk.paths[0]?.trim() ?? '';
  const [scan, setScan] = useState<BackupScan | null>(null);
  const [trusted, setTrusted] = useState<string | null>(null);
  const [busy, setBusy] = useState<'scan' | 'trust' | 'test' | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [test, setTest] = useState<BackupTestResult | null>(null);
  const hostReady = hostOk(host) && portOk(bk.port);
  const testReady = hostReady && userOk(bk.user.trim()) && relPathOk(path);

  async function doScan() {
    setBusy('scan'); setErr(null); setScan(null); setTrusted(null);
    try { setScan(await api<BackupScan>('/backup/hostkey/scan', { method: 'POST', body: { host, port } })); }
    catch (e) { setErr(msgOf(e, b.scanError)); }
    setBusy(null);
  }
  async function doTrust(k: { type: string; key: string; fingerprint: string }) {
    setBusy('trust'); setErr(null);
    try {
      await api('/backup/hostkey/trust', { method: 'POST', body: { host, port, type: k.type, key: k.key } });
      setTrusted(k.fingerprint);
    } catch (e) { setErr(msgOf(e, b.trustError)); }
    setBusy(null);
  }
  async function doTest() {
    setBusy('test'); setErr(null); setTest(null);
    try { setTest(await api<BackupTestResult>('/backup/test', { method: 'POST', body: { host, port, user: bk.user.trim(), path } })); }
    catch (e) { setErr(msgOf(e, b.testFail)); }
    setBusy(null);
  }
  const spin = <Loader2 className="h-4 w-4 animate-spin" aria-hidden />;
  return (
    <section className="min-w-0 space-y-3 rounded-lg border border-border p-3" aria-label={b.scan}>
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" size="sm" disabled={!hostReady || busy !== null} onClick={() => void doScan()}>
          {busy === 'scan' ? <>{spin}{b.scanning}</> : <><ShieldCheck className="h-4 w-4" aria-hidden />{b.scan}</>}
        </Button>
        <Button type="button" variant="outline" size="sm" disabled={!testReady || busy !== null} onClick={() => void doTest()}>
          {busy === 'test' ? <>{spin}{b.testing}</> : <><PlugZap className="h-4 w-4" aria-hidden />{b.test}</>}
        </Button>
      </div>
      {!testReady && <p className="text-xs text-muted-foreground">{b.needFields}</p>}
      {err && <p role="alert" className="text-sm font-medium text-danger">{err}</p>}
      {scan && (
        <div className="space-y-2" aria-live="polite">
          {scan.changed && <p role="alert" className="rounded-md border border-danger p-2 text-sm font-medium text-danger">{b.scanChanged}</p>}
          {scan.known && !scan.changed && <p className="text-sm text-success">{b.scanKnown}</p>}
          {scan.keys.length === 0 ? <p className="text-sm text-muted-foreground">{b.scanNone}</p> : (
            <>
              <p className="text-xs text-muted-foreground">{b.scanIntro}</p>
              <ul className="space-y-2">
                {scan.keys.map((k) => (
                  <li key={k.fingerprint} className="min-w-0 space-y-1 rounded-md bg-muted/40 p-2">
                    <p className="text-xs font-medium">{k.type}</p>
                    <p className="break-all font-mono text-xs">{k.fingerprint}</p>
                    {trusted === k.fingerprint
                      ? <p role="status" className="text-xs font-medium text-success">{b.trusted}</p>
                      : <Button type="button" size="sm" variant={scan.changed ? 'destructive' : 'outline'} disabled={busy !== null} onClick={() => void doTrust(k)}>
                        {busy === 'trust' ? b.trusting : b.trust}
                      </Button>}
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      )}
      {test && (
        <div className="space-y-1 text-sm" aria-live="polite">
          {test.ok ? (
            <>
              <p className="font-medium text-success">{b.testOk}</p>
              <p className="break-all text-xs text-muted-foreground">
                {test.entries && test.entries.length > 0 ? `${b.testEntries} ${test.entries.join(', ')}` : b.testEmpty}
              </p>
            </>
          ) : <p role="alert" className="break-words font-medium text-danger">{b.testFail}{test.message ? ` ${test.message}` : ''}</p>}
          {test.ok && test.fullAccess && <p role="alert" className="rounded-md border border-danger p-2 font-medium text-danger">{b.testFull}</p>}
        </div>
      )}
    </section>
  );
}

/** Destination: a share with backupTarget (fetched from /shares) and a subfolder. */
function DestFields({ value, onChange, errors, taskId }: {
  value: BackupParams; onChange: (p: Partial<BackupParams>) => void; errors: TaskErrors; taskId: string;
}) {
  const [targets, setTargets] = useState<string[] | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api<ShareList>('/shares')
      .then((l) => { if (live) setTargets(l.shares.filter((s) => s.backupTarget).map((s) => s.name)); })
      .catch(() => { if (live) setFailed(true); });
    return () => { live = false; };
  }, []);
  const dest = value.dest;
  const setDest = (p: Partial<typeof dest>) => onChange({ dest: { ...dest, ...p } });
  if (failed) return <p role="alert" className="text-sm font-medium text-danger">{b.destLoadError}</p>;
  if (targets === null) return <Loader2 className="h-4 w-4 animate-spin" aria-label={b.keyLoading} />;
  if (targets.length === 0 && dest.share === '') {
    return <p role="note" className="rounded-md bg-muted/50 p-2 text-sm">{b.destNone} <a className="underline" href="#/shares">{b.destLink}</a></p>;
  }
  const missing = dest.share !== '' && !targets.includes(dest.share);
  return (
    <div className="space-y-2">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={b.dest} hint={b.destHint} error={errors.destShare ?? (missing ? b.destMissing(dest.share) : undefined)}>
          {(p) => (
            <select {...p} className={`${selectCls} ${invalidCls}`} value={dest.share} onChange={(e) => setDest({ share: e.target.value })}>
              <option value="">{b.destChoose}</option>
              {missing && <option value={dest.share}>{dest.share}</option>}
              {targets.map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          )}
        </Field>
        <Field label={b.destFolder} hint={b.destFolderHint} error={errors.destFolder}>
          {(p) => <Input {...p} className={invalidCls} value={dest.folder ?? ''} autoComplete="off" spellCheck={false} maxLength={32}
            onChange={(e) => setDest({ folder: e.target.value })} />}
        </Field>
      </div>
      {dest.share !== '' && <p className="break-all text-xs text-muted-foreground">{b.destPath(dest.share, (dest.folder ?? '').trim(), taskId)}</p>}
    </div>
  );
}

/** Editor fields of a `backup-pull` task (server, folders, retention, key, host-key check). */
export function BackupTaskFields({ value, onChange, errors, isNew, taskId }: {
  value: BackupParams; onChange: (v: BackupParams) => void; errors: TaskErrors; isNew: boolean; taskId: string;
}) {
  const set = (p: Partial<BackupParams>) => onChange({ ...value, ...p });
  const num = (v: string) => (v === '' ? 0 : Number(v));
  return (
    <div className="min-w-0 space-y-4">
      {isNew && <p role="note" className="rounded-md bg-muted/50 p-2 text-sm">{b.notice(value.keepDays >= 1 && value.keepDays <= 90 ? value.keepDays : 35)}</p>}
      <DestFields value={value} onChange={set} errors={errors} taskId={taskId} />
      <Field label={b.server} hint={b.serverHint} error={errors.host}>
        {(p) => <Input {...p} className={invalidCls} value={value.host} autoComplete="off" spellCheck={false} maxLength={253} onChange={(e) => set({ host: e.target.value })} />}
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={b.port} hint={b.portHint} error={errors.port}>
          {(p) => <Input {...p} className={invalidCls} type="number" inputMode="numeric" min={1} max={65535} placeholder="22" value={value.port || ''} onChange={(e) => set({ port: num(e.target.value) })} />}
        </Field>
        <Field label={b.user} error={errors.user}>
          {(p) => <Input {...p} className={invalidCls} value={value.user} autoComplete="off" spellCheck={false} maxLength={32} onChange={(e) => set({ user: e.target.value })} />}
        </Field>
      </div>
      <ListEditor legend={b.paths} hint={b.pathsHint} error={errors.paths} values={value.paths} max={8} placeholder={b.pathPlaceholder}
        itemLabel={b.pathAria} addLabel={b.addPath} onChange={(paths) => set({ paths })} />
      <Field label={b.keepDays} hint={b.keepDaysHint} error={errors.keepDays}>
        {(p) => <Input {...p} className={invalidCls} type="number" inputMode="numeric" min={1} max={90} value={value.keepDays || ''} onChange={(e) => set({ keepDays: num(e.target.value) })} />}
      </Field>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" className="h-4 w-4" checked={!!value.fresh}
          onChange={(e) => set({ fresh: e.target.checked ? { file: value.fresh?.file ?? '', maxAgeHours: value.fresh?.maxAgeHours ?? 26 } : undefined })} />
        <span className="font-medium">{b.freshOn}</span>
      </label>
      {value.fresh && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={b.freshFile} hint={b.freshFileHint} error={errors.freshFile}>
            {(p) => <Input {...p} className={invalidCls} value={value.fresh?.file ?? ''} autoComplete="off" spellCheck={false}
              onChange={(e) => set({ fresh: { file: e.target.value, maxAgeHours: value.fresh?.maxAgeHours ?? 26 } })} />}
          </Field>
          <Field label={b.freshAge} error={errors.freshAge}>
            {(p) => <Input {...p} className={invalidCls} type="number" inputMode="numeric" min={1} max={720} value={value.fresh?.maxAgeHours || ''}
              onChange={(e) => set({ fresh: { file: value.fresh?.file ?? '', maxAgeHours: num(e.target.value) } })} />}
          </Field>
        </div>
      )}
      <Field label={b.limit} hint={b.limitHint} error={errors.bwlimit}>
        {(p) => <Input {...p} className={invalidCls} type="number" inputMode="numeric" min={0} max={1000000} value={value.bwlimitKiB} onChange={(e) => set({ bwlimitKiB: num(e.target.value) })} />}
      </Field>
      <ListEditor legend={b.excludes} hint={b.excludesHint} error={errors.excludes} values={value.excludes} max={16} placeholder={b.excludePlaceholder}
        itemLabel={b.excludeAria} addLabel={b.addExclude} onChange={(excludes) => set({ excludes })} />
      <KeyPanel />
      <ServerCheck bk={value} />
    </div>
  );
}
