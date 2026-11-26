import type { FileBatch } from '../api/types';
import { cs } from '../i18n/cs';

const base = (share: string) => `/files/${encodeURIComponent(share)}`;

/** Paths for api() (no /api/v1 prefix). */
export const listUrl = (share: string, path: string, offset = 0) =>
  `${base(share)}/list?path=${encodeURIComponent(path)}&offset=${offset}`;

export const fileDownloadUrl = (share: string, path: string) =>
  `/api/v1${base(share)}/download?path=${encodeURIComponent(path)}`;

export const uploadUrl = (share: string, dir: string, name: string, overwrite: boolean) =>
  `/api/v1${base(share)}/upload?path=${encodeURIComponent(dir)}&name=${encodeURIComponent(name)}${overwrite ? '&overwrite=1' : ''}`;

export const actionUrl = (share: string, op: 'mkdir' | 'rename' | 'move' | 'delete') => `${base(share)}/${op}`;

export const parentPath = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');

const utf8Len = (s: string) => new TextEncoder().encode(s).length;

/** Same rules as the helper (ValidFileName); null when the name is fine. */
export function nameProblem(n: string): string | null {
  const t = cs.files.name;
  if (n === '') return t.empty;
  if (n === '.' || n === '..' || n === '.recycle' || n.startsWith('.nas-upload-')) return t.reserved;
  if (utf8Len(n) > 255) return t.long;
  if (n.trim() !== n) return t.spaces;
  if (n.endsWith('.')) return t.dot;
  if (/[/\\:*?"<>|\u0000-\u001f\u007f]/.test(n)) return t.chars;
  return null;
}

/** Short Czech summary of a move or delete, null when everything went through. */
export function batchProblem(r: FileBatch): string | null {
  const first = r.failed[0];
  if (!first) return null;
  return cs.files.partial(r.done, r.failed.length, `${first.path}: ${first.message}`);
}

/** "12 %" style progress of an upload. */
export const percent = (sent: number, size: number) => (size <= 0 ? 100 : Math.min(100, Math.round((sent / size) * 100)));
