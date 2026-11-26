import { describe, expect, it } from 'vitest';
import { diagTargetError, listenScope } from './netdiag';

describe('diagTargetError', () => {
  it('accepts hosts and unicast addresses', () => {
    for (const ok of ['192.168.1.1', ' nas.example.org ', '2001:db8::1', 'router']) expect(diagTargetError('ping', ok)).toBeNull();
    expect(diagTargetError('dns', 'example.org')).toBeNull();
  });
  it('rejects what the helper rejects', () => {
    expect(diagTargetError('ping', ' ')).toBe('required');
    for (const bad of ['0.0.0.0', '255.255.255.255', '224.0.0.1', 'ff02::1', '-c', 'a b', 'x_y']) expect(diagTargetError('ping', bad)).toBe('invalid');
    expect(diagTargetError('dns', '192.0.2.1')).toBe('dnsNeedsName');
  });
});

describe('listenScope', () => {
  it('classifies listening addresses', () => {
    expect(listenScope('0.0.0.0')).toBe('all');
    expect(listenScope('::')).toBe('all');
    expect(listenScope('127.0.0.53')).toBe('local');
    expect(listenScope('::1')).toBe('local');
    expect(listenScope('192.0.2.60')).toBe('one');
  });
});
