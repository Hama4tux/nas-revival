import { describe, expect, it } from 'vitest';
import { breadcrumbs, browseUrl, downloadUrl, entryDisabledReason, joinPath, versionDateLabel } from './backupVersions';

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
});
