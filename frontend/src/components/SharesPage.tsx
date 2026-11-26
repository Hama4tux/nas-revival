import { ChevronDown, ChevronUp, FolderOpen, Pencil, Plus, RefreshCw, Trash2, TriangleAlert, Eraser, X } from 'lucide-react';
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type {
  PrincipalKind, RecycleCleanupResult, Share, ShareAccess, ShareDeleteResult, ShareInfo, ShareList, SharePermission, ShareVolume,
} from '../api/types';
import { cs } from '../i18n/cs';
import { formatBytes } from '../lib/format';
import {
  accessChoices, addPermission, daysProblem, descriptionProblem, permissionsProblem, removePermission, shareNameProblem,
  shareValid, usersWithAccess,
} from '../lib/shares';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.shares;
const selectCls = 'h-10 w-full rounded-md border border-border bg-background/80 px-3 text-sm';

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if ([400, 404, 409, 500].includes(e.status)) return e.message || cs.errors.generic;
  }
  return cs.errors.generic;
}

/** Runs one request with busy/error state; the dialog closes only on success. */
function useAction() {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [field, setField] = useState<string | null>(null);
  async function run<T>(fn: () => Promise<T>, done: (r: T) => void) {
    setBusy(true); setErr(null); setField(null);
    try { done(await fn()); }
    catch (e) { setErr(apiMessage(e)); setField(e instanceof ApiError ? e.field : null); }
    finally { setBusy(false); }
  }
  return { busy, err, field, run };
}

function Footer({ busy, onClose, label, busyLabel, disabled, destructive }: {
  busy: boolean; onClose: () => void; label: string; busyLabel: string; disabled?: boolean; destructive?: boolean;
}) {
  return (
    <>
      <Button type="button" variant="outline" onClick={onClose} disabled={busy}>{cs.common.cancel}</Button>
      <Button type="submit" form="shares-form" variant={destructive ? 'destructive' : 'default'} disabled={busy || disabled}>
        {busy ? busyLabel : label}
      </Button>
    </>
  );
}

const volumeLabel = (v: ShareVolume) =>
  t.form.volumeFree(v.path, formatBytes(v.availBytes), formatBytes(v.totalBytes)) + (v.acl ? '' : ` (${t.noAcl})`);

const SmbNote = () => <p role="note" className="text-sm text-muted-foreground">{t.smbEnforced}</p>;

function Toggle({ label, hint, checked, onChange }: { label: string; hint: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex min-h-10 items-start gap-2 rounded-md px-2 py-1.5 hover:bg-muted">
      <input type="checkbox" className="mt-1 h-4 w-4" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span><span className="font-medium">{label}</span><span className="block text-sm text-muted-foreground">{hint}</span></span>
    </label>
  );
}

function PermissionsEditor({ value, onChange, principals, error }: {
  value: SharePermission[]; onChange: (v: SharePermission[]) => void; principals: ShareList['principals']; error: string | null;
}) {
  const [pick, setPick] = useState('');
  const free = (kind: PrincipalKind, names: string[]) => names.filter((n) => !value.some((p) => p.kind === kind && p.name === n));
  const users = free('user', principals.users);
  const groups = free('group', principals.groups);
  function add() {
    const [kind, ...rest] = pick.split(':');
    if (!pick || (kind !== 'user' && kind !== 'group')) return;
    onChange(addPermission(value, { kind, name: rest.join(':'), access: 'rw' }));
    setPick('');
  }
  const set = (p: SharePermission, access: ShareAccess) =>
    onChange(value.map((x) => (x.kind === p.kind && x.name === p.name ? { ...x, access } : x)));
  return (
    <fieldset className="space-y-2">
      <legend className="text-sm font-medium">{t.form.perms}</legend>
      <p className="text-xs text-muted-foreground">{t.form.permsHint}</p>
      {value.length === 0 ? <p className="text-muted-foreground">{t.form.permsEmpty}</p> : (
        <ul className="divide-y divide-border">
          {value.map((p) => (
            <li key={`${p.kind}:${p.name}`} className="flex flex-wrap items-center gap-2 py-2">
              <span className="min-w-0 flex-1 basis-32 break-all">
                <span className="font-medium">{p.name}</span>{' '}
                <span className="text-xs text-muted-foreground">({p.kind === 'group' ? t.form.kindGroup : t.form.kindUser})</span>
              </span>
              <select aria-label={t.form.accessOf(p.name)} className={`${selectCls} !w-auto min-w-36`} value={p.access}
                onChange={(e) => set(p, e.target.value as ShareAccess)}>
                {accessChoices(p.kind).map((a) => <option key={a} value={a}>{t.access[a]}</option>)}
              </select>
              <Button type="button" variant="outline" size="sm" aria-label={t.form.remove(p.name)}
                onClick={() => onChange(removePermission(value, p.kind, p.name))}>
                <X className="h-4 w-4" aria-hidden />
              </Button>
            </li>
          ))}
        </ul>
      )}
      {users.length + groups.length === 0 ? <p className="text-sm text-muted-foreground">{t.form.noPrincipals}</p> : (
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-0 flex-1 basis-48 space-y-1">
            <label htmlFor="share-add-principal" className="text-sm font-medium">{t.form.addLabel}</label>
            <select id="share-add-principal" className={selectCls} value={pick} onChange={(e) => setPick(e.target.value)}>
              <option value="">{t.form.addPlaceholder}</option>
              {groups.length > 0 && <optgroup label={t.form.groupsOpt}>{groups.map((g) => <option key={g} value={`group:${g}`}>{g}</option>)}</optgroup>}
              {users.length > 0 && <optgroup label={t.form.usersOpt}>{users.map((u) => <option key={u} value={`user:${u}`}>{u}</option>)}</optgroup>}
            </select>
          </div>
          <Button type="button" variant="outline" onClick={add} disabled={!pick}><Plus className="h-4 w-4" aria-hidden />{t.form.addBtn}</Button>
        </div>
      )}
      {error && <p role="alert" className="text-sm font-medium text-danger">{error}</p>}
    </fieldset>
  );
}

const blank = (volume: string): Share => ({
  name: '', description: '', volume, folder: '', readOnly: false, hidden: false,
  recycle: { enabled: true, days: 30 }, permissions: [],
});

function ShareDialog({ share, list, onClose, onDone }: {
  share: ShareInfo | null; list: ShareList; onClose: () => void; onDone: (l: ShareList, msg: string) => void;
}) {
  const [s, setS] = useState<Share>(share ?? blank(list.volumes[0]?.path ?? ''));
  const [daysText, setDaysText] = useState(String(s.recycle.days));
  const [nameTouched, setNameTouched] = useState(false);
  const a = useAction();
  const creating = share === null;
  const existing = list.shares.map((x) => x.name);
  const days = Number(daysText);
  const np = creating ? shareNameProblem(s.name, existing) : null;
  const draft: Share = { ...s, folder: creating ? s.name : s.folder, recycle: { enabled: s.recycle.enabled, days } };
  const permProblem = permissionsProblem(s.permissions);
  const valid = shareValid(draft, creating, existing);
  const fieldErr = (f: string) => (a.field === f ? a.err : null);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setNameTouched(true);
    if (!valid) return;
    const body: Share = { ...draft, description: draft.description.trim() };
    if (creating) {
      void a.run(() => api<ShareList>('/shares', { method: 'POST', body }), (l) => onDone(l, t.done.created(body.name)));
    } else {
      void a.run(() => api<ShareList>(`/shares/${body.name}`, { method: 'PUT', body }), (l) => onDone(l, t.done.saved));
    }
  }

  return (
    <Dialog title={creating ? t.form.createTitle : t.form.editTitle(s.name)} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={creating ? t.form.create : cs.common.save}
        busyLabel={creating ? t.form.creating : cs.common.saving} />}>
      <form id="shares-form" onSubmit={submit} className="space-y-3" noValidate>
        {creating ? (
          <>
            <Field label={t.form.name} hint={t.form.nameHint} error={(nameTouched && np ? t.form.nameErr[np] : null) ?? fieldErr('name')}>
              {(p) => <Input {...p} data-autofocus value={s.name} autoComplete="off" autoCapitalize="none" spellCheck={false}
                className={invalidCls} onChange={(e) => setS({ ...s, name: e.target.value })} onBlur={() => setNameTouched(true)} />}
            </Field>
            <Field label={t.form.volume} hint={t.form.volumeHint} error={fieldErr('volume')}>
              {(p) => (
                <select {...p} className={selectCls} value={s.volume} onChange={(e) => setS({ ...s, volume: e.target.value })}>
                  {list.volumes.map((v) => <option key={v.path} value={v.path}>{volumeLabel(v)}</option>)}
                </select>
              )}
            </Field>
          </>
        ) : (
          <>
            <p className="break-all text-muted-foreground">{t.form.folderInfo(s.volume, s.folder)}</p>
            {share.enforcement === 'smb' && <SmbNote />}
          </>
        )}
        <Field label={t.form.description} hint={t.form.descriptionHint}
          error={(descriptionProblem(s.description) ? t.form.descriptionErr : null) ?? fieldErr('description')}>
          {(p) => <Input {...p} value={s.description} autoComplete="off" className={invalidCls}
            data-autofocus={creating ? undefined : true} onChange={(e) => setS({ ...s, description: e.target.value })} />}
        </Field>
        <Toggle label={t.form.readOnly} hint={t.form.readOnlyHint} checked={s.readOnly} onChange={(v) => setS({ ...s, readOnly: v })} />
        <Toggle label={t.form.hidden} hint={t.form.hiddenHint} checked={s.hidden} onChange={(v) => setS({ ...s, hidden: v })} />
        <Toggle label={t.form.recycle} hint={t.form.recycleHint} checked={s.recycle.enabled}
          onChange={(v) => setS({ ...s, recycle: { ...s.recycle, enabled: v } })} />
        {s.recycle.enabled && (
          <Field label={t.form.days} error={(daysProblem(days) ? t.form.daysErr : null) ?? fieldErr('recycle')} className="max-w-40">
            {(p) => <Input {...p} type="number" inputMode="numeric" min={1} max={365} value={daysText} className={invalidCls}
              onChange={(e) => setDaysText(e.target.value)} />}
          </Field>
        )}
        <PermissionsEditor value={s.permissions} onChange={(v) => setS({ ...s, permissions: v })} principals={list.principals}
          error={(permProblem ? t.form.permsErr[permProblem] : null) ?? fieldErr('permissions')} />
        {a.err && (!a.field || !['name', 'volume', 'description', 'recycle', 'permissions'].includes(a.field)) && (
          <p role="alert" className="font-medium text-danger">{a.err}</p>
        )}
      </form>
    </Dialog>
  );
}

function DeleteDialog({ share, onClose, onDone }: {
  share: ShareInfo; onClose: () => void; onDone: (r: ShareDeleteResult) => void;
}) {
  const [withData, setWithData] = useState(false);
  const [confirm, setConfirm] = useState('');
  const a = useAction();
  const ok = !withData || confirm === share.name;
  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!ok) return;
    void a.run(() => api<ShareDeleteResult>(`/shares/${share.name}`, {
      method: 'DELETE', body: { deleteData: withData, confirmName: withData ? confirm : '' },
    }), onDone);
  }
  return (
    <Dialog title={t.del.title(share.name)} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={t.del.submit} busyLabel={t.del.submitting} disabled={!ok} destructive />}>
      <form id="shares-form" onSubmit={submit} className="space-y-3" noValidate>
        <fieldset className="space-y-1">
          <legend className="mb-1">{t.del.body}</legend>
          <label className="flex min-h-10 items-start gap-2 rounded-md px-2 py-1.5 hover:bg-muted">
            <input type="radio" name="del-mode" className="mt-1 h-4 w-4" checked={!withData} onChange={() => setWithData(false)} data-autofocus />
            <span><span className="font-medium">{t.del.configOnly}</span><span className="block text-sm text-muted-foreground">{t.del.configOnlyHint}</span></span>
          </label>
          <label className="flex min-h-10 items-start gap-2 rounded-md border border-transparent px-2 py-1.5 hover:bg-muted has-[:checked]:border-danger/50 has-[:checked]:bg-danger/10">
            <input type="radio" name="del-mode" className="mt-1 h-4 w-4" checked={withData} onChange={() => setWithData(true)} />
            <span><span className="font-medium text-danger">{t.del.withData}</span><span className="block text-sm text-muted-foreground">{t.del.withDataHint}</span></span>
          </label>
        </fieldset>
        {withData && (
          <Field label={t.del.confirm} error={a.field === 'confirmName' ? a.err : null}>
            {(p) => <Input {...p} value={confirm} autoComplete="off" autoCapitalize="none" spellCheck={false} className={invalidCls}
              placeholder={share.name} onChange={(e) => setConfirm(e.target.value)} />}
          </Field>
        )}
        {a.err && a.field !== 'confirmName' && <p role="alert" className="font-medium text-danger">{a.err}</p>}
      </form>
    </Dialog>
  );
}

function Detail({ s }: { s: ShareInfo }) {
  return (
    <div className="space-y-2 rounded-md bg-muted/40 p-3">
      <h3 className="text-sm font-semibold">{t.matrix.title}</h3>
      {s.enforcement === 'smb' && <SmbNote />}
      {s.conflicts.length > 0 && (
        <ul aria-label={t.conflicts} className="space-y-1 text-sm">
          {s.conflicts.map((c) => (
            <li key={c} className="flex items-start gap-2 text-warning">
              <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" aria-hidden /><span>{c}</span>
            </li>
          ))}
        </ul>
      )}
      {s.effective.length === 0 ? <p className="text-sm text-muted-foreground">{t.matrix.empty}</p> : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">{t.matrix.title}</caption>
            <thead className="text-xs text-muted-foreground">
              <tr><th scope="col" className="py-1 pr-3 font-medium">{t.matrix.user}</th>
                <th scope="col" className="py-1 pr-3 font-medium">{t.matrix.access}</th>
                <th scope="col" className="py-1 font-medium">{t.matrix.via}</th></tr>
            </thead>
            <tbody className="divide-y divide-border">
              {s.effective.map((e) => (
                <tr key={e.user}>
                  <th scope="row" className="py-1 pr-3 font-medium break-all">{e.user}</th>
                  <td className={`py-1 pr-3 ${e.access === 'none' ? 'text-muted-foreground' : ''}`}>{t.access[e.access]}</td>
                  <td className="py-1 text-muted-foreground">
                    {e.via === 'user' ? t.via.user : e.via.startsWith('group:') ? t.viaGroup(e.via.slice(6)) : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function Action({ icon, label, onClick, expanded }: { icon: ReactNode; label: string; onClick: () => void; expanded?: boolean }) {
  return (
    <Button type="button" variant="outline" size="sm" onClick={onClick} aria-expanded={expanded}>
      <span aria-hidden className="[&>svg]:h-4 [&>svg]:w-4">{icon}</span>{label}
    </Button>
  );
}

function ShareRow({ s, readOnly, expanded, onToggle, onEdit, onDelete }: {
  s: ShareInfo; readOnly: boolean; expanded: boolean; onToggle: () => void; onEdit: () => void; onDelete: () => void;
}) {
  return (
    <li className="space-y-2 py-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="font-medium break-all">{s.name}</span>
        {s.readOnly && <Badge>{t.badges.readOnly}</Badge>}
        {s.hidden && <Badge>{t.badges.hidden}</Badge>}
        {s.recycle.enabled && <Badge>{t.badges.recycle(s.recycle.days)}</Badge>}
        {!s.exists && <Badge variant="warning">{t.badges.missing}</Badge>}
        <span className="ml-auto text-xs text-muted-foreground">{t.usersCount(usersWithAccess(s.effective))}</span>
      </div>
      {s.description && <p className="text-sm break-words">{s.description}</p>}
      <p className="text-sm text-muted-foreground break-all">{s.path}</p>
      <div className="flex flex-wrap gap-2" role="group" aria-label={t.actions(s.name)}>
        <Action icon={expanded ? <ChevronUp /> : <ChevronDown />} label={expanded ? t.hideDetail : t.detail} onClick={onToggle} expanded={expanded} />
        {!readOnly && <Action icon={<Pencil />} label={t.edit} onClick={onEdit} />}
        {!readOnly && <Action icon={<Trash2 />} label={t.delete} onClick={onDelete} />}
      </div>
      {expanded && <Detail s={s} />}
    </li>
  );
}

type Open = { kind: 'create' } | { kind: 'edit'; share: ShareInfo } | { kind: 'delete'; share: ShareInfo };

export function SharesPage({ readOnly }: { readOnly: boolean }) {
  const [data, setData] = useState<ShareList | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState<Open | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [rowErr, setRowErr] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [cleaning, setCleaning] = useState(false);

  const load = useCallback(async () => {
    try { setData(await api<ShareList>('/shares')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const close = () => setOpen(null);
  const done = (l: ShareList, m: string) => { setData(l); setOpen(null); setMsg(m); };
  const show = (o: Open) => { setMsg(null); setRowErr(null); setOpen(o); };

  async function cleanup() {
    setMsg(null); setRowErr(null); setCleaning(true);
    try {
      const r = await api<RecycleCleanupResult>('/shares/recycle-cleanup', { method: 'POST' });
      setMsg(t.recycled(r.removedFiles, formatBytes(r.removedBytes)));
    } catch (e) { setRowErr(apiMessage(e)); }
    finally { setCleaning(false); }
  }

  if (!data) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }

  return (
    <div className="space-y-4">
      <Widget id="shares-list" title={t.listTitle} icon={<FolderOpen />}
        right={<div className="flex flex-wrap items-center justify-end gap-2">
          {!readOnly && (
            <Button type="button" variant="outline" size="sm" onClick={() => void cleanup()} disabled={cleaning}>
              <Eraser className="h-4 w-4" aria-hidden />{cleaning ? t.recycling : t.recycleNow}
            </Button>
          )}
          {!readOnly && (
            <Button type="button" size="sm" onClick={() => show({ kind: 'create' })} disabled={data.volumes.length === 0}>
              <Plus className="h-4 w-4" aria-hidden />{t.add}
            </Button>
          )}
          <Button type="button" variant="outline" size="sm" aria-label={t.refresh} onClick={() => { setMsg(null); void load(); }}>
            <RefreshCw className="h-4 w-4" aria-hidden />
          </Button>
        </div>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        {!data.smb.installed && <p role="note" className="mt-2 text-sm text-warning">{t.sambaMissing}</p>}
        {data.smb.installed && !data.smb.included && <p role="note" className="mt-2 text-sm text-warning">{t.sambaNotIncluded}</p>}
        {data.volumes.length === 0 && <p role="note" className="mt-2 text-sm text-warning">{t.noVolumes}</p>}
        {msg && <p role="status" className="mt-3 text-sm text-success">{msg}</p>}
        {rowErr && <p role="alert" className="mt-3 text-sm font-medium text-danger">{rowErr}</p>}
        {data.shares.length === 0 ? <p className="mt-3 text-sm">{t.empty}</p> : (
          <ul className="mt-2 divide-y divide-border">
            {data.shares.map((s) => (
              <ShareRow key={s.name} s={s} readOnly={readOnly} expanded={expanded === s.name}
                onToggle={() => setExpanded(expanded === s.name ? null : s.name)}
                onEdit={() => show({ kind: 'edit', share: s })} onDelete={() => show({ kind: 'delete', share: s })} />
            ))}
          </ul>
        )}
      </Widget>

      {open?.kind === 'create' && <ShareDialog share={null} list={data} onClose={close} onDone={done} />}
      {open?.kind === 'edit' && <ShareDialog share={open.share} list={data} onClose={close} onDone={done} />}
      {open?.kind === 'delete' && (
        <DeleteDialog share={open.share} onClose={close}
          onDone={(r) => done(r, r.dataRemoval === 'started' ? t.done.deletedData(open.share.name) : t.done.deleted(open.share.name))} />
      )}
    </div>
  );
}
