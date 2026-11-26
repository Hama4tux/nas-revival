import { useEffect, useRef } from 'react';

/** Run `fn` immediately and then every `ms` while the document is visible. */
export function useVisiblePoll(fn: () => void, ms: number) {
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
