import { AlertTriangle, ExternalLink } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../api/client';
import type { NetworkSettings } from '../api/types';
import { formatCountdown, useCountdown } from '../hooks/useCountdown';
import { cs } from '../i18n/cs';
import { Button } from './ui/button';

const t = cs.pending;

/** Global banner shown on every page while a network change waits for confirmation. */
export function PendingBanner({ pending, onConfirm }: { pending: NonNullable<NetworkSettings['pending']>; onConfirm: () => Promise<void> }) {
  const left = useCountdown(pending.deadline);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const next = pending.next;
  const addr = next.mode === 'static' ? next.address : null;
  const elsewhere = addr != null && addr !== location.hostname;
  const expired = left === 0;

  async function confirm() {
    setBusy(true); setErr(null);
    try { await onConfirm(); }
    catch (e) { setErr(e instanceof ApiError && e.status === 409 ? t.nothing : e instanceof ApiError && e.status === 0 ? cs.errors.network : cs.errors.generic); }
    finally { setBusy(false); }
  }

  return (
    <section aria-labelledby="pending-title" className="sticky top-14 z-40 border-b border-warning/50 bg-card/95 backdrop-blur lg:top-0">
      <div className="mx-auto flex max-w-[1440px] flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 sm:px-6">
        <AlertTriangle className="h-5 w-5 shrink-0 text-warning" aria-hidden />
        <div className="min-w-0 flex-1 basis-64 text-sm">
          <h2 id="pending-title" className="font-semibold">{t.title}</h2>
          <div className="text-muted-foreground">
            {expired ? t.expired : t.body(left == null ? '–' : formatCountdown(left))}
            {' '}{elsewhere ? t.newLogin : addr ? t.hereOk : t.dhcpNew}
          </div>
          {addr && <div className="mt-0.5">{t.newAddress}: <span className="font-mono">{addr}</span></div>}
          <div role="alert">{err && <div className="mt-1 font-medium text-danger">{err}</div>}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          {elsewhere && (
            <Button asChild variant="outline">
              <a href={`https://${addr}:8443`} target="_self">{t.openNew}<ExternalLink className="h-4 w-4" aria-hidden /></a>
            </Button>
          )}
          <Button type="button" disabled={busy || expired} onClick={() => void confirm()}>{busy ? t.confirming : t.confirm}</Button>
        </div>
      </div>
    </section>
  );
}
