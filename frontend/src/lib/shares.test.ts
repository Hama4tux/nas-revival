import { describe, expect, it } from 'vitest';
import type { Share } from '../api/types';
import { accessChoices, addPermission, daysProblem, descriptionProblem, permissionsProblem, removePermission, shareNameProblem, shareValid, toShare, usersWithAccess, withBackupTarget } from './shares';

const share = (p: Partial<Share> = {}): Share => ({
  name: 'Fotky', description: '', volume: '/srv/data', folder: 'Fotky', readOnly: false, hidden: false,
  recycle: { enabled: true, days: 30 }, permissions: [], ...p,
});

describe('share rules', () => {
  it('validates names like the backend', () => {
    expect(shareNameProblem('Fotky')).toBeNull();
    expect(shareNameProblem('')).toBe('empty');
    for (const n of ['_a', 'a b', 'á', 'a'.repeat(33)]) expect(shareNameProblem(n)).toBe('format');
    expect(shareNameProblem('IPC$')).toBe('format');
    expect(shareNameProblem('Global')).toBe('reserved');
    expect(shareNameProblem('fotky', ['Fotky'])).toBe('exists');
  });
  it('checks description and days', () => {
    expect(descriptionProblem('Rodinné fotky')).toBe(false);
    expect(descriptionProblem('a\\b')).toBe(true);
    expect(descriptionProblem('x'.repeat(65))).toBe(true);
    expect(daysProblem(0)).toBe(true);
    expect(daysProblem(366)).toBe(true);
    expect(daysProblem(1.5)).toBe(true);
    expect(daysProblem(30)).toBe(false);
  });
  it('rejects group none and duplicates', () => {
    expect(permissionsProblem([{ kind: 'group', name: 'rodina', access: 'none' }])).toBe('groupNone');
    expect(permissionsProblem([{ kind: 'user', name: 'a', access: 'rw' }, { kind: 'user', name: 'a', access: 'ro' }])).toBe('duplicate');
    expect(permissionsProblem([{ kind: 'user', name: 'a', access: 'none' }, { kind: 'group', name: 'a', access: 'ro' }])).toBeNull();
    expect(accessChoices('group')).toEqual(['rw', 'ro']);
    expect(accessChoices('user')).toContain('none');
  });
  it('validates the whole form', () => {
    expect(shareValid(share(), true, [])).toBe(true);
    expect(shareValid(share({ volume: '' }), true, [])).toBe(false);
    expect(shareValid(share({ recycle: { enabled: true, days: 0 } }), false, [])).toBe(false);
    expect(shareValid(share({ recycle: { enabled: false, days: 0 } }), false, [])).toBe(true);
    expect(shareValid(share(), true, ['fotky'])).toBe(false);
  });
  it('edits permission lists', () => {
    const a = addPermission([], { kind: 'user', name: 'jana', access: 'rw' });
    expect(addPermission(a, { kind: 'user', name: 'jana', access: 'ro' })).toBe(a);
    expect(removePermission(a, 'user', 'jana')).toEqual([]);
    expect(usersWithAccess([{ access: 'rw' }, { access: 'none' }, { access: 'ro' }])).toBe(2);
  });
});

describe('toShare', () => {
  it('drops the read-only fields of ShareInfo', () => {
    const info = { ...share({ permissions: [{ kind: 'user' as const, name: 'jana', access: 'rw' as const }] }),
      path: '/srv/data/Fotky', exists: true, enforcement: 'acl' as const, effective: [], conflicts: [] };
    expect(Object.keys(toShare(info)).sort()).toEqual(Object.keys(share()).sort());
    expect(toShare(info).permissions).toEqual([{ kind: 'user', name: 'jana', access: 'rw' }]);
  });
});

describe('backup target', () => {
  const rw = [{ kind: 'group' as const, name: 'rodina', access: 'rw' as const }];
  it('refuses rw entries and offers only read access', () => {
    expect(permissionsProblem(rw, true)).toBe('backupRw');
    expect(permissionsProblem(rw, false)).toBeNull();
    expect(accessChoices('user', true)).toEqual(['ro', 'none']);
    expect(accessChoices('group', true)).toEqual(['ro']);
    expect(shareValid(share({ backupTarget: true, permissions: rw }), false, [])).toBe(false);
  });
  it('switching it on forces read-only, no recycle bin and no rw', () => {
    const s = withBackupTarget(share({ permissions: rw }), true);
    expect(s).toMatchObject({ backupTarget: true, readOnly: true, recycle: { enabled: false } });
    expect(s.permissions[0]?.access).toBe('ro');
    expect(shareValid(s, false, [])).toBe(true);
    expect(withBackupTarget(s, false).backupTarget).toBe(false);
  });
  it('sends backupTarget only when set', () => {
    expect(toShare(share()).backupTarget).toBeUndefined();
    expect(toShare(share({ backupTarget: true })).backupTarget).toBe(true);
  });
});
