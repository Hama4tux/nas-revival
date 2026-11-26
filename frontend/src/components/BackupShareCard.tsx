import { FolderOpen } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { BackupShare } from '../api/types';
import { cs } from '../i18n/cs';
import { Button } from './ui/button';
import { Widget } from './ui/card';

const s = cs.tasks.backup.share;

/** SMB share (read-only, admins) of the backup versions folder. */
export function BackupShareCard({ readOnly }: { readOnly: boolean }) {
  const [share, setShare] = useState<BackupShare | null>(null);
  const [failed, setFailed] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api<BackupShare>('/backup/share').then((r) => { if (live) setShare(r); }).catch(() => { if (live) setFailed(true); });
    return () => { live = false; };
  }, []);

  async function put(enabled: boolean) {
    setBusy(true); setErr(null);
    try { setShare(await api<BackupShare>('/backup/share', { method: 'PUT', body: { enabled } })); setConfirm(false); }
    catch (e) { setErr(e instanceof ApiError && e.status !== 0 && e.status !== 503 && e.message ? e.message : s.saveError); }
    setBusy(false);
  }
  const unc = share ? `\\\\${window.location.hostname}\\${share.name}` : '';
  return (
    <Widget id="backup-share" title={s.title} icon={<FolderOpen />}>
      {failed ? <p role="alert" className="text-sm text-danger">{s.loadError}</p> : !share ? null : (
        <div className="space-y-3 text-sm">
          <label className="flex items-center gap-2">
            <input type="checkbox" role="switch" className="h-4 w-4" checked={share.enabled} disabled={readOnly || busy}
              onChange={(e) => (e.target.checked ? setConfirm(true) : void put(false))} />
            <span className="font-medium">{s.switch}</span>
          </label>
          {share.enabled ? <p className="break-all font-mono">{s.on(unc)}</p> : <p className="text-muted-foreground">{s.off}</p>}
          <p className="text-muted-foreground">{s.info}</p>
          {readOnly && <p role="note" className="text-muted-foreground">{s.readOnly}</p>}
          {confirm && (
            <div role="alertdialog" aria-label={s.confirm} className="space-y-2 rounded-md border border-border p-3">
              <p className="font-medium">{s.confirm}</p>
              <div className="flex flex-wrap gap-2">
                <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setConfirm(false)}>{cs.common.cancel}</Button>
                <Button type="button" size="sm" disabled={busy} onClick={() => void put(true)}>{s.confirmYes}</Button>
              </div>
            </div>
          )}
          {err && <p role="alert" className="font-medium text-danger">{err}</p>}
        </div>
      )}
    </Widget>
  );
}
