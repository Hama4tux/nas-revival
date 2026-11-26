import { Loader2, Usb } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { UsbDisk, UsbList, UsbPartition } from '../api/types';
import { cs } from '../i18n/cs';
import { formatBytes } from '../lib/format';
import { canEject, usbFsLine, usbState, usbTitle } from '../lib/usb';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Dialog } from './ui/dialog';

const t = cs.usb;

function message(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return t.unavailable;
    if (e.message && [400, 404, 409, 500].includes(e.status)) return e.message;
  }
  return cs.errors.generic;
}

function PartitionRow({ p, readOnly, busy, onAction }: {
  p: UsbPartition; readOnly: boolean; busy: boolean; onAction: (op: 'mount' | 'unmount', p: UsbPartition, ro?: boolean) => void;
}) {
  const st = usbState(p);
  return (
    <li className="flex flex-wrap items-start gap-2 py-2">
      <div className="min-w-0 flex-1">
        <p className="break-all text-sm font-medium">{p.name} · {formatBytes(p.sizeBytes)} · {usbFsLine(p)}</p>
        <p className={`break-words text-xs ${st === 'blocked' ? 'text-muted-foreground' : ''}`}>
          {st === 'mounted' && <>{t.mounted(p.mountPoint)}{p.readOnly && <> · <Badge>{t.mountedRo}</Badge></>}
            {p.totalBytes > 0 && <> · {t.used(formatBytes(p.usedBytes), formatBytes(p.totalBytes))}</>}</>}
          {st === 'blocked' && p.blocked}
          {st === 'ready' && <span className="text-muted-foreground">{t.notMounted}</span>}
        </p>
      </div>
      {!readOnly && st === 'ready' && (
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" disabled={busy} onClick={() => onAction('mount', p)}>{t.mount}</Button>
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => onAction('mount', p, true)}>{t.mountRo}</Button>
        </div>
      )}
      {!readOnly && st === 'mounted' && (
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => onAction('unmount', p)}>{t.unmount}</Button>
      )}
    </li>
  );
}

/** Storage → USB disks (W4.2): mount, unmount, safe eject. */
export function UsbPanel({ readOnly }: { readOnly: boolean }) {
  const [data, setData] = useState<UsbList | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [eject, setEject] = useState<UsbDisk | null>(null);

  const load = useCallback(async () => {
    try { setData(await api<UsbList>('/storage/usb')); setErr(null); } catch (e) { setErr(message(e)); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  async function run(path: string, body: object, done?: string) {
    setBusy(true); setErr(null); setNote(null);
    try {
      setData(await api<UsbList>(path, { method: 'POST', body }));
      if (done) setNote(done);
      return true;
    } catch (e) { setErr(message(e)); return false; } finally { setBusy(false); }
  }

  return (
    <Widget id="storage-usb" title={t.title} icon={<Usb className="h-4 w-4" aria-hidden />}>
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">{t.hint}</p>
        {err && <p role="alert" className="text-sm text-danger">{err}</p>}
        {note && <p role="status" className="text-sm text-success">{note}</p>}
        {!data ? (!err && <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{cs.common.loading}</p>)
          : data.disks.length === 0 ? <p className="text-sm text-muted-foreground">{t.none}</p> : (
            data.disks.map((d) => (
              <section key={d.id} aria-label={usbTitle(d)} className="rounded-lg border border-border p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="min-w-0 flex-1 break-words font-semibold">{usbTitle(d)} <span className="font-normal text-muted-foreground">· {d.name} · {formatBytes(d.sizeBytes)}</span></h3>
                  {!readOnly && (
                    <Button type="button" size="sm" variant="outline" disabled={busy || !canEject(d)} title={canEject(d) ? undefined : t.ejectBlocked}
                      onClick={() => setEject(d)}>{t.eject}</Button>
                  )}
                </div>
                <ul className="divide-y divide-border">
                  {d.partitions.map((p) => (
                    <PartitionRow key={p.name} p={p} readOnly={readOnly} busy={busy}
                      onAction={(op, part, ro) => void run(`/storage/usb/${op}`, { diskId: d.id, partition: part.name, ...(op === 'mount' ? { readOnly: !!ro } : {}) })} />
                  ))}
                </ul>
                {!canEject(d) && <p className="mt-1 text-xs text-muted-foreground">{t.ejectBlocked}</p>}
              </section>
            ))
          )}
      </div>
      {eject && (
        <Dialog title={t.ejectTitle(usbTitle(eject))} onClose={() => setEject(null)} busy={busy} footer={<>
          <Button type="button" variant="outline" onClick={() => setEject(null)} disabled={busy}>{cs.common.cancel}</Button>
          <Button type="button" disabled={busy} onClick={() => void run('/storage/usb/eject', { diskId: eject.id }, t.ejected).then(() => setEject(null))}>
            {busy && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{t.eject}
          </Button>
        </>}>
          <p>{t.ejectText}</p>
        </Dialog>
      )}
    </Widget>
  );
}
