import { describe, expect, it } from 'vitest';
import type { SshService, SshSettings } from '../api/types';
import { keyPasteProblem, keyTypeLabel, portOk, sameSettings, sftpCommand, sshHealth, sshWarnings } from './ssh';

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

describe('keyPasteProblem', () => {
  const ed = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOc0yZ0r1Yk2tK4pV8Cq7q9F7w0mJ3vX6nL4bR2sT5uH jana@notebook';
  it('accepts one public key line, with or without comment', () => {
    expect(keyPasteProblem(ed)).toBeNull();
    expect(keyPasteProblem(`  ${ed}\n`)).toBeNull();
    expect(keyPasteProblem('ssh-rsa AAAAB3NzaC1yc2E=')).toBeNull();
    expect(keyPasteProblem('ecdsa-sha2-nistp256 AAAAE2VjZHNh x')).toBeNull();
  });
  it('names what is wrong', () => {
    expect(keyPasteProblem('  ')).toBe('empty');
    expect(keyPasteProblem('-----BEGIN OPENSSH PRIVATE KEY-----\nabc')).toBe('private');
    expect(keyPasteProblem('---- BEGIN SSH2 PUBLIC KEY ----\nAAAA')).toBe('putty');
    expect(keyPasteProblem(`${ed}\n${ed}`)).toBe('multi');
    expect(keyPasteProblem(`command="x" ${ed}`)).toBe('format');
    expect(keyPasteProblem('ssh-dss AAAAB3NzaC1kc3M=')).toBe('format');
  });
});

describe('keyTypeLabel', () => {
  it('shortens the key types', () => {
    expect(keyTypeLabel('ssh-ed25519')).toBe('ed25519');
    expect(keyTypeLabel('ssh-rsa', 4096)).toBe('RSA 4096');
    expect(keyTypeLabel('ecdsa-sha2-nistp384')).toBe('ECDSA');
    expect(keyTypeLabel('sk-ssh-ed25519@openssh.com')).toBe('ed25519 (FIDO)');
  });
});
