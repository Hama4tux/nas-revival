import { describe, expect, it } from 'vitest';
import type { ServiceInfo } from '../api/types';
import { groupPorts, isDangerAction, isProblem, isRunning, needsConfirm, portSummary, priorityTone, problems, sortServices, stateTone } from './services';

const svc = (p: Partial<ServiceInfo>): ServiceInfo => ({
  name: 'x.service', label: 'X', description: '', state: 'active', subState: 'running', enabled: 'enabled', since: null,
  memoryBytes: null, cpuPercent: null, ports: [], protected: false, actions: [], ...p,
});

describe('services', () => {
  it('maps states to tones', () => {
    expect(stateTone('active')).toBe('success');
    expect(stateTone('done')).toBe('success');
    expect(stateTone('failed')).toBe('danger');
    expect(stateTone('activating')).toBe('warning');
    expect(stateTone('not-installed')).toBe('neutral');
  });

  it('asks before stopping, disabling or restarting a protected unit', () => {
    expect(needsConfirm('stop', svc({}))).toBe(true);
    expect(needsConfirm('disable', svc({}))).toBe(true);
    expect(needsConfirm('restart', svc({}))).toBe(false);
    expect(needsConfirm('restart', svc({ protected: true }))).toBe(true);
    expect(needsConfirm('start', svc({ protected: true }))).toBe(false);
  });

  it('merges IPv4 and IPv6 sockets and marks loopback-only ports', () => {
    expect(groupPorts([
      { proto: 'tcp', address: '::', port: 22 }, { proto: 'tcp', address: '0.0.0.0', port: 22 },
      { proto: 'udp', address: '127.0.0.53', port: 53 }, { proto: 'tcp', address: '127.0.0.54', port: 53 },
      { proto: 'tcp', address: '127.0.0.53', port: 53 },
    ])).toEqual([
      { proto: 'tcp', port: 22, localOnly: false },
      { proto: 'tcp', port: 53, localOnly: true },
      { proto: 'udp', port: 53, localOnly: true },
    ]);
    expect(groupPorts([{ proto: 'tcp', address: '127.0.0.1', port: 80 }, { proto: 'tcp', address: '0.0.0.0', port: 80 }]))
      .toEqual([{ proto: 'tcp', port: 80, localOnly: false }]);
  });

  it('lists units that are not installed last', () => {
    const l = [svc({ name: 'a', state: 'not-installed' }), svc({ name: 'b' }), svc({ name: 'c', state: 'failed' })];
    expect(sortServices(l).map((s) => s.name)).toEqual(['b', 'c', 'a']);
  });

  it('counts failed and unexpectedly stopped units', () => {
    const l = [svc({ name: 'a', state: 'failed' }), svc({ name: 'b', state: 'inactive' }),
      svc({ name: 'c', state: 'inactive', enabled: 'disabled' }), svc({ name: 'd', state: 'done' })];
    expect(problems(l).map((s) => s.name)).toEqual(['a', 'b']);
  });

  it('colours journal priorities', () => {
    expect(priorityTone(3)).toBe('danger');
    expect(priorityTone(4)).toBe('warning');
    expect(priorityTone(6)).toBe('neutral');
  });

  it('flags failed units and enabled units that are stopped', () => {
    expect(isProblem(svc({ state: 'failed', enabled: 'disabled' }))).toBe(true);
    expect(isProblem(svc({ state: 'inactive', enabled: 'enabled' }))).toBe(true);
    expect(isProblem(svc({ state: 'inactive', enabled: 'disabled' }))).toBe(false);
    expect(isProblem(svc({ state: 'active', enabled: 'enabled' }))).toBe(false);
  });

  it('counts transitional states as running', () => {
    expect(isRunning(svc({ state: 'activating' }))).toBe(true);
    expect(isRunning(svc({ state: 'inactive' }))).toBe(false);
    expect(isRunning(svc({ state: 'done' }))).toBe(false);
  });

  it('marks stop and disable as dangerous', () => {
    expect(isDangerAction('stop')).toBe(true);
    expect(isDangerAction('disable')).toBe(true);
    expect(isDangerAction('restart')).toBe(false);
  });

  it('summarises reachable ports per protocol and skips loopback-only ones', () => {
    expect(portSummary([
      { proto: 'tcp', port: 445, address: '0.0.0.0' }, { proto: 'tcp', port: 139, address: '::' },
      { proto: 'udp', port: 137, address: '0.0.0.0' }, { proto: 'tcp', port: 8080, address: '127.0.0.1' },
    ])).toBe('TCP 139, 445 · UDP 137');
    expect(portSummary([{ proto: 'tcp', port: 8080, address: '::1' }])).toBe('');
    expect(portSummary([])).toBe('');
  });
});
