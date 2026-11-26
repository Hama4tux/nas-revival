import { describe, expect, it } from 'vitest';
import type { NfsService, NfsSettings } from '../api/types';
import { disconnects, exportOk, hostOk, nfsHealth, proxmoxCommand, sameSettings } from './nfs';

const set: NfsSettings = { enabled: true, exports: [{ share: 'zalohy', squash: 'root', clients: [{ host: '192.0.2.30', readOnly: false }] }] };
const svc = (p: Partial<NfsService> = {}, s: Partial<NfsSettings> = {}): NfsService =>
  ({ installed: true, running: true, managed: true, missing: [], shares: [], ...p, settings: { ...set, ...s } });

describe('nfsHealth', () => {
  it('reports each state', () => {
    expect(nfsHealth(svc({ installed: false }))).toBe('notInstalled');
    expect(nfsHealth(svc())).toBe('ok');
    expect(nfsHealth(svc({ running: false }))).toBe('notRunning');
    expect(nfsHealth(svc({ running: false }, { enabled: false }))).toBe('off');
    expect(nfsHealth(svc({}, { enabled: false }))).toBe('runningWhileOff');
    expect(nfsHealth(svc({ managed: false }))).toBe('unmanaged');
  });
});

describe('hostOk', () => {
  it('accepts addresses and narrow networks', () => {
    for (const h of ['192.0.2.30', ' 192.0.2.0/24 ', '10.1.0.0/16', '192.0.2.30/32', 'fd00::1', '2001:db8:1::/48']) expect(hostOk(h)).toBe(true);
  });
  it('refuses names, wildcards and wide networks', () => {
    for (const h of ['', '*', 'pve.home', '0.0.0.0', '0.0.0.0/0', '10.0.0.0/8', '::/0', 'fd00::/32', '224.0.0.1', '256.1.1.1',
      '192.0.2.1/24/1', '192.0.2.1(rw)', '192.0.2.1 10.0.0.1']) expect(hostOk(h)).toBe(false);
  });
});

describe('settings', () => {
  it('validates an export', () => {
    expect(exportOk(set.exports[0]!)).toBe(true);
    expect(exportOk({ share: '', squash: 'root', clients: [{ host: '192.0.2.1', readOnly: false }] })).toBe(false);
    expect(exportOk({ share: 'a', squash: 'root', clients: [] })).toBe(false);
  });
  it('compares ignoring host whitespace', () => {
    const b: NfsSettings = { enabled: true, exports: [{ share: 'zalohy', squash: 'root', clients: [{ host: ' 192.0.2.30 ', readOnly: false }] }] };
    expect(sameSettings(set, b)).toBe(true);
    expect(sameSettings(set, { ...b, enabled: false })).toBe(false);
  });
  it('warns when mounted clients lose access', () => {
    expect(disconnects(svc(), set)).toBe(false);
    expect(disconnects(svc(), { ...set, enabled: false })).toBe(true);
    expect(disconnects(svc(), { enabled: true, exports: [] })).toBe(true);
    expect(disconnects(svc(), { enabled: true, exports: [{ share: 'zalohy', squash: 'root', clients: [{ host: '192.0.2.31', readOnly: false }] }] })).toBe(true);
    expect(disconnects(svc({ running: false }), { enabled: true, exports: [] })).toBe(false);
  });
  it('builds the Proxmox storage command', () => {
    expect(proxmoxCommand('nas.home', '/srv/data/zalohy', 'nas')).toBe(
      'pvesm add nfs nas --server nas.home --export /srv/data/zalohy --content backup --options vers=4.2');
  });
});
