import { ChevronRight, File, Folder, Loader2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { BackupBrowse, BackupVersion, BackupVersionList, Task } from '../api/types';
import { cs } from '../i18n/cs';
import { breadcrumbs, browseUrl, downloadUrl, entryDisabledReason, joinPath, versionDateLabel } from '../lib/backupVersions';
import { formatBytes } from '../lib/format';
import { formatWhen } from '../lib/tasks';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

const t = cs.backupVersions;

function Loading() {
  return <p role="status" className="flex items-center gap-2 text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{cs.common.loading}</p>;
}

function ErrorRetry({ onRetry }: { onRetry: () => void }) {
  return (
    <p role="alert" className="text-danger">{t.error} <Button type="button" variant="outline" size="sm" onClick={onRetry}>{cs.common.retry}</Button></p>
  );
}

function VersionList({ id, onPick }: { id: string; onPick: (v: BackupVersion) => void }) {
  const [data, setData] = useState<BackupVersionList | null>(null);
  const [failed, setFailed] = useState(false);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    setData(null); setFailed(false);
    api<BackupVersionList>(`/backup/${encodeURIComponent(id)}/versions`)
      .then((r) => { if (live) setData(r); }).catch(() => { if (live) setFailed(true); });
    return () => { live = false; };
  }, [id, tick]);
  if (failed) return <ErrorRetry onRetry={() => setTick((n) => n + 1)} />;
  if (!data) return <Loading />;
  return (
    <div className="space-y-2">
      {!data.complete && <p role="note" className="text-sm font-medium text-warning">{t.incomplete}</p>}
      {data.versions.length === 0 ? <p className="text-muted-foreground">{t.empty}</p> : (
        <ul className="divide-y divide-border">
          {data.versions.map((v) => (
            <li key={v.date}>
              <button type="button" aria-label={t.open(versionDateLabel(v.date))} onClick={() => onPick(v)}
                className="flex w-full flex-col gap-0.5 rounded-md px-2 py-2 text-left hover:bg-muted">
                <span className="font-medium">{versionDateLabel(v.date)}</span>
                <span className="break-words text-xs text-muted-foreground">
                  {t.files(v.files)} · {formatBytes(v.bytes)} · {t.added(formatBytes(v.addedBytes))}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Browser({ id, version, onBack }: { id: string; version: BackupVersion; onBack: () => void }) {
  const [path, setPath] = useState('');
  const [page, setPage] = useState<BackupBrowse | null>(null);
  const [failed, setFailed] = useState(false);
  const [more, setMore] = useState(false);
  const [tick, setTick] = useState(0);
  const date = version.date;

  useEffect(() => {
    let live = true;
    setPage(null); setFailed(false);
    api<BackupBrowse>(browseUrl(id, date, path))
      .then((r) => { if (live) setPage(r); }).catch(() => { if (live) setFailed(true); });
    return () => { live = false; };
  }, [id, date, path, tick]);

  const loadMore = useCallback(async () => {
    if (!page) return;
    setMore(true);
    try {
      const next = await api<BackupBrowse>(browseUrl(id, date, path, page.offset + page.entries.length));
      setPage({ ...next, entries: [...page.entries, ...next.entries], offset: page.offset });
    } catch { setFailed(true); }
    finally { setMore(false); }
  }, [page, id, date, path]);

  const crumbs = breadcrumbs(path);
  const shown = page ? page.offset + page.entries.length : 0;
  return (
    <div className="space-y-3">
      <Button type="button" variant="outline" size="sm" onClick={onBack}>{t.back}</Button>
      <nav aria-label={t.crumbs}>
        <ol className="flex flex-wrap items-center gap-1 text-sm">
          <li><button type="button" className="break-all rounded px-1 font-medium underline-offset-2 hover:underline" onClick={() => setPath('')}>{versionDateLabel(date)}</button></li>
          {crumbs.map((c, i) => (
            <li key={c.path} className="flex items-center gap-1">
              <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden />
              {i === crumbs.length - 1
                ? <span aria-current="page" className="break-all font-medium">{c.name}</span>
                : <button type="button" className="break-all rounded px-1 underline-offset-2 hover:underline" onClick={() => setPath(c.path)}>{c.name}</button>}
            </li>
          ))}
        </ol>
      </nav>
      {failed ? <ErrorRetry onRetry={() => { setFailed(false); setTick((n) => n + 1); }} />
        : !page ? <Loading /> : (
          <>
            {page.entries.length === 0 && <p className="text-muted-foreground">{t.emptyDir}</p>}
            <ul className="divide-y divide-border">
              {page.entries.map((e) => {
                const reason = entryDisabledReason(e);
                const full = joinPath(path, e.name);
                const meta = e.type === 'file' ? `${formatBytes(e.size)} · ${formatWhen(e.modified)}` : null;
                const icon = e.type === 'dir' ? <Folder className="mt-0.5 h-4 w-4 shrink-0" aria-hidden /> : <File className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />;
                const body = (
                  <>
                    {icon}
                    <span className="min-w-0 flex-1">
                      <span className="block break-all">{e.name}</span>
                      {(meta || reason) && <span className="block break-words text-xs text-muted-foreground">{reason ?? meta}</span>}
                    </span>
                  </>
                );
                const cls = 'flex w-full items-start gap-2 rounded-md px-2 py-2 text-left';
                return (
                  <li key={e.name}>
                    {reason ? <div className={`${cls} opacity-60`}>{body}</div>
                      : e.type === 'dir'
                        ? <button type="button" aria-label={t.openDir(e.name)} className={`${cls} hover:bg-muted`} onClick={() => setPath(full)}>{body}</button>
                        : <a href={downloadUrl(id, date, full)} download aria-label={t.download(e.name)} className={`${cls} hover:bg-muted`}>{body}</a>}
                  </li>
                );
              })}
            </ul>
            {page.truncated && <p role="note" className="text-sm text-muted-foreground">{t.truncated}</p>}
            {shown < page.total && (
              <Button type="button" variant="outline" size="sm" disabled={more} onClick={() => void loadMore()}>
                {more && <Loader2 className="mr-1 h-4 w-4 animate-spin" aria-hidden />}{t.loadMore}
              </Button>
            )}
          </>
        )}
    </div>
  );
}

export function BackupVersions({ task, onClose }: { task: Task; onClose: () => void }) {
  const [version, setVersion] = useState<BackupVersion | null>(null);
  return (
    <Dialog title={t.title(task.name)} onClose={onClose}
      footer={<Button type="button" variant="outline" onClick={onClose}>{cs.common.close}</Button>}>
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        {version ? <Browser id={task.id} version={version} onBack={() => setVersion(null)} />
          : <VersionList id={task.id} onPick={setVersion} />}
      </div>
    </Dialog>
  );
}
