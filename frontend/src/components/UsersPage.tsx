import { KeyRound, Pencil, Plus, RefreshCw, Trash2, TriangleAlert, UserCheck, UserX, Users, UsersRound } from 'lucide-react';
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type { GroupInfo, UserInfo, UserList } from '../api/types';
import { cs } from '../i18n/cs';
import { fullNameProblem, nameProblem, passwordIssues, passwordProblem, toggle } from '../lib/users';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.users;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 404 || e.status === 409 || e.status === 500) return e.message || cs.errors.generic;
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
      <Button type="submit" form="users-form" variant={destructive ? 'destructive' : 'default'} disabled={busy || disabled}>
        {busy ? busyLabel : label}
      </Button>
    </>
  );
}

function Checks({ legend, items, selected, onChange, empty }: {
  legend: string; items: string[]; selected: string[]; onChange: (v: string[]) => void; empty: string;
}) {
  return (
    <fieldset className="space-y-1">
      <legend className="text-sm font-medium">{legend}</legend>
      {items.length === 0 ? <p className="text-muted-foreground">{empty}</p> : (
        <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
          {items.map((g) => (
            <label key={g} className="flex min-h-10 items-center gap-2 rounded-md px-2 hover:bg-muted">
              <input type="checkbox" className="h-4 w-4" checked={selected.includes(g)}
                onChange={(e) => onChange(toggle(selected, g, e.target.checked))} />
              <span className="break-all">{g}</span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}

function PasswordFields({ pw, repeat, setPw, setRepeat, serverErr }: {
  pw: string; repeat: string; setPw: (s: string) => void; setRepeat: (s: string) => void; serverErr: string | null;
}) {
  const [touched, setTouched] = useState(false);
  const problem = passwordProblem(pw, repeat);
  const pwErr = touched && problem && problem !== 'mismatch' ? t.form.pwErr[problem] : serverErr;
  const repErr = touched && repeat && problem === 'mismatch' ? t.form.pwErr.mismatch : null;
  return (
    <>
      <Field label={t.form.password} hint={t.form.passwordHint} error={pwErr}>
        {(p) => <Input {...p} type="password" autoComplete="new-password" value={pw} className={invalidCls}
          onChange={(e) => setPw(e.target.value)} onBlur={() => setTouched(true)} />}
      </Field>
      <Field label={t.form.repeat} error={repErr}>
        {(p) => <Input {...p} type="password" autoComplete="new-password" value={repeat} className={invalidCls}
          onChange={(e) => { setRepeat(e.target.value); setTouched(true); }} />}
      </Field>
    </>
  );
}

function UserDialog({ user, groups, onClose, onDone }: {
  user: UserInfo | null; groups: GroupInfo[]; onClose: () => void; onDone: (l: UserList, msg: string) => void;
}) {
  const [name, setName] = useState('');
  const [fullName, setFullName] = useState(user?.fullName ?? '');
  const [pw, setPw] = useState('');
  const [repeat, setRepeat] = useState('');
  const [sel, setSel] = useState<string[]>(user?.groups ?? []);
  const [nameTouched, setNameTouched] = useState(false);
  const a = useAction();
  const editable = groups.filter((g) => !g.builtin).map((g) => g.name);
  const np = user ? null : nameProblem(name);
  const fnBad = fullNameProblem(fullName);
  const valid = !np && !fnBad && (user !== null || passwordProblem(pw, repeat) === null);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setNameTouched(true);
    if (!valid) return;
    if (user) {
      void a.run(() => api<UserList>(`/users/${user.name}`, { method: 'PUT', body: { fullName, groups: sel } }),
        (l) => onDone(l, t.done.saved));
    } else {
      void a.run(() => api<UserList>('/users', { method: 'POST', body: { name, fullName, password: pw, groups: sel } }),
        (l) => onDone(l, t.done.created(name)));
    }
  }

  const fieldErr = (f: string) => (a.field === f ? a.err : null);
  return (
    <Dialog title={user ? t.form.editTitle(user.name) : t.form.createTitle} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={user ? cs.common.save : t.form.create}
        busyLabel={user ? cs.common.saving : t.form.creating} />}>
      <form id="users-form" onSubmit={submit} className="space-y-3" noValidate>
        {!user && (
          <Field label={t.form.name} hint={t.form.nameHint}
            error={(nameTouched && np ? t.form.nameErr[np] : null) ?? fieldErr('name')}>
            {(p) => <Input {...p} data-autofocus value={name} autoComplete="off" autoCapitalize="none" spellCheck={false}
              className={invalidCls} onChange={(e) => setName(e.target.value)} onBlur={() => setNameTouched(true)} />}
          </Field>
        )}
        <Field label={t.form.fullName} hint={t.form.fullNameHint} error={(fnBad ? t.form.fullNameErr : null) ?? fieldErr('fullName')}>
          {(p) => <Input {...p} value={fullName} autoComplete="off" className={invalidCls} onChange={(e) => setFullName(e.target.value)} />}
        </Field>
        {!user && <PasswordFields pw={pw} repeat={repeat} setPw={setPw} setRepeat={setRepeat} serverErr={fieldErr('password')} />}
        <Checks legend={t.form.groups} items={editable} selected={sel} onChange={setSel} empty={t.form.noGroups} />
        {a.err && (!a.field || a.field === 'groups') && <p role="alert" className="font-medium text-danger">{a.err}</p>}
      </form>
    </Dialog>
  );
}

function PasswordDialog({ user, onClose, onDone }: { user: UserInfo; onClose: () => void; onDone: (msg: string) => void }) {
  const [pw, setPw] = useState('');
  const [repeat, setRepeat] = useState('');
  const a = useAction();
  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (passwordProblem(pw, repeat)) return;
    void a.run(() => api(`/users/${user.name}/password`, { method: 'PUT', body: { password: pw } }),
      () => onDone(t.pw.done(user.name)));
  }
  return (
    <Dialog title={t.pw.title(user.name)} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={t.pw.save} busyLabel={t.pw.saving}
        disabled={passwordProblem(pw, repeat) !== null} />}>
      <form id="users-form" onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-muted-foreground">{t.pw.body}</p>
        <PasswordFields pw={pw} repeat={repeat} setPw={setPw} setRepeat={setRepeat} serverErr={a.field === 'password' ? a.err : null} />
        {a.err && a.field !== 'password' && <p role="alert" className="font-medium text-danger">{a.err}</p>}
      </form>
    </Dialog>
  );
}

function ConfirmDialog({ title, body, label, busyLabel, onClose, action, onDone }: {
  title: string; body: string; label: string; busyLabel: string; onClose: () => void;
  action: () => Promise<UserList>; onDone: (l: UserList) => void;
}) {
  const a = useAction();
  return (
    <Dialog title={title} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={label} busyLabel={busyLabel} destructive />}>
      <form id="users-form" onSubmit={(e) => { e.preventDefault(); void a.run(action, onDone); }}>
        <p>{body}</p>
        {a.err && <p role="alert" className="mt-3 font-medium text-danger">{a.err}</p>}
      </form>
    </Dialog>
  );
}

function GroupDialog({ onClose, onDone }: { onClose: () => void; onDone: (l: UserList, msg: string) => void }) {
  const [name, setName] = useState('');
  const [touched, setTouched] = useState(false);
  const a = useAction();
  const np = nameProblem(name);
  function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (np) return;
    void a.run(() => api<UserList>('/groups', { method: 'POST', body: { name } }), (l) => onDone(l, t.done.groupCreated(name)));
  }
  return (
    <Dialog title={t.group.createTitle} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={t.group.create} busyLabel={t.form.creating} />}>
      <form id="users-form" onSubmit={submit} className="space-y-3" noValidate>
        <Field label={t.group.name} hint={t.form.nameHint} error={(touched && np ? t.form.nameErr[np] : null) ?? a.err}>
          {(p) => <Input {...p} data-autofocus value={name} autoComplete="off" autoCapitalize="none" spellCheck={false}
            className={invalidCls} onChange={(e) => setName(e.target.value)} onBlur={() => setTouched(true)} />}
        </Field>
      </form>
    </Dialog>
  );
}

function MembersDialog({ group, users, onClose, onDone }: {
  group: GroupInfo; users: UserInfo[]; onClose: () => void; onDone: (l: UserList, msg: string) => void;
}) {
  const [sel, setSel] = useState(group.members);
  const a = useAction();
  return (
    <Dialog title={t.group.membersTitle(group.name)} onClose={onClose} busy={a.busy}
      footer={<Footer busy={a.busy} onClose={onClose} label={cs.common.save} busyLabel={cs.common.saving} />}>
      <form id="users-form" className="space-y-3" onSubmit={(e) => {
        e.preventDefault();
        void a.run(() => api<UserList>(`/groups/${group.name}/members`, { method: 'PUT', body: { members: sel } }),
          (l) => onDone(l, t.done.saved));
      }}>
        <Checks legend={t.members} items={users.map((u) => u.name)} selected={sel} onChange={setSel} empty={t.group.noUsers} />
        {a.err && <p role="alert" className="font-medium text-danger">{a.err}</p>}
      </form>
    </Dialog>
  );
}

type Open =
  | { kind: 'create' } | { kind: 'edit'; user: UserInfo } | { kind: 'password'; user: UserInfo }
  | { kind: 'delete'; user: UserInfo } | { kind: 'disable'; user: UserInfo }
  | { kind: 'group' } | { kind: 'members'; group: GroupInfo } | { kind: 'groupDelete'; group: GroupInfo };

function Action({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) {
  return (
    <Button type="button" variant="outline" size="sm" onClick={onClick}>
      <span aria-hidden className="[&>svg]:h-4 [&>svg]:w-4">{icon}</span>{label}
    </Button>
  );
}

function UserRow({ u, setOpen, onEnable }: { u: UserInfo; setOpen: (o: Open) => void; onEnable: () => void }) {
  const issues = passwordIssues(u);
  return (
    <li className="space-y-2 py-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="font-medium break-all">{u.name}</span>
        {u.fullName && <span className="text-muted-foreground break-all">{u.fullName}</span>}
        {u.disabled && <Badge variant="warning">{t.disabled}</Badge>}
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t.uid(u.uid)}</span>
      </div>
      <p className="text-sm text-muted-foreground">
        {u.groups.length ? u.groups.join(', ') : t.noGroupsOfUser}
      </p>
      {issues.length > 0 && (
        <ul className="space-y-1 text-sm">
          {issues.map((i) => (
            <li key={i} className="flex items-center gap-2 text-warning">
              <TriangleAlert className="h-4 w-4 shrink-0" aria-hidden />{t.issues[i]}
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap gap-2" role="group" aria-label={t.actions(u.name)}>
        <Action icon={<Pencil />} label={t.edit} onClick={() => setOpen({ kind: 'edit', user: u })} />
        <Action icon={<KeyRound />} label={t.password} onClick={() => setOpen({ kind: 'password', user: u })} />
        {u.disabled
          ? <Action icon={<UserCheck />} label={t.enable} onClick={onEnable} />
          : <Action icon={<UserX />} label={t.disable} onClick={() => setOpen({ kind: 'disable', user: u })} />}
        <Action icon={<Trash2 />} label={t.delete} onClick={() => setOpen({ kind: 'delete', user: u })} />
      </div>
    </li>
  );
}

export function UsersPage() {
  const [data, setData] = useState<UserList | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState<Open | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [rowErr, setRowErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    try { setData(await api<UserList>('/users')); setFailed(false); }
    catch { setFailed(true); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const close = () => setOpen(null);
  const done = (l: UserList, m: string) => { setData(l); setOpen(null); setMsg(m); };

  async function enable(u: UserInfo) {
    setMsg(null); setRowErr(null);
    try { done(await api<UserList>(`/users/${u.name}/disabled`, { method: 'PUT', body: { disabled: false } }), t.done.enabled(u.name)); }
    catch (e) { setRowErr(apiMessage(e)); }
  }
  const show = (o: Open) => { setMsg(null); setRowErr(null); setOpen(o); };

  const refresh = (
    <Button type="button" variant="outline" size="sm" onClick={() => { setMsg(null); void load(); }} aria-label={t.refresh}>
      <RefreshCw className="h-4 w-4" aria-hidden />
    </Button>
  );

  if (!data) {
    return failed
      ? <p role="alert" className="text-danger">{t.loadError} <Button variant="outline" size="sm" onClick={() => void load()}>{cs.common.retry}</Button></p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-48 w-full" /></div>;
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[3fr_2fr]">
      <Widget id="users-list" title={t.usersTitle} icon={<Users />}
        right={<div className="flex items-center gap-2">
          <Button type="button" size="sm" onClick={() => show({ kind: 'create' })}><Plus className="h-4 w-4" aria-hidden />{t.add}</Button>
          {refresh}
        </div>}>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        {!data.samba && <p role="note" className="mt-2 text-sm text-muted-foreground">{t.sambaMissing}</p>}
        <p className="mt-2 text-sm text-muted-foreground">{t.webLoginLater}</p>
        {msg && <p role="status" className="mt-3 text-sm text-success">{msg}</p>}
        {rowErr && <p role="alert" className="mt-3 text-sm font-medium text-danger">{rowErr}</p>}
        {data.users.length === 0 ? <p className="mt-3 text-sm">{t.empty}</p> : (
          <ul className="mt-2 divide-y divide-border">
            {data.users.map((u) => <UserRow key={u.name} u={u} setOpen={show} onEnable={() => void enable(u)} />)}
          </ul>
        )}
      </Widget>

      <Widget id="groups-list" title={t.groupsTitle} icon={<UsersRound />}
        right={<Button type="button" size="sm" variant="outline" onClick={() => show({ kind: 'group' })}>
          <Plus className="h-4 w-4" aria-hidden />{t.addGroup}
        </Button>}>
        {data.groups.filter((g) => !g.builtin).length === 0 && <p className="mb-2 text-sm">{t.noGroups}</p>}
        <ul className="divide-y divide-border">
          {data.groups.map((g) => (
            <li key={g.name} className="space-y-2 py-3">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span className="font-medium break-all">{g.name}</span>
                {g.builtin && <Badge>{t.builtin}</Badge>}
                <span className="text-sm text-muted-foreground">{t.memberCount(g.members.length)}</span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t.gid(g.gid)}</span>
              </div>
              <p className="text-sm text-muted-foreground break-words">
                {g.builtin ? t.builtinHint : g.members.length ? g.members.join(', ') : t.noMembers}
              </p>
              {!g.builtin && (
                <div className="flex flex-wrap gap-2" role="group" aria-label={t.actions(g.name)}>
                  <Action icon={<Users />} label={t.members} onClick={() => show({ kind: 'members', group: g })} />
                  <Action icon={<Trash2 />} label={t.delete} onClick={() => show({ kind: 'groupDelete', group: g })} />
                </div>
              )}
            </li>
          ))}
        </ul>
      </Widget>

      {open?.kind === 'create' && <UserDialog user={null} groups={data.groups} onClose={close} onDone={done} />}
      {open?.kind === 'edit' && <UserDialog user={open.user} groups={data.groups} onClose={close} onDone={done} />}
      {open?.kind === 'password' && (
        <PasswordDialog user={open.user} onClose={close} onDone={(m) => { setOpen(null); setMsg(m); void load(); }} />
      )}
      {open?.kind === 'disable' && (
        <ConfirmDialog title={t.confirm.disableTitle(open.user.name)} body={t.confirm.disableBody} label={t.disable}
          busyLabel={t.confirm.working} onClose={close}
          action={() => api<UserList>(`/users/${open.user.name}/disabled`, { method: 'PUT', body: { disabled: true } })}
          onDone={(l) => done(l, t.done.disabled(open.user.name))} />
      )}
      {open?.kind === 'delete' && (
        <ConfirmDialog title={t.confirm.deleteTitle(open.user.name)} body={t.confirm.deleteBody} label={t.delete}
          busyLabel={t.confirm.deleting} onClose={close}
          action={() => api<UserList>(`/users/${open.user.name}`, { method: 'DELETE' })}
          onDone={(l) => done(l, t.done.deleted(open.user.name))} />
      )}
      {open?.kind === 'group' && <GroupDialog onClose={close} onDone={done} />}
      {open?.kind === 'members' && <MembersDialog group={open.group} users={data.users} onClose={close} onDone={done} />}
      {open?.kind === 'groupDelete' && (
        <ConfirmDialog title={t.confirm.groupDeleteTitle(open.group.name)} body={t.confirm.groupDeleteBody} label={t.delete}
          busyLabel={t.confirm.deleting} onClose={close}
          action={() => api<UserList>(`/groups/${open.group.name}`, { method: 'DELETE' })}
          onDone={(l) => done(l, t.done.groupDeleted(open.group.name))} />
      )}
    </div>
  );
}
