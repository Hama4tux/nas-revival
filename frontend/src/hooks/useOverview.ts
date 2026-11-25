import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import type { Overview, Smart } from '../api/types';
import { bytesRate, type NetSample } from '../lib/netrate';
import { isStale, pushSample } from '../lib/stale';

export interface NetRate { rx: number | null; tx: number | null }
export interface NetHistory { rx: number[]; tx: number[] }

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
  const [netRates, setNetRates] = useState<Record<string, NetRate>>({});
  const [netHistory, setNetHistory] = useState<Record<string, NetHistory>>({});
  const prevNet = useRef<Record<string, NetSample>>({});
  const busy = useRef(false);

  const loadOverview = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const o = await api<Overview>('/overview');
      setData(o);
      setError(false);
      setLastOk(Date.now());
      const t = Date.parse(o.generatedAt);
      const at = Number.isFinite(t) ? t : Date.now();
      const rates: Record<string, NetRate> = {};
      const next: Record<string, NetSample> = {};
      for (const n of o.network) {
        const p = prevNet.current[n.name];
        rates[n.name] = p
          ? { rx: bytesRate(p.rx, n.rxBytes, at - p.t), tx: bytesRate(p.tx, n.txBytes, at - p.t) }
          : { rx: null, tx: null };
        next[n.name] = { t: at, rx: n.rxBytes, tx: n.txBytes };
      }
      prevNet.current = next;
      setNetRates(rates);
      setNetHistory((h) => {
        const out: Record<string, NetHistory> = {};
        for (const [name, r] of Object.entries(rates)) {
          const prev = h[name] ?? { rx: [], tx: [] };
          out[name] = r.rx == null || r.tx == null ? prev : { rx: pushSample(prev.rx, r.rx), tx: pushSample(prev.tx, r.tx) };
        }
        return out;
      });
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
    data, error, smart, smartError, cpuHistory, tempHistory, netRates, netHistory, lastOk,
    stale: data !== null && isStale(lastOk, now),
    reload: loadOverview,
  };
}
