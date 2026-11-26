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

export function restoreUrl(id: string, date: string): string {
  return `${base(id, date)}/restore`;
}

export const restoreRunning = (s: { state: string }): boolean => s.state === 'scanning' || s.state === 'copying';

/** Copy progress in percent by bytes (by files when there are no bytes); 0 while scanning. */
export function restorePercent(s: { state: string; totalBytes: number; bytes: number; totalFiles: number; files: number }): number {
  if (s.state === 'done') return 100;
  if (s.state !== 'copying') return 0;
  if (s.totalBytes > 0) return Math.min(100, (s.bytes / s.totalBytes) * 100);
  return s.totalFiles > 0 ? Math.min(100, (s.files / s.totalFiles) * 100) : 0;
}

/** Characters the NAS refuses in new names (Windows-safe rules of the File Station). */
const BAD_NAME = /[\\/:*?"<>|\u0000-\u001f\u007f]/g;

/**
 * Suggested name of the restored item: "<name> (záloha 25. 11. 2026)", for a
 * file before its extension. The root of a version takes the job name.
 */
export function defaultRestoreName(path: string, jobName: string, date: string, isFile: boolean): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const tag = `záloha ${m ? `${Number(m[3])}. ${Number(m[2])}. ${m[1]}` : date}`;
  let name = path === '' ? jobName : path.slice(path.lastIndexOf('/') + 1);
  let ext = '';
  const dot = name.indexOf('.', 1);
  if (isFile && dot > 0) { ext = name.slice(dot); name = name.slice(0, dot); }
  const clean = name.replace(BAD_NAME, '-').trim().replace(/\.+$/, '').slice(0, 180) || 'obnova';
  return `${clean} (${tag})${ext.replace(BAD_NAME, '-')}`;
}
