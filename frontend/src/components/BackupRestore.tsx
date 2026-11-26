import { Loader2 } from 'lucide-react';
import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError } from '../api/client';
import type { FileShareList, RestoreStatus } from '../api/types';
import { cs } from '../i18n/cs';
import { defaultRestoreName, restorePercent, restoreRunning, restoreUrl, versionDateLabel } from '../lib/backupVersions';
import { formatBytes } from '../lib/format';
import { Button } from './ui/button';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Progress } from './ui/progress';

const t = cs.backupRestore;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';

export interface RestoreTarget { path: string; isFile: boolean }

/** Form for restoring one folder or file of a version into a share (W4.3). */
export function RestoreForm({ id, jobName, date, target, onStarted, onCancel }: {
  id: string; jobName: string; date: string; target: RestoreTarget;
  onStarted: (s: RestoreStatus) => void; onCancel: () => void;
}) {
  const [shares, setShares] = useState<FileShareList | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [share, setShare] = useState('');
  const [dir, setDir] = useState('');
  const [name, setName] = useState(() => defaultRestoreName(target.path, jobName, date, target.isFile));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ field: string | null; message: string } | null>(null);

  useEffect(() => {
    let live = true;
    api<FileShareList>('/files/shares').then((r) => {
      if (!live) return;
      setShares(r);
      setShare(r.shares.find((s) => s.access === 'rw')?.name ?? '');
    }).catch(() => { if (live) setLoadFailed(true); });
    return () => { live = false; };
  }, []);

  const writable = shares?.shares.filter((s) => s.access === 'rw') ?? [];
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(null);
    try {
      const st = await api<RestoreStatus>(restoreUrl(id, date), {
        method: 'POST', body: { path: target.path, share, dir: dir.trim().replace(/^\/+|\/+$/g, ''), name: name.trim() },
      });
      onStarted(st);
    } catch (err) {
      setError(err instanceof ApiError ? { field: err.field, message: err.message } : { field: null, message: t.error });
    } finally { setBusy(false); }
  };

  const label = target.path === '' ? t.wholeVersion : target.path.slice(target.path.lastIndexOf('/') + 1);
  const fieldErr = (f: string) => (error && error.field === f ? error.message : null);
  const otherErr = error && !['share', 'dir', 'name'].includes(error.field ?? '') ? error.message : null;
  return (
    <form className="space-y-3" onSubmit={(e) => void submit(e)}>
      <div>
        <h3 className="break-all font-semibold">{t.title(label)}</h3>
        <p className="text-sm text-muted-foreground">{t.from(versionDateLabel(date))}</p>
      </div>
      {loadFailed ? <p role="alert" className="text-danger">{cs.backupVersions.error}</p>
        : !shares ? <p role="status" className="flex items-center gap-2 text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{cs.common.loading}</p>
          : shares.noAccount ? <p role="note" className="text-sm">{t.noAccount}</p>
            : writable.length === 0 ? <p role="note" className="text-sm">{t.noShares}</p> : (
              <>
                <Field label={t.share} error={fieldErr('share')}>
                  {(p) => (
                    <select {...p} className={`${selectCls} ${invalidCls}`} value={share} onChange={(e) => setShare(e.target.value)}>
                      {writable.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
                    </select>
                  )}
                </Field>
                <Field label={t.dir} hint={t.dirHint} error={fieldErr('dir')}>
                  {(p) => <Input {...p} className={invalidCls} value={dir} onChange={(e) => setDir(e.target.value)} autoComplete="off" />}
                </Field>
                <Field label={t.name} hint={t.nameHint} error={fieldErr('name')}>
                  {(p) => <Input {...p} className={invalidCls} value={name} required maxLength={255} onChange={(e) => setName(e.target.value)} autoComplete="off" />}
                </Field>
                <p className="text-xs text-muted-foreground">{t.note}</p>
              </>
            )}
      {otherErr && <p role="alert" className="text-sm font-medium text-danger">{otherErr}</p>}
      <div className="flex flex-wrap gap-2">
        {writable.length > 0 && !shares?.noAccount && (
          <Button type="submit" disabled={busy || !share || name.trim() === ''}>
            {busy && <Loader2 className="mr-1 h-4 w-4 animate-spin" aria-hidden />}{busy ? t.starting : t.start}
          </Button>
        )}
        <Button type="button" variant="outline" onClick={onCancel}>{t.cancel}</Button>
      </div>
    </form>
  );
}

/** Progress or result of the current or last restore. */
export function RestoreProgress({ status, onStop }: { status: RestoreStatus; onStop: () => Promise<void> }) {
  const [stopping, setStopping] = useState(false);
  if (status.state === 'idle') return null;
  const dest = `${status.share ?? ''}/${status.dest ?? ''}`;
  if (restoreRunning(status)) {
    return (
      <section aria-live="polite" className="space-y-2 rounded-md border border-border p-3">
        <p className="break-all text-sm font-medium">{t.running}: {dest}</p>
        <Progress value={restorePercent(status)} label={t.progressLabel} warn={101} crit={101} />
        <p className="text-xs text-muted-foreground">
          {status.state === 'scanning' ? t.scanning
            : t.progress(status.files, status.totalFiles, formatBytes(status.bytes), formatBytes(status.totalBytes))}
        </p>
        <Button type="button" variant="outline" size="sm" disabled={stopping}
          onClick={() => { setStopping(true); void onStop().finally(() => setStopping(false)); }}>
          {stopping ? t.stopping : t.stop}
        </Button>
      </section>
    );
  }
  const tone = status.state === 'done' ? 'text-success' : status.state === 'failed' ? 'text-danger' : 'text-muted-foreground';
  return (
    <section aria-live="polite" className="space-y-1 rounded-md border border-border p-3 text-sm">
      <p className={`break-all font-medium ${tone}`}>
        {status.state === 'done' ? t.done(dest, status.files, formatBytes(status.bytes))
          : status.state === 'failed' ? `${t.failed}: ${status.message ?? ''}` : t.cancelled}
      </p>
      {status.state === 'done' && status.skipped > 0 && <p className="text-muted-foreground">{t.skipped(status.skipped)}</p>}
    </section>
  );
}
