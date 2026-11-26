import { ChevronRight, File, Folder, FolderPlus, Link2, Loader2, MoveRight, Pencil, RefreshCw, Trash2, Upload, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { BackupEntry, FileBatch, FileList, FileShare, FileShareList } from '../api/types';
import { cs } from '../i18n/cs';
import { breadcrumbs, joinPath } from '../lib/backupVersions';
import { actionUrl, batchProblem, fileDownloadUrl, listUrl, nameProblem, percent, uploadUrl } from '../lib/files';
import { formatBytes } from '../lib/format';
import { formatWhen } from '../lib/tasks';
import { uploadFile } from '../lib/upload';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Progress } from './ui/progress';

const t = cs.files;
const MAX_UPLOADS = 2;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return t.unavailable;
    if (e.message && [400, 403, 404, 409, 413, 429, 500].includes(e.status)) return e.message;
  }
  return cs.errors.generic;
}

function Loading() {
  return <p role="status" className="flex items-center gap-2 text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{cs.common.loading}</p>;
}

function ErrorRetry({ msg, onRetry }: { msg: string; onRetry: () => void }) {
  return <p role="alert" className="text-danger">{msg} <Button type="button" variant="outline" size="sm" onClick={onRetry}>{cs.common.retry}</Button></p>;
}

const disabledReason = (e: BackupEntry): string | null =>
  e.badName ? t.badName : e.type === 'link' ? t.isLink : e.type === 'other' ? t.isOther : null;

/* ---------- uploads ---------- */

type UpState = 'waiting' | 'running' | 'done' | 'error' | 'cancelled' | 'conflict';
interface Up { id: number; share: string; dir: string; file: File; sent: number; state: UpState; message: string; overwrite: boolean }

/** Upload queue: at most MAX_UPLOADS at once (the server refuses more), the rest waits. */
function useUploads(onDone: (share: string, dir: string) => void) {
  const [list, setList] = useState<Up[]>([]);
  const ctrls = useRef(new Map<number, AbortController>());
  const started = useRef(new Set<number>());
  const nextId = useRef(1);
  const done = useRef(onDone);
  done.current = onDone;
  const patch = useCallback((id: number, p: Partial<Up>) => setList((l) => l.map((u) => (u.id === id ? { ...u, ...p } : u))), []);

  useEffect(() => {
    let free = MAX_UPLOADS - list.filter((u) => u.state === 'running').length;
    for (const u of list) {
      if (free <= 0) break;
      if (u.state !== 'waiting' || started.current.has(u.id)) continue;
      started.current.add(u.id);
      free--;
      const c = new AbortController();
      ctrls.current.set(u.id, c);
      patch(u.id, { state: 'running', sent: 0 });
      uploadFile(uploadUrl(u.share, u.dir, u.file.name, u.overwrite), u.file, (sent) => patch(u.id, { sent }), c.signal)
        .then(() => { patch(u.id, { state: 'done', sent: u.file.size }); done.current(u.share, u.dir); })
        .catch((e: unknown) => {
          if (e instanceof DOMException && e.name === 'AbortError') patch(u.id, { state: 'cancelled' });
          else if (e instanceof ApiError && e.code === 'file_exists') patch(u.id, { state: 'conflict', message: e.message });
          else patch(u.id, { state: 'error', message: apiMessage(e) });
        })
        .finally(() => { ctrls.current.delete(u.id); started.current.delete(u.id); });
    }
  }, [list, patch]);

  const active = list.some((u) => u.state === 'running' || u.state === 'waiting');
  useEffect(() => {
    if (!active) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [active]);

  const add = (share: string, dir: string, files: File[]) => setList((l) => [...l, ...files.map((file): Up => {
    const problem = nameProblem(file.name);
    return { id: nextId.current++, share, dir, file, sent: 0, overwrite: false,
      state: problem ? 'error' : 'waiting', message: problem ?? '' };
  })]);
  const cancel = (id: number) => {
    const c = ctrls.current.get(id);
    if (c) c.abort(); else patch(id, { state: 'cancelled' });
  };
  const overwrite = (id: number) => patch(id, { state: 'waiting', overwrite: true, message: '' });
  const dismiss = () => setList((l) => l.filter((u) => u.state === 'running' || u.state === 'waiting' || u.state === 'conflict'));
  return { list, add, cancel, overwrite, dismiss };
}

function UploadPanel({ up }: { up: ReturnType<typeof useUploads> }) {
  if (up.list.length === 0) return null;
  const state = (u: Up) => ({
    waiting: t.uploadWaiting, running: `${percent(u.sent, u.file.size)} %`, done: t.uploadDone,
    cancelled: t.uploadCancelled, error: u.message, conflict: u.message,
  })[u.state];
  return (
    <Widget id="files-uploads" title={t.uploads} icon={<Upload className="h-4 w-4" aria-hidden />} right={
      <Button type="button" variant="ghost" size="sm" onClick={up.dismiss}>{t.uploadDismiss}</Button>
    }>
      <ul className="space-y-3" aria-live="polite">
        {up.list.map((u) => (
          <li key={u.id} className="space-y-1">
            <div className="flex items-start gap-2">
              <span className="min-w-0 flex-1">
                <span className="block break-all text-sm">{u.file.name}</span>
                <span className={`block break-words text-xs ${u.state === 'error' || u.state === 'conflict' ? 'text-danger' : 'text-muted-foreground'}`}>
                  {formatBytes(u.file.size)} · {state(u)}
                </span>
              </span>
              {u.state === 'conflict' && <Button type="button" variant="outline" size="sm" onClick={() => up.overwrite(u.id)}>{t.uploadOverwrite}</Button>}
              {(u.state === 'running' || u.state === 'waiting' || u.state === 'conflict') && (
                <Button type="button" variant="ghost" size="icon" className="h-8 w-8" aria-label={t.uploadCancel(u.file.name)} onClick={() => up.cancel(u.id)}>
                  <X className="h-4 w-4" aria-hidden />
                </Button>
              )}
            </div>
            {u.state === 'running' && <Progress value={percent(u.sent, u.file.size)} label={t.uploadProgress(u.file.name)} warn={101} crit={101} />}
          </li>
        ))}
      </ul>
    </Widget>
  );
}

/* ---------- dialogs ---------- */

function NameDialog({ title, initial, submit, onSubmit, onClose }: {
  title: string; initial: string; submit: string; onSubmit: (name: string) => Promise<void>; onClose: () => void;
}) {
  const [name, setName] = useState(initial);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function go(e?: React.FormEvent) {
    e?.preventDefault();
    const p = nameProblem(name);
    if (p) { setErr(p); return; }
    setBusy(true); setErr(null);
    try { await onSubmit(name); onClose(); } catch (x) { setErr(apiMessage(x)); } finally { setBusy(false); }
  }
  return (
    <Dialog title={title} onClose={onClose} busy={busy} footer={<>
      <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
      <Button type="button" onClick={() => void go()} disabled={busy}>{busy && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{submit}</Button>
    </>}>
      <form onSubmit={(e) => void go(e)}>
        <Field label={t.name.label} error={err}>
          {(p) => <Input {...p} data-autofocus className={invalidCls} value={name} maxLength={255} onChange={(e) => setName(e.target.value)} />}
        </Field>
      </form>
    </Dialog>
  );
}

function MoveDialog({ share, from, names, onClose, onMoved }: {
  share: string; from: string; names: string[]; onClose: () => void; onMoved: (r: FileBatch) => void;
}) {
  const [dest, setDest] = useState(from);
  const [dirs, setDirs] = useState<BackupEntry[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    setDirs(null); setErr(null);
    api<FileList>(listUrl(share, dest))
      .then((r) => { if (live) setDirs(r.entries.filter((e) => e.type === 'dir' && !e.badName)); })
      .catch((e: unknown) => { if (live) setErr(apiMessage(e)); });
    return () => { live = false; };
  }, [share, dest]);
  const moving = new Set(names.map((n) => joinPath(from, n)));
  async function go() {
    setBusy(true); setErr(null);
    try {
      const r = await api<FileBatch>(actionUrl(share, 'move'), { method: 'POST', body: { paths: [...moving], dest } });
      onMoved(r); onClose();
    } catch (e) { setErr(apiMessage(e)); } finally { setBusy(false); }
  }
  const crumbs = breadcrumbs(dest);
  return (
    <Dialog title={t.moveTitle(names.length)} onClose={onClose} busy={busy} footer={<>
      <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
      <Button type="button" onClick={() => void go()} disabled={busy || dest === from}>{busy && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{t.moveHere}</Button>
    </>}>
      <p className="text-muted-foreground">{t.moveHint}</p>
      <Crumbs root={share} crumbs={crumbs} onPick={setDest} />
      {err && <p role="alert" className="text-danger">{err}</p>}
      {!dirs && !err ? <Loading /> : (
        <ul className="divide-y divide-border">
          {(dirs ?? []).map((d) => {
            const p = joinPath(dest, d.name);
            const self = moving.has(p);
            return (
              <li key={d.name}>
                <button type="button" disabled={self} onClick={() => setDest(p)} aria-label={t.openDir(d.name)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left hover:bg-muted disabled:opacity-50">
                  <Folder className="h-4 w-4 shrink-0" aria-hidden /><span className="break-all">{d.name}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}

function DeleteDialog({ share, recycle, paths, onClose, onDeleted }: {
  share: string; recycle: boolean; paths: string[]; onClose: () => void; onDeleted: (r: FileBatch) => void;
}) {
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function go() {
    setBusy(true); setErr(null);
    try {
      onDeleted(await api<FileBatch>(actionUrl(share, 'delete'), { method: 'POST', body: { paths } }));
      onClose();
    } catch (e) { setErr(apiMessage(e)); } finally { setBusy(false); }
  }
  return (
    <Dialog title={t.deleteTitle(paths.length)} onClose={onClose} busy={busy} footer={<>
      <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
      <Button type="button" variant="destructive" onClick={() => void go()} disabled={busy}>
        {busy && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{recycle ? t.deleteConfirm : t.deleteForeverConfirm}
      </Button>
    </>}>
      <p className={recycle ? '' : 'font-medium text-danger'}>{recycle ? t.deleteRecycle : t.deleteForever}</p>
      <ul className="max-h-40 list-disc overflow-y-auto pl-5">{paths.map((p) => <li key={p} className="break-all">{p}</li>)}</ul>
      {err && <p role="alert" className="text-danger">{err}</p>}
    </Dialog>
  );
}

/* ---------- browser ---------- */

function Crumbs({ root, crumbs, onPick }: { root: string; crumbs: { name: string; path: string }[]; onPick: (p: string) => void }) {
  return (
    <nav aria-label={t.crumbs}>
      <ol className="flex flex-wrap items-center gap-1 text-sm">
        <li><button type="button" className="break-all rounded px-1 font-medium underline-offset-2 hover:underline" onClick={() => onPick('')}>{root}</button></li>
        {crumbs.map((c, i) => (
          <li key={c.path} className="flex items-center gap-1">
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden />
            {i === crumbs.length - 1
              ? <span aria-current="page" className="break-all font-medium">{c.name}</span>
              : <button type="button" className="break-all rounded px-1 underline-offset-2 hover:underline" onClick={() => onPick(c.path)}>{c.name}</button>}
          </li>
        ))}
      </ol>
    </nav>
  );
}

type Modal = null | { kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'move' } | { kind: 'delete' };

function ShareBrowser({ share, up }: { share: FileShare; up: ReturnType<typeof useUploads> }) {
  const [path, setPath] = useState('');
  const [page, setPage] = useState<FileList | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [more, setMore] = useState(false);
  const [tick, setTick] = useState(0);
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [modal, setModal] = useState<Modal>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const rw = share.access === 'rw' && (page?.access ?? 'rw') === 'rw';
  const reload = useCallback(() => setTick((n) => n + 1), []);

  useEffect(() => {
    let live = true;
    setErr(null); setSel(new Set());
    api<FileList>(listUrl(share.name, path))
      .then((r) => { if (live) setPage(r); })
      .catch((e: unknown) => { if (live) { setPage(null); setErr(apiMessage(e)); } });
    return () => { live = false; };
  }, [share.name, path, tick]);

  // A finished upload into the open folder shows up without a manual refresh.
  const here = useRef({ share: share.name, path });
  here.current = { share: share.name, path };
  useEffect(() => { refreshOn.current = (s, d) => { if (s === here.current.share && d === here.current.path) reload(); }; }, [reload]);

  async function loadMore() {
    if (!page) return;
    setMore(true);
    try {
      const next = await api<FileList>(listUrl(share.name, path, page.offset + page.entries.length));
      setPage({ ...next, entries: [...page.entries, ...next.entries], offset: page.offset });
    } catch (e) { setErr(apiMessage(e)); } finally { setMore(false); }
  }

  const after = (r: FileBatch) => { setNotice(batchProblem(r)); reload(); };
  const toggle = (n: string) => setSel((s) => { const c = new Set(s); if (c.has(n)) c.delete(n); else c.add(n); return c; });
  const selectable = (page?.entries ?? []).filter((e) => !e.badName).map((e) => e.name);
  const allSel = selectable.length > 0 && selectable.every((n) => sel.has(n));
  const selected = [...sel];
  const shown = page ? page.offset + page.entries.length : 0;

  return (
    <div className="space-y-3">
      <Crumbs root={share.name} crumbs={breadcrumbs(path)} onPick={setPath} />
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" size="sm" onClick={reload}><RefreshCw className="h-4 w-4" aria-hidden />{t.refresh}</Button>
        {rw && <>
          <Button type="button" variant="outline" size="sm" onClick={() => setModal({ kind: 'mkdir' })}><FolderPlus className="h-4 w-4" aria-hidden />{t.newFolder}</Button>
          <Button type="button" size="sm" onClick={() => fileInput.current?.click()}><Upload className="h-4 w-4" aria-hidden />{t.upload}</Button>
          <input ref={fileInput} type="file" multiple className="hidden" tabIndex={-1} aria-hidden
            onChange={(e) => {
              // Uploads go to the folder that is open now, even if the user moves on.
              const f = Array.from(e.target.files ?? []); e.target.value = ''; if (f.length) up.add(share.name, path, f);
            }} />
        </>}
      </div>
      {rw && selected.length > 0 && (
        <div role="toolbar" aria-label={t.selected(selected.length)} className="flex flex-wrap items-center gap-2 rounded-lg bg-muted/60 p-2">
          <span className="text-sm font-medium">{t.selected(selected.length)}</span>
          {selected.length === 1 && <Button type="button" variant="outline" size="sm" onClick={() => setModal({ kind: 'rename', name: selected[0] ?? '' })}><Pencil className="h-4 w-4" aria-hidden />{t.rename}</Button>}
          <Button type="button" variant="outline" size="sm" onClick={() => setModal({ kind: 'move' })}><MoveRight className="h-4 w-4" aria-hidden />{t.move}</Button>
          <Button type="button" variant="outline" size="sm" className="text-danger" onClick={() => setModal({ kind: 'delete' })}><Trash2 className="h-4 w-4" aria-hidden />{t.delete}</Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setSel(new Set())}>{t.clear}</Button>
        </div>
      )}
      {!rw && <p role="note" className="text-sm text-muted-foreground">{t.readOnlyNote}</p>}
      {notice && <p role="alert" className="text-sm text-danger">{notice}</p>}
      {err ? <ErrorRetry msg={err} onRetry={reload} /> : !page ? <Loading /> : (
        <>
          {page.entries.length === 0 && <p className="text-muted-foreground">{t.emptyDir}</p>}
          {rw && page.entries.length > 0 && (
            <label className="flex items-center gap-2 px-2 text-sm text-muted-foreground">
              <input type="checkbox" className="h-4 w-4" checked={allSel}
                onChange={() => setSel(allSel ? new Set() : new Set(selectable))} />{t.selectAll}
            </label>
          )}
          <ul className="divide-y divide-border">
            {page.entries.map((e) => {
              const reason = disabledReason(e);
              const full = joinPath(path, e.name);
              const meta = e.type === 'file' ? `${formatBytes(e.size)} · ${formatWhen(e.modified)}` : formatWhen(e.modified);
              const Icon = e.type === 'dir' ? Folder : e.type === 'link' ? Link2 : File;
              const body = (
                <>
                  <Icon className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block break-all">{e.name}</span>
                    <span className="block break-words text-xs text-muted-foreground">{reason ?? meta}</span>
                  </span>
                </>
              );
              const cls = 'flex min-w-0 flex-1 items-start gap-2 rounded-md px-2 py-2 text-left';
              return (
                <li key={e.name} className="flex items-center gap-1">
                  {rw && <input type="checkbox" className="ml-2 h-4 w-4 shrink-0" aria-label={t.select(e.name)} disabled={e.badName}
                    checked={sel.has(e.name)} onChange={() => toggle(e.name)} />}
                  {reason ? <div className={`${cls} opacity-70`}>{body}</div>
                    : e.type === 'dir'
                      ? <button type="button" aria-label={t.openDir(e.name)} className={`${cls} hover:bg-muted`} onClick={() => setPath(full)}>{body}</button>
                      : <a href={fileDownloadUrl(share.name, full)} download aria-label={t.download(e.name)} className={`${cls} hover:bg-muted`}>{body}</a>}
                </li>
              );
            })}
          </ul>
          {page.truncated && <p role="note" className="text-sm text-muted-foreground">{t.truncated}</p>}
          {shown < page.total && (
            <Button type="button" variant="outline" size="sm" disabled={more} onClick={() => void loadMore()}>
              {more && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{t.loadMore}
            </Button>
          )}
        </>
      )}
      {modal?.kind === 'mkdir' && (
        <NameDialog title={t.mkdirTitle} initial="" submit={t.create} onClose={() => setModal(null)}
          onSubmit={async (name) => { await api(actionUrl(share.name, 'mkdir'), { method: 'POST', body: { path, name } }); reload(); }} />
      )}
      {modal?.kind === 'rename' && (
        <NameDialog title={t.renameTitle(modal.name)} initial={modal.name} submit={t.rename} onClose={() => setModal(null)}
          onSubmit={async (name) => { await api(actionUrl(share.name, 'rename'), { method: 'POST', body: { path: joinPath(path, modal.name), name } }); reload(); }} />
      )}
      {modal?.kind === 'move' && (
        <MoveDialog share={share.name} from={path} names={selected} onClose={() => setModal(null)} onMoved={after} />
      )}
      {modal?.kind === 'delete' && (
        <DeleteDialog share={share.name} recycle={share.recycle} paths={selected.map((n) => joinPath(path, n))}
          onClose={() => setModal(null)} onDeleted={after} />
      )}
    </div>
  );
}

// Set by the open browser; the upload queue calls it when an upload finishes.
const refreshOn: { current: (share: string, dir: string) => void } = { current: () => {} };

export function FilesPage() {
  const [data, setData] = useState<FileShareList | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const [current, setCurrent] = useState<string | null>(null);
  const up = useUploads((s, d) => refreshOn.current(s, d));

  useEffect(() => {
    let live = true;
    setErr(null);
    api<FileShareList>('/files')
      .then((r) => { if (live) { setData(r); setCurrent((c) => (c && r.shares.some((s) => s.name === c) ? c : r.shares[0]?.name ?? null)); } })
      .catch((e: unknown) => { if (live) setErr(e instanceof ApiError && e.status === 503 ? t.unavailable : t.loadError); });
    return () => { live = false; };
  }, [tick]);

  if (err) return <ErrorRetry msg={err} onRetry={() => setTick((n) => n + 1)} />;
  if (!data) return <Loading />;
  if (data.noAccount) return <p role="note" className="text-muted-foreground">{t.noAccount}</p>;
  if (data.shares.length === 0) return <p role="note" className="text-muted-foreground">{t.noShares}</p>;
  const share = data.shares.find((s) => s.name === current) ?? data.shares[0];
  if (!share) return null;
  return (
    <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <div className="space-y-4">
        <Widget id="files-shares" title={t.shares} icon={<Folder className="h-4 w-4" aria-hidden />}>
          <ul className="space-y-1" aria-label={t.pickShare}>
            {data.shares.map((s) => (
              <li key={s.name}>
                <button type="button" aria-current={s.name === share.name ? 'true' : undefined} onClick={() => setCurrent(s.name)}
                  className={`flex w-full flex-col rounded-md px-3 py-2 text-left text-sm ${s.name === share.name ? 'bg-primary/15 font-semibold ring-1 ring-inset ring-primary/30' : 'hover:bg-muted'}`}>
                  <span className="break-all">{s.name}</span>
                  <span className="flex flex-wrap gap-1 pt-0.5">
                    {s.access === 'ro' && <Badge>{t.readOnly}</Badge>}
                    {s.recycle && <Badge>{t.recycle}</Badge>}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </Widget>
        <UploadPanel up={up} />
      </div>
      <Widget id="files-browser" title={share.name} icon={<Folder className="h-4 w-4" aria-hidden />}>
        {share.description && <p className="mb-2 text-sm text-muted-foreground">{share.description}</p>}
        <ShareBrowser key={share.name} share={share} up={up} />
      </Widget>
    </div>
  );
}
