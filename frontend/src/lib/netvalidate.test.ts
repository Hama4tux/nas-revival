import { describe, expect, it } from 'vitest';
import {
  checkHostAddress, isHostname, sameNetConfig, isValidIPv6, parseIPv4, prefixToMask, validateDns, validateNtp, validateStatic,
} from './netvalidate';

describe('parseIPv4', () => {
  it('parses valid and rejects malformed', () => {
    expect(parseIPv4('192.168.1.10')).toBe(0xc0a8010a);
    for (const bad of ['', '1.2.3', '1.2.3.4.5', '256.1.1.1', '01.2.3.4', 'a.b.c.d', '1.2.3.-4', ' 1.2.3.4x'])
      expect(parseIPv4(bad)).toBeNull();
  });
});

describe('prefixToMask', () => {
  it('computes masks', () => {
    expect(prefixToMask(24)).toBe('255.255.255.0');
    expect(prefixToMask(8)).toBe('255.0.0.0');
    expect(prefixToMask(30)).toBe('255.255.255.252');
  });
});

describe('checkHostAddress', () => {
  it('rejects special addresses', () => {
    expect(checkHostAddress('127.0.0.1', 24)).toBe('loopback');
    expect(checkHostAddress('224.0.0.5', 24)).toBe('multicast');
    expect(checkHostAddress('169.254.1.1', 16)).toBe('linkLocal');
    expect(checkHostAddress('192.168.1.0', 24)).toBe('network');
    expect(checkHostAddress('192.168.1.255', 24)).toBe('broadcast');
    expect(checkHostAddress('0.1.2.3', 24)).toBe('unspecified');
    expect(checkHostAddress('nope', 24)).toBe('invalid');
    expect(checkHostAddress('192.168.0.255', 23)).toBeNull();
    expect(checkHostAddress('192.168.1.51', 24)).toBeNull();
  });
});

describe('validateStatic', () => {
  const ok = { address: '192.168.1.51', prefix: '24', gateway: '192.168.1.1', dns: ['192.168.1.1'] };
  it('accepts a valid config', () => expect(validateStatic(ok)).toEqual({}));
  it('checks prefix range 8-30', () => {
    expect(validateStatic({ ...ok, prefix: '7' }).prefixLength).toBe('prefix');
    expect(validateStatic({ ...ok, prefix: '31' }).prefixLength).toBe('prefix');
    expect(validateStatic({ ...ok, prefix: '' }).prefixLength).toBe('prefix');
    expect(validateStatic({ ...ok, prefix: '8', address: '10.1.2.3', gateway: '10.1.2.1' })).toEqual({});
  });
  it('checks gateway', () => {
    expect(validateStatic({ ...ok, gateway: '192.168.2.1' }).gateway).toBe('outsideSubnet');
    expect(validateStatic({ ...ok, gateway: '192.168.1.51' }).gateway).toBe('sameAsAddress');
    expect(validateStatic({ ...ok, gateway: '192.168.1.255' }).gateway).toBe('invalid');
    expect(validateStatic({ ...ok, gateway: 'x' }).gateway).toBe('invalid');
  });
  it('requires a DNS server', () => expect(validateStatic({ ...ok, dns: [' '] }).dns).toBe('required'));
});

describe('validateDns', () => {
  it('accepts IPv4 and IPv6, max 3', () => {
    expect(validateDns(['1.1.1.1', '2606:4700:4700::1111'], true)).toBeNull();
    expect(validateDns([], false)).toBeNull();
    expect(validateDns(['1.1.1.1', '8.8.8.8', '9.9.9.9', '4.4.4.4'], true)).toBe('tooMany');
    expect(validateDns(['dns.example'], true)).toBe('invalid');
  });
});

describe('isValidIPv6', () => {
  it('works', () => {
    for (const g of ['::1', 'fe80::1', '2001:db8::ff00:42:8329', '1:2:3:4:5:6:7:8', '::ffff:1.2.3.4']) expect(isValidIPv6(g)).toBe(true);
    for (const b of ['1::2::3', '12345::1', '1:2:3:4:5:6:7', 'g::1', '1.2.3.4']) expect(isValidIPv6(b)).toBe(false);
  });
});

describe('hostnames and NTP', () => {
  it('validates RFC 1123 names', () => {
    expect(isHostname('tik.cesnet.cz')).toBe(true);
    expect(isHostname('0.debian.pool.ntp.org')).toBe(true);
    expect(isHostname('-bad.example')).toBe(false);
    expect(isHostname('bad_.example')).toBe(false);
    expect(isHostname('a..b')).toBe(false);
    expect(isHostname('a'.repeat(64))).toBe(false);
    expect(isHostname('1.2.3')).toBe(false);
  });
  it('validates the server list', () => {
    expect(validateNtp([])).toBeNull();
    expect(validateNtp(['tik.cesnet.cz', '192.0.2.1'])).toBeNull();
    expect(validateNtp(['a.cz', 'b.cz', 'c.cz', 'd.cz', 'e.cz'])).toBe('tooMany');
    expect(validateNtp([''])).toBe('empty');
    expect(validateNtp(['bad host'])).toBe('invalid');
    expect(validateNtp(['a.cz', 'A.cz'])).toBe('duplicate');
  });
});

describe('sameNetConfig', () => {
  const stored = { mode: 'static' as const, address: '192.0.2.4', prefixLength: 24, gateway: '192.0.2.1', dns: ['192.0.2.1'], useDhcpDns: true };
  it('ignores useDhcpDns for a static config (no false "changed")', () => {
    expect(sameNetConfig({ ...stored, useDhcpDns: false }, stored)).toBe(true);
  });
  it('sees real changes', () => {
    expect(sameNetConfig({ ...stored, dns: ['192.0.2.1', '192.0.2.2'] }, stored)).toBe(false);
    expect(sameNetConfig({ ...stored, address: '192.0.2.5' }, stored)).toBe(false);
    const dhcp = { mode: 'dhcp' as const, address: null, prefixLength: null, gateway: null, dns: [], useDhcpDns: true };
    expect(sameNetConfig({ ...dhcp, useDhcpDns: false }, dhcp)).toBe(false);
  });
});
