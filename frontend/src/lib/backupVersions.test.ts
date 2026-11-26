import { describe, expect, it } from 'vitest';
import { breadcrumbs, browseUrl, defaultRestoreName, downloadUrl, entryDisabledReason, joinPath, restorePercent, restoreUrl, versionDateLabel } from './backupVersions';

describe('backupVersions helpers', () => {
  it('formats the version date in Czech', () => {
    expect(versionDateLabel('2026-11-25')).toBe('středa 25. 11. 2026');
    expect(versionDateLabel('2026-02-30')).toBe('2026-02-30');
    expect(versionDateLabel('x')).toBe('x');
  });
  it('splits breadcrumbs', () => {
    expect(breadcrumbs('')).toEqual([]);
    expect(breadcrumbs('a/b')).toEqual([{ name: 'a', path: 'a' }, { name: 'b', path: 'a/b' }]);
  });
  it('joins paths', () => {
    expect(joinPath('', 'a')).toBe('a');
    expect(joinPath('a', 'b')).toBe('a/b');
  });
  it('encodes each query value', () => {
    expect(downloadUrl('exp ort', '2026-11-25', 'a b/č&x.txt'))
      .toBe('/api/v1/backup/exp%20ort/versions/2026-11-25/file?path=a%20b%2F%C4%8D%26x.txt');
    expect(browseUrl('e', '2026-11-25', '', 200)).toBe('/backup/e/versions/2026-11-25/browse?path=&offset=200');
  });
  it('explains disabled entries', () => {
    expect(entryDisabledReason({ type: 'dir' })).toBeNull();
    expect(entryDisabledReason({ type: 'file' })).toBeNull();
    expect(entryDisabledReason({ type: 'link' })).toBeTruthy();
    expect(entryDisabledReason({ type: 'other' })).toBeTruthy();
    expect(entryDisabledReason({ type: 'file', badName: true })).toBeTruthy();
  });
  it('suggests a restore name the NAS accepts', () => {
    expect(defaultRestoreName('nas-export/db', 'Export', '2026-11-25', false)).toBe('db (záloha 25. 11. 2026)');
    expect(defaultRestoreName('nas-export/db/dump.sql.gz', 'Export', '2026-11-05', true)).toBe('dump (záloha 5. 11. 2026).sql.gz');
    expect(defaultRestoreName('', 'Server: web/db', '2026-11-25', false)).toBe('Server- web-db (záloha 25. 11. 2026)');
    expect(defaultRestoreName('.env', 'x', '2026-11-25', true)).toBe('.env (záloha 25. 11. 2026)');
  });
  it('computes restore progress and URL', () => {
    const s = { totalBytes: 200, bytes: 50, totalFiles: 4, files: 1 };
    expect(restorePercent({ ...s, state: 'scanning' })).toBe(0);
    expect(restorePercent({ ...s, state: 'copying' })).toBe(25);
    expect(restorePercent({ ...s, totalBytes: 0, bytes: 0, files: 2, state: 'copying' })).toBe(50);
    expect(restorePercent({ ...s, state: 'done' })).toBe(100);
    expect(restoreUrl('exp ort', '2026-11-25')).toBe('/backup/exp%20ort/versions/2026-11-25/restore');
  });
});
