import { AlertTriangle, Power, RotateCcw } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SystemPowerAction, SystemPowerPreview } from '../api/types';
import { cs } from '../i18n/cs';
import { confirmMatches } from '../lib/systemPower';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { Field } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.sysPower;
const iconBtn = 'flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-sm hover:bg-muted aria-expanded:bg-muted';

/** Quick actions in the top bar: restart and shut down (CORE-12). */
export function PowerMenu({ onStarted }: { onStarted: (a: SystemPowerAction) => void }) {
  const [open, setOpen] = useState(false);
  const [action, setAction] = useState<SystemPowerAction | null>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const first = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    first.current?.focus();
    const down = (e: MouseEvent) => { if (!wrap.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', down);
    return () => document.removeEventListener('mousedown', down);
  }, [open]);

  function pick(a: SystemPowerAction) { setOpen(false); setAction(a); }
  const item = 'flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted';

  return (
    <div ref={wrap} className="relative"
      onKeyDown={(e) => { if (e.key === 'Escape' && open) { setOpen(false); btn.current?.focus(); } }}>
      <button ref={btn} type="button" className={iconBtn} aria-haspopup="menu" aria-expanded={open}
        aria-label={t.menu} title={t.menu} onClick={() => setOpen((v) => !v)}>
        <Power className="h-5 w-5" aria-hidden />
      </button>
      {open && (
        <div role="menu" aria-label={t.menu} className="pop glass absolute right-0 top-11 z-50 w-44 rounded-lg p-1 text-foreground">
          <button ref={first} role="menuitem" type="button" className={item} onClick={() => pick('reboot')}>
            <RotateCcw className="h-4 w-4" aria-hidden />{t.reboot}…
          </button>
          <button role="menuitem" type="button" className={`${item} text-danger`} onClick={() => pick('shutdown')}>
            <Power className="h-4 w-4" aria-hidden />{t.shutdown}…
          </button>
        </div>
      )}
      {action && <PowerDialog action={action} onClose={() => { setAction(null); btn.current?.focus(); }} onStarted={onStarted} />}
    </div>
  );
}

/** Preflight dialog: effects, running work (CORE-13), typed NAS name, second confirmation to force. */
function PowerDialog({ action, onClose, onStarted }: {
  action: SystemPowerAction; onClose: () => void; onStarted: (a: SystemPowerAction) => void;
}) {
  const [pv, setPv] = useState<SystemPowerPreview | null>(null);
  const [failed, setFailed] = useState(false);
  const [name, setName] = useState('');
  const [force, setForce] = useState(false);
  const [sending, setSending] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  const load = useCallback(async () => {
    setFailed(false);
    try {
      setPv(await api<SystemPowerPreview>(`/system/${action}/preview`, { method: 'POST' }));
      setForce(false);
    } catch { setFailed(true); }
  }, [action]);
  useEffect(() => { void load(); }, [load]);

  const blocked = (pv?.blockers.length ?? 0) > 0;
  const ready = !!pv && confirmMatches(name, pv.hostname) && (!blocked || force);

  async function run() {
    if (!pv || !ready) return;
    setSending(true); setMsg(null);
    try {
      await api(`/system/${action}`, { method: 'POST', body: { confirmToken: pv.confirmToken, force: blocked && force } });
      onStarted(action);
    } catch (e) {
      setSending(false);
      if (e instanceof ApiError && e.status === 409) {
        setMsg(e.code === 'blocked' ? t.blocked : e.code === 'power_pending' ? t.alreadyRunning : t.expired);
        if (e.code !== 'power_pending') await load();
        return;
      }
      setMsg(e instanceof ApiError && e.status === 503 ? cs.errors.helperDown
        : e instanceof ApiError && e.status === 0 ? cs.errors.network : cs.errors.generic);
    }
  }

  return (
    <Dialog title={t.title[action] ?? ''} onClose={onClose} busy={sending}
      footer={<>
        <Button type="button" variant="outline" onClick={onClose} disabled={sending}>{cs.common.cancel}</Button>
        <Button type="submit" form="power-form" variant="destructive" disabled={!ready || sending}>
          {sending ? t.sending : t.confirm[action]}
        </Button>
      </>}>
      {!pv && !failed && <div aria-busy="true" aria-label={t.checking}><Skeleton className="h-24 w-full" /></div>}
      {failed && (
        <div role="alert" className="space-y-2">
          <p className="text-danger">{t.loadError}</p>
          <Button type="button" size="sm" variant="outline" onClick={() => void load()}>{cs.common.retry}</Button>
        </div>
      )}
      {pv && (
        <form id="power-form" className="space-y-4" onSubmit={(e) => { e.preventDefault(); void run(); }}>
          {blocked && (
            <div role="alert" className="space-y-2 rounded-lg border border-warning/50 bg-warning/10 p-3">
              <p className="flex items-center gap-2 font-semibold">
                <AlertTriangle className="h-4 w-4 shrink-0 text-warning" aria-hidden />{t.blockersTitle}
              </p>
              <ul className="list-disc space-y-1 pl-5">
                {pv.blockers.map((b, i) => (
                  <li key={i} className="break-words">{t.blockers[b.code]?.(b.diskId) ?? b.code}</li>
                ))}
              </ul>
              <label className="flex items-start gap-2 pt-1">
                <input type="checkbox" className="mt-1" checked={force} onChange={(e) => setForce(e.target.checked)} disabled={sending} />
                <span>{t.force}</span>
              </label>
            </div>
          )}
          <div>
            <p className="mb-1 font-medium">{t.effectsTitle}</p>
            <ul className="list-disc space-y-1 pl-5 text-muted-foreground">
              {pv.effects.map((e) => <li key={e}>{t.effects[e] ?? e}</li>)}
            </ul>
          </div>
          <Field label={t.typeNameLabel} hint={t.typeName(pv.hostname)}>
            {(p) => (
              <Input {...p} autoFocus autoComplete="off" autoCapitalize="none" spellCheck={false}
                value={name} onChange={(e) => setName(e.target.value)} disabled={sending} />
            )}
          </Field>
          {msg && <p role="alert" className="text-danger">{msg}</p>}
        </form>
      )}
    </Dialog>
  );
}
