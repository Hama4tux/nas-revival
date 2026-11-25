// Development-only fake API. Imported dynamically from main.tsx behind import.meta.env.VITE_MOCK,
// so it never reaches the production bundle.
import type { Overview, Smart } from '../api/types';

const start = Date.now();
let loggedIn = false;
let fails = 0;

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
const err = (status: number, code: string, message: string, h?: Record<string, string>) =>
  json(status, { error: { code, message } }, h);

const wave = (period: number, amp: number, base: number) =>
  base + amp * Math.sin((Date.now() - start) / 1000 / period);

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
      ],
      fans: [{ id: 'lm85/fan1', label: 'fan1', rpm: 1800 }],
    },
    network: [
      {
        name: 'eth0', up: true, speedMbps: 1000, rxBytes: 123456789012 + Math.floor(t * 50000),
        txBytes: 45678901234 + Math.floor(t * 20000), addresses: ['192.0.2.10/24', 'fe80::1/64'],
      },
      { name: 'eth1', up: false, speedMbps: null, rxBytes: 0, txBytes: 0, addresses: [] },
    ],
    storage: {
      disks: [
        { name: 'sda', model: 'ST1000LM014', sizeBytes: 1000204886016, rotational: true, removable: false },
        { name: 'sdb', model: 'WDC WD20EZRX', sizeBytes: 2000398934016, rotational: true, removable: false },
      ],
      filesystems: [
        { mount: '/srv/data', device: '/dev/sda3', fsType: 'ext4', totalBytes: 950e9, usedBytes: 700e9, availableBytes: 250e9 },
        { mount: '/srv/backup', device: '/dev/sdb1', fsType: 'ext4', totalBytes: 1900e9, usedBytes: 1780e9, availableBytes: 120e9 },
        { mount: '/', device: '/dev/sda1', fsType: 'ext4', totalBytes: 8e9, usedBytes: 7.8e9, availableBytes: 0.2e9 },
      ],
    },
    services: [
      { name: 'ssh.service', label: 'SSH', state: 'active' },
      { name: 'smbd.service', label: 'Samba', state: 'active' },
      { name: 'nfs-server.service', label: 'NFS', state: 'inactive' },
      { name: 'minidlna.service', label: 'DLNA', state: 'failed' },
      { name: 'cron.service', label: 'Cron', state: 'unknown' },
    ],
  };
}

const smart = (): Smart => ({
  checkedAt: new Date().toISOString(),
  disks: [
    { name: 'sda', model: 'ST1000LM014', health: 'passed', temperatureCelsius: 38, powerOnHours: 1200, reallocatedSectors: 0, pendingSectors: 0, standby: false },
    { name: 'sdb', model: 'WDC WD20EZRX', health: 'unknown', temperatureCelsius: null, powerOnHours: null, reallocatedSectors: null, pendingSectors: null, standby: true },
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
    return err(404, 'not_found', 'Not found');
  };
}
