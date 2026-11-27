import { describe, expect, it } from 'vitest';
import type { FirewallSettings } from '../api/types';
import { allowsClient, cidrContains, cleanList, invalidEntries, isAnywhere, lockoutServices, parseCidr, parseIPv4, sameFirewall } from './firewall';

const settings = (enabled: boolean, p: Partial<FirewallSettings['allow']> = {}): FirewallSettings =>
  ({ enabled, allow: { web: [], ssh: [], smb: [], nfs: [], ...p } });

describe('parseIPv4', () => {
  it('accepts valid and rejects invalid addresses', () => {
    expect(parseIPv4('192.168.1.10')).toBe(0xc0a8010a);
    expect(parseIPv4('0.0.0.0')).toBe(0);
    expect(parseIPv4('255.255.255.255')).toBe(0xffffffff);
    for (const s of ['', '1.2.3', '1.2.3.4.5', '256.1.1.1', '01.2.3.4', 'a.b.c.d', '1.2.3.4/24', '::1']) expect(parseIPv4(s)).toBeNull();
  });
});

describe('parseCidr', () => {
  it('parses addresses and networks', () => {
    expect(parseCidr('10.0.0.1')).toEqual({ addr: 0x0a000001, prefix: 32 });
    expect(parseCidr('192.168.1.0/24')).toEqual({ addr: 0xc0a80100, prefix: 24 });
    expect(parseCidr('0.0.0.0/0')?.prefix).toBe(0);
  });
  it('rejects bad prefixes', () => {
    for (const s of ['1.2.3.4/33', '1.2.3.4/', '1.2.3.4/-1', '1.2.3.4/x', '1.2.3/24']) expect(parseCidr(s)).toBeNull();
  });
});

describe('cidrContains', () => {
  it('checks network membership', () => {
    const net = parseCidr('192.168.1.0/24')!;
    expect(cidrContains(net, parseIPv4('192.168.1.200')!)).toBe(true);
    expect(cidrContains(net, parseIPv4('192.168.2.1')!)).toBe(false);
    expect(cidrContains(parseCidr('0.0.0.0/0')!, parseIPv4('8.8.8.8')!)).toBe(true);
    expect(cidrContains(parseCidr('10.1.2.3')!, parseIPv4('10.1.2.3')!)).toBe(true);
    expect(cidrContains(parseCidr('10.1.2.3')!, parseIPv4('10.1.2.4')!)).toBe(false);
    expect(cidrContains(parseCidr('128.0.0.0/1')!, parseIPv4('200.0.0.1')!)).toBe(true);
  });
  it('treats host bits in the network as ignored', () => {
    expect(cidrContains(parseCidr('192.168.1.77/24')!, parseIPv4('192.168.1.5')!)).toBe(true);
  });
});

describe('allowsClient', () => {
  it('matches address or network, ignores invalid entries', () => {
    expect(allowsClient(['192.168.1.0/24'], '192.168.1.5')).toBe(true);
    expect(allowsClient(['10.0.0.1', '192.168.1.5'], '192.168.1.5')).toBe(true);
    expect(allowsClient(['10.0.0.0/8', 'nonsense'], '192.168.1.5')).toBe(false);
    expect(allowsClient([], '192.168.1.5')).toBe(false);
    expect(allowsClient(['0.0.0.0/0'], '192.168.1.5')).toBe(true);
    expect(allowsClient(['0.0.0.0/0'], 'bad')).toBe(false);
  });
});

describe('lockoutServices', () => {
  const client = '192.168.1.5';
  it('is empty while the firewall is disabled', () => {
    expect(lockoutServices(settings(false), client)).toEqual([]);
  });
  it('reports web and ssh that do not allow the client', () => {
    expect(lockoutServices(settings(true), client)).toEqual(['web', 'ssh']);
    expect(lockoutServices(settings(true, { web: ['192.168.1.0/24'] }), client)).toEqual(['ssh']);
    expect(lockoutServices(settings(true, { web: [' 192.168.1.5 '], ssh: ['0.0.0.0/0'] }), client)).toEqual([]);
  });
  it('does not require smb or nfs', () => {
    expect(lockoutServices(settings(true, { web: [client], ssh: [client] }), client)).toEqual([]);
  });
});

describe('helpers', () => {
  it('cleans and validates lists', () => {
    expect(cleanList([' 1.2.3.4 ', '', '  '])).toEqual(['1.2.3.4']);
    expect(invalidEntries(['1.2.3.4', 'x', '10.0.0.0/33'])).toEqual(['x', '10.0.0.0/33']);
    expect(isAnywhere(['10.0.0.0/8', '0.0.0.0/0'])).toBe(true);
    expect(isAnywhere(['10.0.0.0/8'])).toBe(false);
  });
  it('compares settings ignoring blank rows', () => {
    expect(sameFirewall(settings(true, { web: ['1.2.3.4', ''] }), settings(true, { web: ['1.2.3.4'] }))).toBe(true);
    expect(sameFirewall(settings(true), settings(false))).toBe(false);
  });
});
