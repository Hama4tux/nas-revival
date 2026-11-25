const NBSP = ' ';
export const DASH = '—';

const nf = (digits: number) =>
  new Intl.NumberFormat('cs-CZ', { minimumFractionDigits: digits, maximumFractionDigits: digits });

export function formatNumber(v: number | null | undefined, digits = 0): string {
  return v == null || !Number.isFinite(v) ? DASH : nf(digits).format(v);
}

/** Binary units (KiB, MiB, ...). */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return DASH;
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = i === 0 || v >= 100 ? 0 : v >= 10 ? 1 : 2;
  return `${nf(digits).format(v)}${NBSP}${units[i]}`;
}

export function formatPercent(v: number | null | undefined, digits = 1): string {
  return v == null || !Number.isFinite(v) ? DASH : `${nf(digits).format(v)}${NBSP}%`;
}

/** Czech plural: one / few (2-4) / many. */
function plural(n: number, one: string, few: string, many: string): string {
  if (n === 1) return one;
  return n >= 2 && n <= 4 ? few : many;
}

export function formatUptime(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return DASH;
  const total = Math.floor(seconds);
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}${NBSP}${plural(days, 'den', 'dny', 'dní')}`);
  if (days > 0 || hours > 0) parts.push(`${hours}${NBSP}h`);
  if (days === 0) parts.push(`${minutes}${NBSP}min`);
  return parts.join(' ');
}

export function formatTemp(milliCelsius: number | null | undefined): string {
  return milliCelsius == null ? DASH : `${nf(1).format(milliCelsius / 1000)}${NBSP}°C`;
}

export function formatSpeed(mbps: number | null | undefined): string {
  return mbps == null ? DASH : `${formatNumber(mbps)}${NBSP}Mb/s`;
}

export const ratioPercent = (used: number, total: number): number =>
  total > 0 ? Math.min(100, Math.max(0, (used / total) * 100)) : 0;
