import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { NetworkSettings } from '../api/types';

/**
 * Network settings incl. the pending change. Polls every 5 s while enabled (all pages).
 * Poll errors (status 0 / 5xx while the NAS reapplies its network) are kept in `error`
 * but never log out - only a real 401 does, via the global handler.
 */
export function useNetworkSettings(enabled: boolean) {
  const [data, setData] = useState<NetworkSettings | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const alive = useRef(true);

  const reload = useCallback(async () => {
    try {
      const d = await api<NetworkSettings>('/settings/network');
      if (!alive.current) return;
      setData(d); setError(null);
    } catch (e) {
      if (alive.current && e instanceof ApiError) setError(e);
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    if (!enabled) return () => { alive.current = false; };
    void reload();
    const id = setInterval(() => { void reload(); }, 5000);
    return () => { alive.current = false; clearInterval(id); };
  }, [enabled, reload]);

  return { data, error, reload, setData };
}
