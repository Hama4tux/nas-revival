// Development-only fake API. Imported dynamically from main.tsx behind import.meta.env.VITE_MOCK,
// so it never reaches the production bundle.
import type { NetConfig, NetworkSettings, Overview, Smart, TimeSettings, TlsInfo } from '../api/types';

const start = Date.now();
let loggedIn = false;
let fails = 0;

/* ---------- settings state (module-level, lives until page reload) ---------- */
const HOST_ADDR = '192.0.2.10';
let netConfig: NetConfig = { mode: 'dhcp', address: null, prefixLength: null, gateway: null, dns: [], useDhcpDns: true };
let netCurrent = { addresses: [`${HOST_ADDR}/24`], gateway: '192.0.2.1' as string | null, dns: ['192.0.2.1'] };
let netPending: { deadline: number; previous: NetConfig; next: NetConfig; prevCurrent: typeof netCurrent } | null = null;
let ntp: string[] = [];
const DEFAULT_NTP = ['0.debian.pool.ntp.org', '1.debian.pool.ntp.org'];
const day = 86400e3;
const makeCert = (selfSigned: boolean, notBefore: number, notAfter: number): TlsInfo => ({
  subject: selfSigned ? 'CN=nas' : 'CN=nas.example.org', issuer: selfSigned ? 'CN=nas' : 'CN=Mock CA',
  selfSigned, dnsNames: ['nas'], ipAddresses: [HOST_ADDR], notBefore: new Date(notBefore).toISOString(),
  notAfter: new Date(notAfter).toISOString(),
  sha256: Array.from({ length: 32 }, () => Math.floor(Math.random() * 256).toString(16).padStart(2, '0').toUpperCase()).join(':'),
  keyType: 'ECDSA P-256',
});
let tls = makeCert(true, Date.now() - 100 * day, Date.now() + 725 * day);

/** Auto-rollback once the deadline passed (the real helper does this with a timer). */
function settle() {
  if (netPending && Date.now() >= netPending.deadline) {
    netConfig = netPending.previous; netCurrent = netPending.prevCurrent; netPending = null;
  }
}
const netBody = (): NetworkSettings => ({
  interface: 'enp1s0', config: netConfig, current: netCurrent,
  pending: netPending && { deadline: new Date(netPending.deadline).toISOString(), previous: netPending.previous, next: netPending.next },
});
const bad = (field: string, message: string) => json(400, { error: { code: 'invalid', message, field } });
const ip4 = (s: unknown): number | null => {
  const m = typeof s === 'string' ? /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s) : null;
  return m && m.slice(1).every((x) => Number(x) < 256) ? m.slice(1).reduce((a, x) => a * 256 + Number(x), 0) : null;
};

function putNetwork(c: NetConfig): Response {
  if (netPending) return err(409, 'change_pending', 'Jiná změna čeká na potvrzení.');
  if (c.mode === 'static') {
    const a = ip4(c.address), g = ip4(c.gateway), p = c.prefixLength;
    if (a == null) return bad('address', 'Neplatná IPv4 adresa.');
    if (p == null || p < 8 || p > 30) return bad('prefixLength', 'Délka prefixu musí být 8 až 30.');
    const mask = (0xffffffff << (32 - p)) >>> 0;
    if (g == null) return bad('gateway', 'Neplatná brána.');
    if (((a & mask) >>> 0) !== ((g & mask) >>> 0)) return bad('gateway', 'Brána neleží ve stejné podsíti jako adresa.');
    if (!c.dns.length) return bad('dns', 'Zadejte alespoň jeden DNS server.');
  }
  const previous = netConfig, prevCurrent = netCurrent;
  netConfig = c;
  netCurrent = c.mode === 'static'
    ? { addresses: [`${c.address}/${c.prefixLength}`], gateway: c.gateway, dns: c.dns }
    : { addresses: ['192.0.2.77/24'], gateway: '192.0.2.1', dns: c.useDhcpDns ? ['192.0.2.1'] : c.dns };
  netPending = { deadline: Date.now() + 180e3, previous, next: c, prevCurrent };
  return json(202, netBody());
}

const isHost = (s: string) => s.length <= 253 && s.split('.').every((l) => /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/.test(l));

function settings(path: string, method: string, body: unknown): Response | null {
  settle();
  if (path === '/settings/network' && method === 'GET') return json(200, netBody());
  if (path === '/settings/network' && method === 'PUT') return putNetwork(body as NetConfig);
  if (path === '/settings/network/confirm' && method === 'POST') {
    if (!netPending) return err(409, 'nothing_pending', 'Není co potvrzovat.');
    netPending = null;
    return json(200, netBody());
  }
  const timeBody = (): TimeSettings => ({
    ntpServers: ntp.length ? ntp : DEFAULT_NTP, defaultServers: ntp.length === 0, synchronized: true,
    serverName: (ntp[0] ?? DEFAULT_NTP[0])!, serverAddress: '192.0.2.123', timezone: 'Europe/Prague', now: new Date().toISOString(),
  });
  if (path === '/settings/time' && method === 'GET') return json(200, timeBody());
  if (path === '/settings/time' && method === 'PUT') {
    const l = (body as { ntpServers?: string[] }).ntpServers;
    if (!Array.isArray(l) || l.length > 4) return bad('ntpServers', 'Nejvýše 4 NTP servery.');
    if (l.some((x) => !isHost(x) && ip4(x) == null)) return bad('ntpServers', 'Neplatný název hostitele nebo IP adresa.');
    ntp = l;
    return json(200, timeBody());
  }
  if (path === '/settings/tls' && method === 'GET') return json(200, tls);
  if (path === '/settings/tls' && method === 'PUT') {
    const b = body as { certificate?: string; privateKey?: string };
    if (!b.certificate?.includes('BEGIN CERTIFICATE')) return bad('certificate', 'Certifikát není platný PEM.');
    if (!b.privateKey?.includes('PRIVATE KEY')) return bad('privateKey', 'Soukromý klíč není platný PEM.');
    if (b.privateKey.includes('MISMATCH')) return bad('privateKey', 'Soukromý klíč neodpovídá certifikátu.');
    tls = makeCert(false, Date.now() - 10 * day, Date.now() + 20 * day); // short validity shows the warning
    return json(200, tls);
  }
  if (path === '/settings/tls/generate' && method === 'POST') {
    tls = makeCert(true, Date.now(), Date.now() + 825 * day);
    return json(200, tls);
  }
  return null;
}

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
const err = (status: number, code: string, message: string, h?: Record<string, string>) =>
  json(status, { error: { code, message } }, h);

const wave = (period: number, amp: number, base: number) =>
  base + amp * Math.sin((Date.now() - start) / 1000 / period);

const ago = (s: number) => new Date(start - s * 1000).toISOString();

function overview(): Overview {
  const t = (Date.now() - start) / 1000;
  const first = t < 6;
  return {
    generatedAt: new Date().toISOString(),
    host: {
      hostname: 'nas', os: 'Debian GNU/Linux 13 (trixie)', kernel: '6.12.119',
      uptimeSeconds: 3 * 86400 + 5400 + Math.floor(t), boardModel: 'ZyXEL NSA310',
    },
    cpu: {
      model: 'Feroceon 88FR131 rev 1 (v5l)', cores: 1, load1: 0.12, load5: 0.1, load15: 0.05,
      usagePercent: first ? null : Math.max(0.5, wave(20, 12, 15) + Math.random() * 4),
    },
    memory: {
      totalBytes: 262144000, availableBytes: Math.round(wave(30, 8e6, 150e6)),
      swapTotalBytes: 1073741824, swapFreeBytes: 1073741824 - 12 * 1024 * 1024,
    },
    sensors: {
      status: 'ok',
      temperatures: [
        { id: 'lm85/temp1', label: 'temp1', milliCelsius: Math.round(wave(40, 3000, 44000)) },
        { id: 'lm85/temp2', label: 'temp2', milliCelsius: 38500 },
        { id: 'lm85/temp3', label: 'temp3', milliCelsius: 41200 },
      ],
      fans: [{ id: 'lm85/fan1', label: 'Ventilátor', rpm: Math.round(wave(25, 150, 9000)) }],
    },
    network: [
      {
        name: 'eth0', up: true, speedMbps: 1000, rxBytes: 123456789012 + Math.floor(t * 900000 + 400000 * Math.sin(t / 7) * 5),
        txBytes: 45678901234 + Math.floor(t * 120000), addresses: ['192.0.2.10/24', 'fe80::1/64'],
      },
      { name: 'eth1', up: false, speedMbps: null, rxBytes: 0, txBytes: 0, addresses: [] },
    ],
    storage: {
      disks: [
        { name: 'sda', model: 'ST1000LM014-SSHD-8GB', sizeBytes: 1000204886016, rotational: true, removable: false },
        { name: 'sdb', model: 'Cruzer Fit', sizeBytes: 15376318464, rotational: false, removable: true },
      ],
      filesystems: [
        { mount: '/', device: '/dev/sda2', fsType: 'ext4', totalBytes: 31.5e9, usedBytes: 0.54e9, availableBytes: 30e9 },
        { mount: '/srv/data', device: '/dev/sda3', fsType: 'ext4', totalBytes: 966e9, usedBytes: 2.2e6, availableBytes: 966e9 },
        { mount: '/media/usb', device: '/dev/sdb1', fsType: 'vfat', totalBytes: 15.3e9, usedBytes: 13.9e9, availableBytes: 1.4e9 },
      ],
    },
    services: [
      { name: 'ssh.service', label: 'SSH', state: 'active', since: ago(3 * 86400 + 5400) },
      { name: 'smbd.service', label: 'Samba', state: 'active', since: ago(2 * 3600) },
      { name: 'nfs-server.service', label: 'NFS', state: 'inactive', since: null },
      { name: 'systemd-resolved.service', label: 'DNS', state: 'active', since: ago(3 * 86400 + 5395) },
      { name: 'nsa310-leds.service', label: 'LED', state: 'done', since: ago(3 * 86400 + 5390) },
      { name: 'nsa310-fan.service', label: 'Ventilátor', state: 'active', since: ago(3 * 86400 + 5390) },
      { name: 'nsa310-button.service', label: 'Tlačítko COPY', state: 'failed', since: ago(25 * 60) },
      { name: 'cron.service', label: 'Plánovač', state: 'unknown', since: null },
    ],
  };
}

const smart = (): Smart => ({
  checkedAt: new Date().toISOString(),
  disks: [
    { name: 'sda', model: 'ST1000LM014-SSHD-8GB', health: 'passed', temperatureCelsius: 41, powerOnHours: 6866, reallocatedSectors: 0, pendingSectors: 0, standby: false },
  ],
});

export function installMock(): void {
  const real = window.fetch.bind(window);
  window.fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.href);
    if (!url.pathname.startsWith('/api/v1')) return real(input, init);
    await new Promise((r) => setTimeout(r, 250));
    const path = url.pathname.slice('/api/v1'.length);
    const method = (init?.method ?? 'GET').toUpperCase();
    const h = new Headers(init?.headers);
    if (method !== 'GET' && h.get('X-Requested-With') !== 'nas-revival') return err(403, 'csrf', 'Missing header');

    if (path === '/session') {
      if (method === 'POST') {
        const b = JSON.parse(String(init?.body ?? '{}')) as { username?: string; password?: string };
        if (fails >= 5) return err(429, 'rate_limited', 'Too many attempts', { 'Retry-After': '240' });
        if (b.password !== 'demo') { fails++; return err(401, 'invalid_credentials', 'Invalid credentials'); }
        fails = 0; loggedIn = true;
        return new Response(null, { status: 204 });
      }
      if (method === 'DELETE') { loggedIn = false; return new Response(null, { status: 204 }); }
      return loggedIn
        ? json(200, { username: 'admin', expiresAt: new Date(Date.now() + 12 * 3600e3).toISOString() })
        : err(401, 'unauthorized', 'Login required');
    }
    if (!loggedIn) return err(401, 'unauthorized', 'Login required');
    if (path === '/overview') return json(200, overview());
    if (path === '/smart') return json(200, smart());
    if (path.startsWith('/settings/')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = settings(path, method, body);
      if (r) return r;
    }
    return err(404, 'not_found', 'Not found');
  };
}
