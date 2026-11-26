import { cs } from '../i18n/cs';

const WEEKDAYS = ['neděle', 'pondělí', 'úterý', 'středa', 'čtvrtek', 'pátek', 'sobota'];

/** "2026-11-25" -> "středa 25. 11. 2026"; unparsable input is returned unchanged. */
export function versionDateLabel(date: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) return date;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const dt = new Date(Date.UTC(y, mo - 1, d));
  if (dt.getUTCFullYear() !== y || dt.getUTCMonth() !== mo - 1 || dt.getUTCDate() !== d) return date;
  return `${WEEKDAYS[dt.getUTCDay()]} ${d}. ${mo}. ${y}`;
}

export interface Crumb { name: string; path: string }

/** "a/b/c" -> [{a,"a"},{b,"a/b"},{c,"a/b/c"}]; "" -> []. */
export function breadcrumbs(path: string): Crumb[] {
  const parts = path.split('/').filter((p) => p !== '');
  return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') }));
}

export const joinPath = (dir: string, name: string): string => (dir === '' ? name : `${dir}/${name}`);

const base = (id: string, date: string) => `/backup/${encodeURIComponent(id)}/versions/${encodeURIComponent(date)}`;

/** Path for api() (no /api/v1 prefix). */
export function browseUrl(id: string, date: string, path: string, offset = 0): string {
  return `${base(id, date)}/browse?path=${encodeURIComponent(path)}&offset=${offset}`;
}

export function downloadUrl(id: string, date: string, path: string): string {
  return `/api/v1${base(id, date)}/file?path=${encodeURIComponent(path)}`;
}

/** Why an entry cannot be opened, or null when it is a clickable dir/file. */
export function entryDisabledReason(e: { type: string; badName?: boolean }): string | null {
  if (e.badName) return cs.backupVersions.badName;
  if (e.type === 'link') return cs.backupVersions.isLink;
  if (e.type === 'other') return cs.backupVersions.isOther;
  return null;
}
