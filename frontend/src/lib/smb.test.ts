import { describe, expect, it } from 'vitest';
import type { SmbService, SmbSettings } from '../api/types';
import { descriptionOk, disconnects, sameSettings, shareAddress, smbHealth, workgroupOk } from './smb';

const set: SmbSettings = { enabled: true, workgroup: 'WORKGROUP', description: 'NAS', minProtocol: 'SMB2_10', netbios: false };
const svc = (p: Partial<SmbService> = {}, s: Partial<SmbSettings> = {}): SmbService =>
  ({ installed: true, running: true, netbiosRunning: false, managed: true, ...p, settings: { ...set, ...s } });

describe('smbHealth', () => {
  it('reports each state', () => {
    expect(smbHealth(svc({ installed: false }))).toBe('notInstalled');
    expect(smbHealth(svc())).toBe('ok');
    expect(smbHealth(svc({ running: false }))).toBe('notRunning');
    expect(smbHealth(svc({ running: false, managed: false }, { enabled: false }))).toBe('off');
    expect(smbHealth(svc({}, { enabled: false }))).toBe('runningWhileOff');
    expect(smbHealth(svc({ managed: false }))).toBe('unmanaged');
  });
});

describe('validation', () => {
  it('matches the backend rules', () => {
    expect(workgroupOk(' home-lan ')).toBe(true);
    expect(workgroupOk('')).toBe(false);
    expect(workgroupOk('SKUPINA Č')).toBe(false);
    expect(workgroupOk('A'.repeat(16))).toBe(false);
    expect(descriptionOk('Domácí NAS')).toBe(true);
    expect(descriptionOk('50 %')).toBe(false);
    expect(descriptionOk('a\\b')).toBe(false);
    expect(descriptionOk('a\nb')).toBe(false);
    expect(descriptionOk('x'.repeat(65))).toBe(false);
  });
});

describe('disconnects', () => {
  it('warns only when clients would drop', () => {
    expect(disconnects(svc(), { ...set, workgroup: 'HOME' })).toBe(false);
    expect(disconnects(svc(), { ...set, enabled: false })).toBe(true);
    expect(disconnects(svc(), { ...set, netbios: true })).toBe(true);
    expect(disconnects(svc({ running: false }), { ...set, enabled: false })).toBe(false);
  });
  it('compares normalised settings', () => {
    expect(sameSettings(set, { ...set, workgroup: ' workgroup ' })).toBe(true);
    expect(sameSettings(set, { ...set, minProtocol: 'SMB3_11' })).toBe(false);
  });
  it('builds addresses', () => {
    expect(shareAddress('nas', 'data')).toEqual({ unc: '\\\\nas\\data', url: 'smb://nas/data' });
  });
});
