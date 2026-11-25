import { AlertTriangle, Gauge, Info, Play } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { ActivityDisk, DiskActivity, StorageDisk } from '../api/types';
import { useCountdown } from '../hooks/useCountdown';
import { cs } from '../i18n/cs';
import { activityTitle, busyFirst, DURATIONS, isWarning, perMinute, relevantPartitions, sortHints } from '../lib/activity';
import { formatBytes } from '../lib/format';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Progress } from './ui/progress';

const t = cs.activity;

function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 409) return e.message;
  }
  return t.error;
}

function DiskResult({ d, seconds, storage }: { d: ActivityDisk; seconds: number; storage: StorageDisk[] | null }) {
  const hints = sortHints(d.hints);
  const parts = relevantPartitions(d);
  return (
    <section className="rounded-lg border border-border/70 p-3" aria-label={activityTitle(d, storage)}>
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <h3 className="font-medium">{activityTitle(d, storage)}</h3>
        <span className="font-mono text-xs text-muted-foreground">{d.name}</span>
        {d.system && <Badge variant="success">{cs.storage.systemDisk}</Badge>}
      </div>
      <p className="mt-1 text-sm tabular-nums">
        {d.readOps + d.writeOps === 0 ? t.nothing : <>
          {t.written(formatBytes(d.writeBytes), d.writeOps, formatBytes(perMinute(d.writeBytes, seconds)))}
          <span className="text-muted-foreground"> · {t.read(formatBytes(d.readBytes), d.readOps)}</span>
        </>}
      </p>
      {hints.length > 0 && (
        <ul className="mt-2 space-y-1.5">
          {hints.map((h) => {
            const warn = isWarning(h);
            const Icon = warn ? AlertTriangle : Info;
            return (
              <li key={h.code + (h.value ?? '')} role="note"
                className={`flex gap-2 rounded-md p-2 text-sm ${warn ? 'border border-warning/40 bg-warning/10' : 'bg-muted/40'}`}>
                <Icon className={`mt-0.5 h-4 w-4 shrink-0 ${warn ? 'text-warning' : 'text-muted-foreground'}`} aria-hidden />
                <span>{(t.hints[h.code] ?? (() => h.code))(h.value)}</span>
              </li>
            );
          })}
        </ul>
      )}
      {parts.length > 0 && (
        <ul className="mt-2 space-y-0.5 text-sm" aria-label={t.partitions}>
          {parts.map((p) => (
            <li key={p.name} className="flex flex-wrap justify-between gap-x-3">
              <span><span className="font-mono text-xs">{p.name}</span>{' '}
                <span className="text-muted-foreground break-all">{p.swap ? t.swap : p.mount || t.notMounted}</span></span>
              <span className="tabular-nums text-muted-foreground">
                {formatBytes(p.writeBytes)} / {p.writeOps}×{p.journalCommits ? ` · ${t.commits(p.journalCommits)}` : ''}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Results({ r, storage }: { r: DiskActivity; storage: StorageDisk[] | null }) {
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t.measuredAt(new Date(r.startedAt).toLocaleString('cs-CZ'), r.seconds)}</p>
      <div className="grid gap-3 md:grid-cols-2">
        {busyFirst(r.disks).map((d) => <DiskResult key={d.name} d={d} seconds={r.seconds} storage={storage} />)}
      </div>
      {(r.swapInPages > 0 || r.swapOutPages > 0) && <p className="text-sm">{t.swapPages(r.swapOutPages, r.swapInPages)}</p>}
      <div className="grid gap-4 md:grid-cols-2">
        <section>
          <h3 className="mb-2 text-sm text-muted-foreground">{t.processes}</h3>
          {r.processes.length === 0 ? <p className="text-sm text-muted-foreground">{t.noProcesses}</p> : (
            <ul className="space-y-2 text-sm">
              {r.processes.map((p) => (
                <li key={p.pid} className="rounded-md border border-border/70 p-2">
                  <p><span className="font-medium">{p.comm}</span> <span className="text-muted-foreground">({p.pid})</span>
                    {p.unit && <span className="text-muted-foreground"> · {t.unit(p.unit)}</span>}
                    {p.writeBytes != null && <span className="tabular-nums"> · {t.wrote(formatBytes(p.writeBytes))}</span>}</p>
                  {p.files.map((f) => <p key={f} className="font-mono text-xs break-all text-muted-foreground">{f}</p>)}
                </li>
              ))}
            </ul>
          )}
          {!r.processIo && <p className="mt-2 text-xs text-muted-foreground">{t.noProcessIo}</p>}
        </section>
        <section>
          <h3 className="mb-2 text-sm text-muted-foreground">{t.files}</h3>
          {r.files.length === 0 ? <p className="text-sm text-muted-foreground">{t.noFiles}</p> : (
            <ul className="space-y-2 text-sm">
              {r.files.map((f) => (
                <li key={f.path} className="rounded-md border border-border/70 p-2">
                  <p className="font-mono text-xs break-all">{f.path}</p>
                  <p className="text-xs text-muted-foreground">
                    {f.device}
                    {f.sizeDelta != null && f.sizeDelta !== 0 && ` · ${t.size((f.sizeDelta > 0 ? '+' : '−') + formatBytes(Math.abs(f.sizeDelta)))}`}
                    {' · '}{f.processes.length ? f.processes.join(', ') : t.byNobody}
                  </p>
                </li>
              ))}
            </ul>
          )}
          {r.filesTruncated && <p className="mt-2 text-xs text-muted-foreground">{t.truncated}</p>}
          {r.notScanned.length > 0 && <p className="mt-2 text-xs text-muted-foreground">{t.notScanned(r.notScanned.join(', '))}</p>}
        </section>
      </div>
    </div>
  );
}

/** Storage → disk activity (W1.9): runs only when the user asks, never polls. */
export function ActivityPanel({ storage }: { storage: StorageDisk[] | null }) {
  const [seconds, setSeconds] = useState<number>(DURATIONS[0]);
  const [deadline, setDeadline] = useState<string | null>(null);
  const [result, setResult] = useState<DiskActivity | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);
  const left = useCountdown(deadline);
  useEffect(() => () => abort.current?.abort(), []);

  async function run() {
    abort.current?.abort();
    const ac = new AbortController();
    abort.current = ac;
    setErr(null);
    setDeadline(new Date(Date.now() + seconds * 1000).toISOString());
    try {
      setResult(await api<DiskActivity>(`/storage/activity?seconds=${seconds}`, { signal: ac.signal }));
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return;
      setErr(errorText(e));
    } finally {
      if (abort.current === ac) setDeadline(null);
    }
  }

  const busy = deadline !== null;
  return (
    <Widget id="disk-activity" title={t.title} icon={<Gauge />}>
      <p className="text-sm text-muted-foreground">{t.intro}</p>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <fieldset className="flex items-center gap-1" disabled={busy}>
          <legend className="sr-only">{t.duration}</legend>
          {DURATIONS.map((d) => (
            <Button key={d} type="button" size="sm" variant={d === seconds ? 'default' : 'outline'} aria-pressed={d === seconds}
              onClick={() => setSeconds(d)}>{t.seconds(d)}</Button>
          ))}
        </fieldset>
        <Button type="button" onClick={() => void run()} disabled={busy}>
          <Play className="h-4 w-4" aria-hidden />{busy ? t.running : result ? t.again : t.start}
        </Button>
      </div>
      {busy && (
        <div className="mt-3 space-y-1" aria-live="polite">
          <Progress value={left == null ? 0 : Math.min(100, Math.round(((seconds - left) / seconds) * 100))} label={t.running} warn={101} crit={101} />
          <p className="text-xs text-muted-foreground tabular-nums">{t.remaining(left ?? seconds)}</p>
        </div>
      )}
      {err && <p role="alert" className="mt-3 text-sm text-danger">{err}</p>}
      {result && !busy && <div className="mt-4"><Results r={result} storage={storage} /></div>}
    </Widget>
  );
}
