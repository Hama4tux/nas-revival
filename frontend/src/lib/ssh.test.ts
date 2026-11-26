import { describe, expect, it } from 'vitest';
import type { SshService, SshSettings } from '../api/types';
import { portOk, sameSettings, sftpCommand, sshHealth, sshWarnings } from './ssh';

const set: SshSettings = { port: 22, passwordAuth: true, sftpGroups: [] };
const svc = (p: Partial<SshService> = {}, s: Partial<SshSettings> = {}): SshService => ({
  managed: true, running: true, groups: ['nas-users'], chrootOk: true,
  effective: { ports: [22], passwordAuth: true, rootLogin: 'without-password' }, ...p, settings: { ...set, ...s },
});

describe('sshHealth', () => {
  it('reports each state', () => {
    expect(sshHealth(svc())).toBe('ok');
    expect(sshHealth(svc({ managed: false }))).toBe('unmanaged');
    expect(sshHealth(svc({ running: false }))).toBe('notRunning');
  });
});

describe('portOk', () => {
  it('matches the server rule', () => {
    for (const p of [22, 1024, 2222, 65535]) expect(portOk(p)).toBe(true);
    for (const p of [0, 21, 23, 1023, 2049, 8443, 65536, 22.5, NaN]) expect(portOk(p)).toBe(false);
  });
});

describe('sameSettings', () => {
  it('ignores group order', () => {
    expect(sameSettings({ ...set, sftpGroups: ['a', 'b'] }, { ...set, sftpGroups: ['b', 'a'] })).toBe(true);
    expect(sameSettings(set, { ...set, port: 2222 })).toBe(false);
    expect(sameSettings(set, { ...set, passwordAuth: false })).toBe(false);
  });
});

describe('sshWarnings', () => {
  it('warns about a new port, SFTP without passwords and removed groups', () => {
    expect(sshWarnings(svc(), set)).toEqual([]);
    expect(sshWarnings(svc(), { ...set, port: 2222 })).toEqual(['port']);
    expect(sshWarnings(svc(), { ...set, passwordAuth: false, sftpGroups: ['nas-users'] })).toEqual(['noPassword']);
    expect(sshWarnings(svc({}, { sftpGroups: ['nas-users'] }), set)).toEqual(['sftpOff']);
  });
});

describe('sftpCommand', () => {
  it('adds -P only for a non-standard port', () => {
    expect(sftpCommand('nas.home', 22)).toBe('sftp uzivatel@nas.home');
    expect(sftpCommand('nas.home', 2222, 'jana')).toBe('sftp -P 2222 jana@nas.home');
  });
});
