import { describe, expect, it } from 'vitest';
import type { Overview, Smart } from '../api/types';
import { deriveHealth } from './health';

const base = (): Overview => ({
  generatedAt: '', host: { hostname: 'nas', os: '', kernel: '', uptimeSeconds: 1, boardModel: '' },
  cpu: { model: '', cores: 1, load1: 0, load5: 0, load15: 0, usagePercent: 1 },
  memory: { totalBytes: 1, availableBytes: 1, swapTotalBytes: 0, swapFreeBytes: 0 },
  sensors: { status: 'ok', temperatures: [], fans: [{ id: 'f', label: 'Ventilátor', rpm: 9000 }] },
  network: [],
  storage: { disks: [], filesystems: [{ mount: '/', device: '', fsType: 'ext4', totalBytes: 100, usedBytes: 50, availableBytes: 50 }] },
  services: [{ name: 'a', label: 'SSH', state: 'active' }],
});
const smart = (health: 'passed' | 'failed', standby = false): Smart => ({
  checkedAt: '', disks: [{ name: 'sda', model: 'x', health, temperatureCelsius: 1, powerOnHours: 1, reallocatedSectors: 0, pendingSectors: 0, standby }],
});

describe('deriveHealth', () => {
  it('is ok for a healthy system', () => {
    expect(deriveHealth(base(), smart('passed'), false)).toEqual({ level: 'ok', reasons: [] });
  });
  it('warns on filesystem > 85 % and problems at > 95 %', () => {
    const o = base(); o.storage.filesystems[0]!.usedBytes = 90;
    expect(deriveHealth(o, null, false).level).toBe('warning');
    o.storage.filesystems[0]!.usedBytes = 96;
    const h = deriveHealth(o, null, false);
    expect(h.level).toBe('problem');
    expect(h.reasons[0]).toMatchObject({ code: 'fsCritical', arg: '/' });
  });
  it('treats 85 % exactly as fine', () => {
    const o = base(); o.storage.filesystems[0]!.usedBytes = 85;
    expect(deriveHealth(o, null, false).level).toBe('ok');
  });
  it('flags stopped fan as warning', () => {
    const o = base(); o.sensors.fans[0]!.rpm = 0;
    expect(deriveHealth(o, null, false).reasons).toEqual([{ level: 'warning', code: 'fanStopped', arg: 'Ventilátor' }]);
  });
  it('null fan rpm is not a stopped fan', () => {
    const o = base(); o.sensors.fans[0]!.rpm = null;
    expect(deriveHealth(o, null, false).level).toBe('ok');
  });
  it('sensor fault is a problem, unsupported a warning', () => {
    const o = base(); o.sensors.status = 'fault';
    expect(deriveHealth(o, null, false).level).toBe('problem');
    o.sensors.status = 'unsupported';
    expect(deriveHealth(o, null, false).level).toBe('warning');
  });
  it('failed SMART and failed service are problems; standby disks ignored', () => {
    expect(deriveHealth(base(), smart('failed'), false).level).toBe('problem');
    expect(deriveHealth(base(), smart('failed', true), false).level).toBe('ok');
    const o = base(); o.services[0]!.state = 'failed';
    expect(deriveHealth(o, null, false).reasons[0]!.code).toBe('serviceFailed');
  });
  it('stale data warns and problems sort first', () => {
    const o = base(); o.services[0]!.state = 'failed';
    const h = deriveHealth(o, null, true);
    expect(h.level).toBe('problem');
    expect(h.reasons.map((r) => r.code)).toEqual(['serviceFailed', 'stale']);
  });
});
