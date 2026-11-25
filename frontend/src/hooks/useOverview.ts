import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import type { Overview, Smart } from '../api/types';
import { isStale, pushSample } from '../lib/stale';

const POLL_MS = 5000;
const SMART_MS = 60_000;

/** Run `fn` immediately and then every `ms` while the document is visible. */
function useVisiblePoll(fn: () => void, ms: number) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    let timer: number | undefined;
    const stop = () => { if (timer !== undefined) { clearInterval(timer); timer = undefined; } };
    const start = () => {
      stop();
      ref.current();
      timer = window.setInterval(() => ref.current(), ms);
    };
    const onVis = () => (document.visibilityState === 'visible' ? start() : stop());
    if (document.visibilityState === 'visible') start();
    document.addEventListener('visibilitychange', onVis);
    return () => { stop(); document.removeEventListener('visibilitychange', onVis); };
  }, [ms]);
}

export function useOverview() {
  const [data, setData] = useState<Overview | null>(null);
  const [error, setError] = useState(false);
  const [lastOk, setLastOk] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [cpuHistory, setCpuHistory] = useState<number[]>([]);
  const [tempHistory, setTempHistory] = useState<number[]>([]);
  const [smart, setSmart] = useState<Smart | null>(null);
  const [smartError, setSmartError] = useState(false);
  const busy = useRef(false);

  const loadOverview = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const o = await api<Overview>('/overview');
      setData(o);
      setError(false);
      setLastOk(Date.now());
      setCpuHistory((h) => pushSample(h, o.cpu.usagePercent));
      const temps = o.sensors.status === 'ok'
        ? o.sensors.temperatures.map((t) => t.milliCelsius).filter((v): v is number => v != null)
        : [];
      setTempHistory((h) => pushSample(h, temps.length ? Math.max(...temps) / 1000 : null));
    } catch {
      setError(true); // 401 is handled globally by the api client
    } finally {
      busy.current = false;
    }
  }, []);

  const loadSmart = useCallback(async () => {
    try {
      setSmart(await api<Smart>('/smart'));
      setSmartError(false);
    } catch {
      setSmartError(true);
    }
  }, []);

  useVisiblePoll(loadOverview, POLL_MS);
  useVisiblePoll(loadSmart, SMART_MS);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  return {
    data, error, smart, smartError, cpuHistory, tempHistory, lastOk,
    stale: data !== null && isStale(lastOk, now),
    reload: loadOverview,
  };
}
