import { Bell, CheckCheck } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import type { NotificationList } from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { notificationStatus, notificationText } from '../lib/notifications';
import { cn } from '../lib/utils';

const t = cs.notify;
const iconBtn = 'relative flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-sm hover:bg-muted aria-expanded:bg-muted';

export interface Notifications {
  data: NotificationList | null;
  error: boolean;
  markRead: (ids: string[] | 'all') => void;
}

/** Polls the notification centre (W4.5) every 30 s while the page is visible. */
export function useNotifications(enabled: boolean): Notifications {
  const [data, setData] = useState<NotificationList | null>(null);
  const [error, setError] = useState(false);
  const load = useCallback(() => {
    if (!enabled) return;
    api<NotificationList>('/notifications').then((d) => { setData(d); setError(false); }, () => setError(true));
  }, [enabled]);
  useVisiblePoll(load, 30_000);
  const markRead = useCallback((ids: string[] | 'all') => {
    const pick = (id: string) => ids === 'all' || ids.includes(id);
    // Optimistic: the badge drops at once; the next poll corrects it.
    setData((d) => d && { ...d, unread: d.items.filter((i) => !i.read && !pick(i.id)).length,
      items: d.items.map((i) => (pick(i.id) ? { ...i, read: true } : i)) });
    api('/notifications/read', { method: 'POST', body: ids === 'all' ? { all: true } : { ids } }).then(load, load);
  }, [load]);
  return { data, error, markRead };
}

export function NotificationBell({ n }: { n: Notifications }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const unread = n.data?.unread ?? 0;

  useEffect(() => {
    if (!open) return;
    panel.current?.focus();
    const down = (e: MouseEvent) => { if (!wrap.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', down);
    return () => document.removeEventListener('mousedown', down);
  }, [open]);

  const items = n.data?.items.slice(0, 50) ?? [];
  return (
    <div ref={wrap} className="relative"
      onKeyDown={(e) => { if (e.key === 'Escape' && open) { setOpen(false); btn.current?.focus(); } }}>
      <button ref={btn} type="button" className={iconBtn} aria-haspopup="dialog" aria-expanded={open}
        aria-label={t.open(unread)} onClick={() => setOpen((v) => !v)}>
        <Bell className="h-5 w-5" aria-hidden />
        {unread > 0 && (
          <span aria-hidden className="absolute right-0.5 top-0.5 min-w-[1.1rem] rounded-full bg-danger px-1 text-center text-[0.65rem] font-semibold leading-[1.1rem] text-white dark:text-slate-900">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>
      {open && (
        <div ref={panel} role="dialog" aria-label={t.title} tabIndex={-1}
          className="pop glass fixed inset-x-2 top-14 z-50 flex max-h-[70vh] flex-col rounded-lg text-foreground outline-none sm:absolute sm:inset-x-auto sm:right-0 sm:top-11 sm:w-96">
          <div className="flex items-center gap-2 border-b border-border px-3 py-2">
            <h2 className="text-sm font-semibold">{t.title}</h2>
            {unread > 0 && (
              <button type="button" onClick={() => n.markRead('all')}
                className="ml-auto flex items-center gap-1 rounded-md px-2 py-1 text-xs text-primary hover:bg-muted">
                <CheckCheck className="h-4 w-4" aria-hidden />{t.markAll}
              </button>
            )}
          </div>
          <div className="overflow-y-auto p-1">
            {n.error && !n.data && <p className="px-3 py-4 text-sm text-muted-foreground">{t.error}</p>}
            {n.data && items.length === 0 && <p className="px-3 py-4 text-sm text-muted-foreground">{t.empty}</p>}
            <ul>
              {items.map((i) => (
                <li key={i.id} className={cn('flex items-start gap-2 rounded-md px-3 py-2', !i.read && 'bg-primary/5')}>
                  <span aria-hidden className={cn('mt-1.5 h-2 w-2 shrink-0 rounded-full',
                    i.resolvedAt ? 'bg-muted-foreground/40' : i.severity === 'crit' ? 'bg-danger' : 'bg-warning')} />
                  <div className="min-w-0 flex-1">
                    <p className={cn('text-sm', !i.read && 'font-semibold', i.resolvedAt && 'text-muted-foreground')}>
                      {notificationText(i)}
                      {!i.read && <span className="sr-only"> ({t.unread})</span>}
                    </p>
                    <p className="text-xs text-muted-foreground">{notificationStatus(i)}</p>
                  </div>
                  {!i.read && (
                    <button type="button" onClick={() => n.markRead([i.id])} aria-label={`${t.markRead}: ${notificationText(i)}`}
                      title={t.markRead} className="shrink-0 rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground">
                      <CheckCheck className="h-4 w-4" aria-hidden />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
