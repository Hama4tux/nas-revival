import { CheckCircle2, Loader2, PowerOff, TriangleAlert } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { SystemPowerAction } from '../api/types';
import { cs } from '../i18n/cs';
import { nextPhase, type DownPhase } from '../lib/systemPower';
import { Button } from './ui/button';

const t = cs.sysPower.down;
const POLL_MS = 3000;
const PROBE_TIMEOUT_MS = 4000;

/** Does the web service answer? A 401 counts: after a restart the session is gone. */
async function probe(): Promise<boolean> {
  try {
    await api('/session', { quiet401: true, signal: AbortSignal.timeout(PROBE_TIMEOUT_MS) });
    return true;
  } catch (e) {
    return e instanceof ApiError && e.status !== 0 && e.status < 500;
  }
}

const mmss = (ms: number) => {
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

/** Full-screen "restarting / shutting down" page; keeps polling until the NAS is gone or back (CORE-14). */
export function GoingDown({ action, onBack }: { action: SystemPowerAction; onBack: () => void }) {
  const [phase, setPhase] = useState<DownPhase>('stopping');
  const [elapsed, setElapsed] = useState(0);
  const start = useRef(Date.now());
  const phaseRef = useRef(phase);
  phaseRef.current = phase;

  useEffect(() => {
    let stop = false;
    let timer: number | undefined;
    async function tick() {
      const up = await probe();
      if (stop) return;
      const ms = Date.now() - start.current;
      setElapsed(ms);
      const next = nextPhase(action, phaseRef.current, up, ms);
      setPhase(next);
      // A shut-down NAS will not answer again; one more probe a minute is enough.
      if (next !== 'back') timer = window.setTimeout(tick, action === 'shutdown' && next === 'offline' ? 60_000 : POLL_MS);
    }
    timer = window.setTimeout(tick, POLL_MS);
    return () => { stop = true; window.clearTimeout(timer); };
  }, [action]);

  const view = phase === 'stopping'
    ? { icon: <Loader2 className="h-10 w-10 animate-spin text-primary" aria-hidden />, title: t.stopping[action], detail: t.stoppingDetail }
    : phase === 'back'
      ? { icon: <CheckCircle2 className="h-10 w-10 text-success" aria-hidden />, title: t.back, detail: t.backDetail }
      : phase === 'late'
        ? { icon: <TriangleAlert className="h-10 w-10 text-warning" aria-hidden />, title: t.late, detail: t.lateDetail }
        : action === 'reboot'
          ? { icon: <Loader2 className="h-10 w-10 animate-spin text-primary" aria-hidden />, title: t.offlineReboot, detail: t.offlineRebootDetail }
          : { icon: <PowerOff className="h-10 w-10 text-muted-foreground" aria-hidden />, title: t.offlineShutdown, detail: t.offlineShutdownDetail };

  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <div className="glass w-full max-w-md rounded-2xl bg-card/80 p-6 text-center shadow-xl backdrop-blur-xl">
        <div className="mb-4 flex justify-center">{view.icon}</div>
        <div role="status" aria-live="polite">
          <h1 className="text-xl font-semibold">{view.title}</h1>
          <p className="mt-2 text-sm text-muted-foreground">{view.detail}</p>
        </div>
        {phase !== 'back' && <p className="mt-4 text-xs text-muted-foreground">{t.elapsed(mmss(elapsed))}</p>}
        {phase === 'back' && <Button type="button" className="mt-5 w-full" autoFocus onClick={onBack}>{t.login}</Button>}
      </div>
    </main>
  );
}
