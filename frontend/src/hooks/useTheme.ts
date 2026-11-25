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
// Dark is the primary look; light only when the user picks it with the toggle.
const current = (): Theme => stored() ?? 'dark';
const apply = () => document.documentElement.classList.toggle('dark', current() === 'dark');

export function initTheme(): void {
  apply();
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
