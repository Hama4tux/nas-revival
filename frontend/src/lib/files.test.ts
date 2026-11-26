import { describe, expect, it } from 'vitest';
import { batchProblem, fileDownloadUrl, listUrl, nameProblem, parentPath, percent, uploadUrl } from './files';

describe('file station helpers', () => {
  it('builds encoded URLs', () => {
    expect(listUrl('Fotky 2026', 'a/č', 200)).toBe('/files/Fotky%202026/list?path=a%2F%C4%8D&offset=200');
    expect(fileDownloadUrl('data', 'a&b.txt')).toBe('/api/v1/files/data/download?path=a%26b.txt');
    expect(uploadUrl('data', '', 'x y', true)).toBe('/api/v1/files/data/upload?path=&name=x%20y&overwrite=1');
    expect(uploadUrl('data', 'd', 'x', false)).toBe('/api/v1/files/data/upload?path=d&name=x');
  });
  it('finds the parent', () => {
    expect(parentPath('a')).toBe('');
    expect(parentPath('a/b/c')).toBe('a/b');
  });
  it('validates names like the helper', () => {
    for (const ok of ['a.txt', 'Faktura 2026', 'žluťoučký kůň', '.bashrc']) expect(nameProblem(ok)).toBeNull();
    for (const bad of ['', '.', '..', '.recycle', '.nas-upload-1', ' a', 'a ', 'a.', 'a/b', 'a\\b', 'a:b', 'a?', 'a\u0001', 'x'.repeat(256)]) {
      expect(nameProblem(bad), bad).not.toBeNull();
    }
    // 128 two-byte letters = 256 bytes
    expect(nameProblem('č'.repeat(128))).not.toBeNull();
  });
  it('summarises partial batches', () => {
    expect(batchProblem({ done: 2, failed: [], recycled: false })).toBeNull();
    expect(batchProblem({ done: 1, failed: [{ path: 'a', message: 'Položka neexistuje.' }], recycled: false })).toContain('a: Položka neexistuje.');
  });
  it('computes progress', () => {
    expect(percent(0, 0)).toBe(100);
    expect(percent(50, 200)).toBe(25);
    expect(percent(300, 200)).toBe(100);
  });
});
