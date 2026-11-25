import { useCallback, useSyncExternalStore } from 'react';

const KEY = 'nr-theme';
type Theme = 'light' | 'dark';
const listeners = new Set<() => void>();
let mem: Theme | null = null; // fallback when localStorage is unavailable

function stored(): Theme | null {
  try {
    const v = localStorage.getItem(KEY);
    if (v === 'light' || v === 'dark') return v;
  } catch { /* storage unavailable */ }
  return mem;
}
const systemDark = () => window.matchMedia('(prefers-color-scheme: dark)').matches;
const current = (): Theme => stored() ?? (systemDark() ? 'dark' : 'light');
const apply = () => document.documentElement.classList.toggle('dark', current() === 'dark');

export function initTheme(): void {
  apply();
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    apply();
    listeners.forEach((l) => l());
  });
}

export function useTheme() {
  const theme = useSyncExternalStore(
    (cb) => { listeners.add(cb); return () => listeners.delete(cb); },
    current,
  );
  const toggle = useCallback(() => {
    mem = current() === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem(KEY, mem); } catch { /* storage unavailable */ }
    apply();
    listeners.forEach((l) => l());
  }, []);
  return { theme, toggle };
}
