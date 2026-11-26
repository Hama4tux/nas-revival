// Development-only fake API. Imported dynamically from main.tsx behind import.meta.env.VITE_MOCK,
// so it never reaches the production bundle.
import type { Role, DiskPower, DiskPowerHistory, NetConfig, PowerSettings, NetworkSettings, Overview, Smart, SmartAttribute, SmartDetail, StorageReport, SystemPowerAction, PowerBlocker, TimeSettings, TlsInfo, ConfigEntry, ConfigDiffLine, UserInfo, UserList, Share, ShareInfo, ShareList, ShareVolume, UserAccess, ShareAccess, ServiceInfo, ServiceAction, SmbService, SmbSettings, SmbStatus, NfsService, NfsSettings, NfsStatus, SshService, SshSettings, SshKeys, NetworkInfo, DiagResult, MonitorHistory, MonitorSample, ProcessInfo, FanProfile, FanProfileId, HardwareSettings, HardwareState, HardwareHistory, HardwareThresholds, LedSettings } from '../api/types';
import { ApiError } from '../api/client';
import { setUploadImpl } from '../lib/upload';

const start = Date.now();
let loggedIn = false;
let fails = 0;
/** Log in as "viewer" or "user" (any other name = admin) to try the roles. */
let sessionUser = 'admin';
const sessionRole = (): Role => (sessionUser === 'viewer' ? 'viewer' : sessionUser === 'user' ? 'user' : 'admin');

/* ---------- hardware (W3.4) ---------- */
const hwProfiles: FanProfile[] = [
  { id: 'quiet', zoneMinC: 45, zoneMaxC: 65, pwmMin: 52 },
  { id: 'balanced', zoneMinC: 40, zoneMaxC: 60, pwmMin: 64 },
  { id: 'performance', zoneMinC: 35, zoneMaxC: 55, pwmMin: 102 },
];
let hwSettings: HardwareSettings = {
  fanProfile: 'balanced', thresholds: { boardWarnC: 55, boardCritC: 65, diskWarnC: 50, diskCritC: 55 },
  leds: { night: { enabled: false, start: '22:00', end: '07:00' }, faultBlink: true },
};
function hwState(): HardwareState {
  const p = hwProfiles.find((x) => x.id === hwSettings.fanProfile)!;
  const wave = Math.sin(Date.now() / 600_000);
  const led = (name: string, trigger: string, on: boolean) => ({ name, trigger, on, red: name.includes(':red:') });
  return {
    settings: hwSettings, profiles: hwProfiles, zoneCritC: 70,
    fan: { mode: 'auto', pwm: 98, pwmPercent: 38, zoneMinC: p.zoneMinC, zoneMaxC: p.zoneMaxC, pwmMin: p.pwmMin, profile: p.id },
    fanRpm: 4590,
    sensors: [
      { id: 'lm85/temp1', label: 'Dioda 1', kind: 'board', c: 37 + Math.round(wave * 2) },
      { id: 'lm85/temp2', label: 'Čip LM85', kind: 'board', c: 38 },
      { id: 'lm85/temp3', label: 'Dioda 2', kind: 'board', c: 43 + Math.round(wave * 3) },
      { id: 'disk/sda', label: 'sda · WDC WD20EFPX-68C4TN0', kind: 'disk', c: 51 },
    ],
    leds: [
      led('enp1s0-0::lan', 'netdev', true), led('enp1s0-1::lan', 'netdev', false), led('enp1s0-2::lan', 'netdev', true),
      led('nsa310:green:copy', 'none', false), led('nsa310:green:hdd', 'disk-activity', false), led('nsa310:green:sys', 'default-on', true),
      led('nsa310:green:usb', 'usbport', true), led('nsa310:red:sys', 'panic', false),
    ],
  };
}
function hwHistory(since: number): HardwareHistory {
  const now = Math.floor(Date.now() / 60_000) * 60_000;
  const points = [];
  for (let i = 6 * 60; i >= 0; i--) {
    const t = now - i * 60_000;
    if (t <= since) continue;
    const w = Math.sin(t / 3_600_000);
    points.push({ t, temps: { 'lm85/temp1': 37 + w * 2, 'lm85/temp2': 38, 'lm85/temp3': 43 + w * 4, 'disk/sda': 46 + w * 5 }, fanRpm: 4500, pwmPercent: 35 + Math.round(w * 6) });
  }
  return { intervalSeconds: 60, points };
}

/* ---------- settings state (module-level, lives until page reload) ---------- */
const HOST_ADDR = '192.0.2.10';
let netConfig: NetConfig = { mode: 'dhcp', address: null, prefixLength: null, gateway: null, dns: [], useDhcpDns: true };
let netCurrent = { addresses: [`${HOST_ADDR}/24`], gateway: '192.0.2.1' as string | null, dns: ['192.0.2.1'] };
let netPending: { deadline: number; previous: NetConfig; next: NetConfig; prevCurrent: typeof netCurrent } | null = null;
let ntp: string[] = [];
let powerToken: string | null = null;
let downUntil: { from: number; to: number } | null = null;
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

let mockHostname = 'nas';
const netInfo = (): NetworkInfo => ({
  hostname: mockHostname, domains: ['home'], checkedAt: new Date().toISOString(),
  interfaces: [{ name: 'enp1s0', mac: '02:00:00:00:00:01', mtu: 1500, state: 'up', carrier: true, speedMbps: 1000, duplex: 'full',
    linkChanges: 1, addresses: [...netCurrent.addresses, 'fe80::1/64'] }],
  routes: [
    ...(netCurrent.gateway ? [{ family: 'inet' as const, destination: 'default', gateway: netCurrent.gateway, device: 'enp1s0', protocol: 'static', metric: null }] : []),
    { family: 'inet', destination: '192.0.2.0/24', device: 'enp1s0', protocol: 'kernel', scope: 'link', source: HOST_ADDR, metric: null },
    { family: 'inet6', destination: 'fe80::/64', device: 'enp1s0', protocol: 'kernel', metric: 256 },
  ],
});

function mockDiag({ test, target = '' }: { test: DiagResult['test']; target?: string }): DiagResult {
  const ping = (ok: boolean) => ({ address: ok ? '192.0.2.1' : target, sent: 4, received: ok ? 4 : 0, lossPercent: ok ? 0 : 100,
    minMs: ok ? 0.31 : null, avgMs: ok ? 0.42 : null, maxMs: ok ? 0.6 : null });
  switch (test) {
    case 'gateway': return { test, ok: true, durationMs: 3012, ping: ping(true), addresses: ['192.0.2.1'] };
    case 'ping': {
      const ok = !target.startsWith('192.0.2.9');
      return { test, target, ok, durationMs: ok ? 3008 : 5030, ping: ping(ok),
        message: ok ? undefined : 'Cíl neodpověděl. Může být vypnutý, mimo síť, nebo odpovědi na ping blokuje.' };
    }
    case 'dns': return target.endsWith('.invalid')
      ? { test, target, ok: false, durationMs: 40, message: 'Název se nepodařilo přeložit. Zkontrolujte DNS servery v nastavení sítě.' }
      : { test, target, ok: true, durationMs: 23, addresses: ['192.0.2.80', '2001:db8::80'] };
    case 'internet': return { test, ok: true, durationMs: 85, steps: [
      { name: 'dns', ok: true, detail: 'deb.debian.org → 192.0.2.80', durationMs: 21 },
      { name: 'connect', ok: true, detail: '192.0.2.80:443', durationMs: 64 }] };
    case 'ports': return { test, ok: true, durationMs: 140, ports: [
      { proto: 'tcp', address: '0.0.0.0', port: 22, process: 'sshd' }, { proto: 'udp', address: '127.0.0.53', port: 53, process: 'systemd-resolve' },
      { proto: 'tcp', address: '0.0.0.0', port: 445, process: 'smbd' }, { proto: 'tcp', address: '0.0.0.0', port: 2049, process: '' },
      { proto: 'tcp', address: '::', port: 8443, process: 'nas-revival' }] };
    case 'connections': return { test, ok: true, durationMs: 120, connections: [
      { localPort: 22, peer: '192.0.2.155', peerPort: 55120, process: 'sshd-session' },
      { localPort: 8443, peer: '192.0.2.155', peerPort: 60300, process: 'nas-revival' }] };
  }
}

const isHost = (s: string) => s.length <= 253 && s.split('.').every((l) => /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/.test(l));

function settings(path: string, method: string, body: unknown): Response | null {
  settle();
  if (path === '/system/info' && method === 'GET') {
    return json(200, {
      version: 'mock', hostname: 'nas', boardModel: 'ZyXEL NSA310', os: 'Debian GNU/Linux 13 (trixie)', debianVersion: '13.8',
      kernel: '6.12.119', architecture: 'armv5tel', cpuModel: 'Feroceon 88FR131 rev 1 (v5l)', cores: 1, memTotalBytes: 252043264,
      bootTime: new Date(Date.now() - 3 * 3600e3).toISOString(), uptimeSeconds: 3 * 3600, lastPackageChange: new Date(Date.now() - 3600e3).toISOString(),
    });
  }
  if (path === '/system/capabilities' && method === 'GET') {
    const caps: [string, string][] = [['temperatures', 'verified'], ['fanSpeed', 'verified'], ['fanControl', 'verified'], ['leds', 'verified'],
      ['copyButton', 'verified'], ['smart', 'verified'], ['diskStandby', 'unverified'], ['kernelSlots', 'verified'], ['rtcWake', 'unverified'],
      ['wakeOnLan', 'unverified'], ['autoPowerOn', 'unsupported'], ['raid', 'unsupported']];
    return json(200, { profileId: 'zyxel-nsa310', profileName: 'ZyXEL NSA310', sataBays: 1, usbPorts: 2,
      capabilities: caps.map(([id, status]) => ({ id, status })) });
  }
  const pw = /^\/system\/(reboot|shutdown)(\/preview)?$/.exec(path);
  if (pw && method === 'POST') {
    const action = pw[1] as SystemPowerAction;
    if (pw[2]) {
      powerToken = `mock-${Date.now()}`;
      const blockers: PowerBlocker[] = smartDetail().selfTest?.running ? [{ code: 'smart_test', diskId: 'wwn-0x5000c500a1b2c3d4' }] : [];
      return json(200, {
        action, hostname: 'nas', confirmToken: powerToken, expiresAt: new Date(Date.now() + 60e3).toISOString(),
        effects: ['web_offline', 'connections_drop', 'sessions_end', action === 'reboot' ? 'back_in_minutes' : 'power_button'], blockers,
      });
    }
    const b = body as { confirmToken?: string; force?: boolean };
    if (!powerToken || b.confirmToken !== powerToken) return err(409, 'confirm_required', 'Potvrzení vypršelo.');
    powerToken = null;
    // Simulate: down after 4 s, back (restart) after 15 s.
    downUntil = { from: Date.now() + 4000, to: action === 'reboot' ? Date.now() + 15000 : Infinity };
    return json(202, { action, forced: !!b.force, blockers: [] });
  }
  if (path === '/network' && method === 'GET') return json(200, netInfo());
  if (path === '/network/hostname' && method === 'PUT') {
    const h = String((body as { hostname?: string }).hostname ?? '').trim().toLowerCase();
    if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(h) || /^\d+$/.test(h) || h === 'localhost') return bad('hostname', 'Neplatný název NASu.');
    mockHostname = h;
    return json(200, netInfo());
  }
  if (path === '/network/diagnostics' && method === 'POST') return json(200, mockDiag(body as { test: DiagResult['test']; target?: string }));
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

/* ---------- config history ---------- */
const NET_PATH = '/etc/systemd/network/20-wired.network', NTP_PATH = '/etc/systemd/timesyncd.conf.d/50-nas-revival.conf';
const mockNet = (dhcp: boolean) => `# Managed by nas-revival.\n[Match]\nName=enp1s0\n\n[Network]\n${dhcp ? 'DHCP=yes' : 'Address=192.0.2.60/24\nGateway=192.0.2.1\nDNS=192.0.2.1'}\n`;
const mockVersions: { entry: ConfigEntry; content: Record<string, string | null> }[] = [
  { entry: { id: 1, time: new Date(Date.now() - 5 * day).toISOString(), user: 'system', kind: 'baseline', files: [], changed: ['network'] },
    content: { network: mockNet(true), time: null, 'disk-power': null } },
  { entry: { id: 2, time: new Date(Date.now() - 2 * day).toISOString(), user: 'admin', kind: 'change', module: 'time', files: [], changed: ['time'] },
    content: { network: mockNet(true), time: '# Managed by nas-revival.\n[Time]\nNTP=tik.cesnet.cz tak.cesnet.cz\n', 'disk-power': null } },
  { entry: { id: 3, time: new Date(Date.now() - 3600e3).toISOString(), user: 'admin', kind: 'change', module: 'network', files: [], changed: ['network'] },
    content: { network: mockNet(false), time: '# Managed by nas-revival.\n[Time]\nNTP=tik.cesnet.cz tak.cesnet.cz\n', 'disk-power': null } },
];
const mockPaths: Record<string, string> = { network: NET_PATH, time: NTP_PATH, 'disk-power': '/etc/nas-revival/disks.json' };
const mockLines = (a: string | null, b: string | null): ConfigDiffLine[] => {
  const x = a ? a.trimEnd().split('\n') : [], y = b ? b.trimEnd().split('\n') : [];
  return [...x.filter((l) => !y.includes(l)).map((text) => ({ op: '-' as const, text })),
    ...y.map((text) => ({ op: x.includes(text) ? ' ' as const : '+' as const, text }))];
};

function activity(seconds: number) {
  const dev = (name: string, parent: string, writeOps: number, extra: Record<string, unknown> = {}) => ({
    name, parent, mount: '', fsType: '', swap: false, journalCommits: null,
    readOps: 0, readBytes: 0, writeOps, writeBytes: writeOps * 8192, ...extra,
  });
  const journal = '/var/log/journal/0123456789abcdef/system.journal';
  const devices = [
    dev('sda', '', 4), dev('sda1', 'sda', 4, { mount: '/', fsType: 'ext4', journalCommits: 2 }),
    dev('sda2', 'sda', 0, { swap: true }), dev('sda3', 'sda', 0, { mount: '/srv/data', fsType: 'ext4', journalCommits: 0 }),
    dev('sdb', '', 0), dev('sdb1', 'sdb', 0, { mount: '/boot', fsType: 'ext2' }),
  ];
  return {
    startedAt: new Date(Date.now() - seconds * 1000).toISOString(), seconds, processIo: false, devices,
    swapInPages: 0, swapOutPages: 0,
    processes: [{ pid: 312, comm: 'systemd-journal', unit: 'systemd-journald.service', readBytes: null, writeBytes: null, files: [journal] }],
    files: [{ path: journal, device: 'sda1', sizeDelta: 0, processes: ['systemd-journal (312)'] },
      { path: '/var/lib/smartmontools/attrlog.csv', device: 'sda1', sizeDelta: null, processes: [] }],
    filesTruncated: false, notScanned: ['/boot'],
    disks: [
      { id: 'wwn-0x5000c50000000001', name: 'sda', system: true, ...devices[0], partitions: devices.slice(1, 4),
        hints: [{ code: 'no_standby' }, { code: 'journald' }, { code: 'shared_data' }] },
      { id: 'usb-Generic_Flash_Disk-0:0', name: 'sdb', system: false, ...devices[4], partitions: devices.slice(5), hints: [{ code: 'idle' }] },
    ],
  };
}

function configRoute(path: string, method: string): Response | null {
  const latest = mockVersions[mockVersions.length - 1]!;
  if (path === '/config/history' && method === 'GET') {
    return json(200, { entries: [...mockVersions].reverse().map((v) => v.entry), maxEntries: 20, maxBytes: 2 << 20, usedBytes: 4096,
      drift: Object.keys(mockPaths).map((id) => ({ id, path: mockPaths[id], state: netPending && id === 'network' ? 'pending' : 'ok' })) });
  }
  const m = /^\/config\/(history|rollback)\/([1-9][0-9]*)$/.exec(path);
  const v = m && mockVersions.find((x) => x.entry.id === Number(m[2]));
  if (!m) return null;
  if (!v) return err(404, 'not_found', 'Tato verze konfigurace v historii není.');
  const files = Object.keys(mockPaths).map((id) => {
    const cur = latest.content[id] ?? null, old = v.content[id] ?? null;
    return { id, path: mockPaths[id], same: cur === old, present: old !== null, currentPresent: cur !== null,
      lines: cur === old ? null : mockLines(cur, old), tooLarge: false };
  });
  if (m[1] === 'history' && method === 'GET') return json(200, { entry: v.entry, files, drift: [] });
  if (m[1] === 'rollback' && method === 'POST') {
    const restored = files.filter((f) => !f.same).map((f) => f.id);
    if (restored.length) {
      mockVersions.push({ entry: { id: latest.entry.id + 1, time: new Date().toISOString(), user: 'admin', kind: 'rollback',
        rollbackOf: v.entry.id, files: [], changed: restored as ConfigEntry['changed'] }, content: { ...v.content } });
    }
    return json(200, { rollbackOf: v.entry.id, restored, networkPending: null });
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
        { name: 'sda', model: 'ST1000LM014-SSHD-8GB', sizeBytes: 1000204886016, rotational: true, removable: false, powerState: 'active' },
        { name: 'sdb', model: 'Cruzer Fit', sizeBytes: 15376318464, rotational: false, removable: true, powerState: null },
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
    backups: [
      { id: 'b1', name: 'Dokumenty z pracovní stanice', enabled: true, lastResult: 'ok', lastEnd: ago(5 * 3600), message: '', usedBytes: 42e9 },
      { id: 'b2', name: 'Fotky z notebooku', enabled: true, lastResult: 'failed', lastEnd: ago(26 * 3600), message: 'Server nedostupný', usedBytes: 118e9 },
    ],
    health: {
      status: 'crit', checkedAt: new Date().toISOString(), smartCheckedAt: ago(-5),
      problems: [
        { severity: 'crit', code: 'serviceFailed', subject: 'Tlačítko COPY' },
        { severity: 'crit', code: 'backupFailed', subject: 'Fotky z notebooku' },
        { severity: 'warn', code: 'fsWarn', subject: '/media/usb' },
      ],
    },
  };
}

const smart = (): Smart => ({
  checkedAt: new Date().toISOString(),
  disks: [
    { name: 'sda', model: 'ST1000LM014-SSHD-8GB', health: 'passed', temperatureCelsius: 41, powerOnHours: 6866, reallocatedSectors: 0, pendingSectors: 0, standby: false },
  ],
});

const GiB = 1024 ** 3;
const storage = (): StorageReport => ({
  disks: [
    {
      id: 'wwn-0x5000c50000000001', idStable: true, name: 'sda', model: 'ST1000LM014-SSHD-8GB', vendor: null, serial: 'MOCK0001',
      firmware: 'LVD4', bus: 'ata', type: 'sshd', rotationRpm: 5400, sizeBytes: 1000204886016, removable: false, smart: true, readOnly: false,
      partitionTable: 'gpt', system: true, roles: ['system', 'swap', 'data'], sharedWithData: true,
      partitions: [
        { name: 'sda1', number: 1, sizeBytes: 30 * GiB, partUuid: 'mock-0001', partLabel: null, fsType: 'ext4', fsUuid: 'mock-fs-1', label: null,
          swap: false, mounts: [{ point: '/', readOnly: false, bind: false, root: '/' }],
          usage: { totalBytes: 29.4 * GiB, usedBytes: 2.1 * GiB, availableBytes: 25.8 * GiB } },
        { name: 'sda2', number: 2, sizeBytes: 1 * GiB, partUuid: 'mock-0002', partLabel: null, fsType: 'swap', fsUuid: null, label: null,
          swap: true, mounts: [], usage: null },
        { name: 'sda3', number: 3, sizeBytes: 900.5 * GiB, partUuid: 'mock-0003', partLabel: 'nas-data', fsType: 'ext4', fsUuid: 'mock-fs-3', label: 'nasdata',
          swap: false, mounts: [{ point: '/srv/data', readOnly: false, bind: false, root: '/' }],
          usage: { totalBytes: 885 * GiB, usedBytes: 12.3 * GiB, availableBytes: 828 * GiB } },
      ],
    },
    {
      id: 'usb-SanDisk_Cruzer_Fit_MOCK0002-0:0', idStable: true, name: 'sdb', model: 'Cruzer Fit', vendor: "SanDisk'", serial: 'MOCK0002',
      firmware: '1.00', bus: 'usb', type: 'flash', rotationRpm: null, sizeBytes: 8 * GiB, removable: true, smart: false, readOnly: false,
      partitionTable: 'dos', system: false, roles: ['boot'], sharedWithData: false,
      partitions: [
        { name: 'sdb1', number: 1, sizeBytes: 0.5 * GiB, partUuid: 'mock-01', partLabel: null, fsType: 'ext2', fsUuid: null, label: null,
          swap: false, mounts: [{ point: '/boot', readOnly: false, bind: false, root: '/' }],
          usage: { totalBytes: 0.48 * GiB, usedBytes: 0.03 * GiB, availableBytes: 0.42 * GiB } },
        { name: 'sdb2', number: 2, sizeBytes: 4 * GiB, partUuid: 'mock-02', partLabel: null, fsType: 'ext4', fsUuid: null, label: 'rootfs',
          swap: false, mounts: [], usage: null },
      ],
    },
  ],
});

/* ---------- SMART detail: a self-test "runs" for 2 minutes after it is started ---------- */
let mockTest: { type: 'short' | 'long'; start: number } | null = null;
const TEST_MS = 120e3;
const a = (id: number, name: string, count: number, p: Partial<SmartAttribute> = {}): SmartAttribute => ({
  id, name, value: 100, worst: 100, thresh: 0, raw: String(count), rawValue: count, count, prefailure: false,
  whenFailed: '', key: [4, 5, 9, 10, 12, 187, 188, 193, 194, 197, 198, 199].includes(id), level: 'ok', ...p,
});
function smartDetail(): SmartDetail {
  const attrs = [
    a(1, 'Raw_Read_Error_Rate', 0, { value: 116, worst: 99, thresh: 6, prefailure: true, raw: '109331216', rawValue: 109331216 }),
    a(4, 'Start_Stop_Count', 9731, { value: 91, worst: 91, thresh: 20 }),
    a(5, 'Reallocated_Sector_Ct', 0, { thresh: 10, prefailure: true }),
    a(7, 'Seek_Error_Rate', 16, { value: 72, worst: 60, thresh: 30, prefailure: true, raw: '68992411861', rawValue: 68992411861 }),
    a(9, 'Power_On_Hours', 6870, { value: 93, worst: 93 }),
    a(12, 'Power_Cycle_Count', 10115, { value: 91, worst: 91, thresh: 20 }),
    a(187, 'Reported_Uncorrect', 0), a(188, 'Command_Timeout', 0),
    a(193, 'Load_Cycle_Count', 35015, { value: 83, worst: 83 }),
    a(194, 'Temperature_Celsius', 42, { value: 42, worst: 47, raw: '42 (0 6 0 0 0)' }),
    a(197, 'Current_Pending_Sector', 0), a(198, 'Offline_Uncorrectable', 0),
    a(199, 'UDMA_CRC_Error_Count', 2, { value: 200, worst: 200, level: 'warn' }),
  ].map((x) => (x.key && x.level === 'ok' ? { ...x, level: 'info' as const } : x));
  const history: SmartDetail['history'] = [
    { type: 'long', typeText: 'Extended offline', status: 'Interrupted (host reset)', passed: false, remainingPercent: 80, lifetimeHours: 6866 },
    { type: 'short', typeText: 'Short offline', status: 'Completed without error', passed: true, remainingPercent: null, lifetimeHours: 6865 },
  ];
  let selfTest: SmartDetail['selfTest'] = { value: 0, text: 'completed without error', running: false, remainingPercent: null, passed: true };
  if (mockTest) {
    const done = (Date.now() - mockTest.start) / TEST_MS;
    if (done < 1) {
      const rem = Math.max(10, Math.ceil((1 - done) * 10) * 10);
      selfTest = { value: 240 + rem / 10, text: `in progress, ${rem}% remaining`, running: true, remainingPercent: rem, passed: null };
    } else {
      history.unshift({ type: mockTest.type, typeText: mockTest.type === 'long' ? 'Extended offline' : 'Short offline',
        status: 'Completed without error', passed: true, remainingPercent: null, lifetimeHours: 6870 });
    }
  }
  return {
    checkedAt: new Date().toISOString(), standby: false, health: 'passed', model: 'ST1000LM014-SSHD-8GB', family: 'Seagate Laptop SSHD',
    temperatureCelsius: 42, powerOnHours: 6870, powerCycles: 10115, selfTestsSupported: true, shortMinutes: 1, longMinutes: 206,
    selfTest, attributes: attrs, history,
  };
}

function smartRoute(path: string, method: string, body: unknown): Response | null {
  const m = /^\/storage\/disks\/([^/]+)\/smart(\/test)?$/.exec(path);
  if (!m) return null;
  if (decodeURIComponent(m[1] ?? '') !== 'wwn-0x5000c50000000001') return err(404, 'smart_unsupported', 'Disk nepodporuje SMART.');
  if (!m[2] && method === 'GET') return json(200, smartDetail());
  if (m[2] && method === 'POST') {
    const type = (body as { type?: string }).type;
    if (type !== 'short' && type !== 'long') return bad('type', 'Typ testu musí být short nebo long.');
    if (smartDetail().selfTest?.running) return err(409, 'test_running', 'Na disku už běží samotest. Počkejte na jeho dokončení.');
    mockTest = { type, start: Date.now() };
    return json(202, { diskId: decodeURIComponent(m[1] ?? ''), type, estimatedMinutes: type === 'long' ? 206 : 1 });
  }
  return null;
}

let mockPower: { settings: PowerSettings; stored: boolean; state: DiskPower['state'] } = {
  settings: { profile: 'balanced', standbyMinutes: 0, apm: 254 }, stored: false, state: 'active',
};

function mockPowerHistory(): DiskPowerHistory {
  const now = Date.now(), h = 3600e3;
  const events: DiskPowerHistory['events'] = [{ t: now - 30 * h, state: 'active' }];
  for (const [sleep, wake] of [[20, 17.5], [9, 7.2], [5, 4.6], [3.1, 2.4]] as const) {
    events.push({ t: now - sleep * h, state: 'standby' }, { t: now - wake * h, state: 'active' });
  }
  const counters = Array.from({ length: 31 }, (_, i) => ({
    t: now - (30 - i) * h, loadCycles: 35003 + i * 6, startStop: 10115 + Math.floor(i / 6),
  }));
  return {
    events, counters, wakes24h: 4, wakes7d: 4, standbyPercent24h: 26.5, trackedSince: now - 30 * h, sampleSeconds: 300,
    growth: { loadCyclesPerDay: 144, startStopPerDay: 4.8, spanHours: 20, loadCycleLimit: 100, startStopLimit: 25,
      loadCyclesWarn: true, startStopWarn: false, latestLoadCycles: 35183, latestStartStop: 10120, latestCounterAt: now },
  };
}

function powerRoute(path: string, method: string, body: unknown): Response | null {
  const m = /^\/storage\/disks\/([^/]+)\/(power|standby|power\/history)$/.exec(path);
  if (!m) return null;
  const id = decodeURIComponent(m[1] ?? '');
  if (id !== 'wwn-0x5000c50000000001') return err(404, 'power_unsupported', 'Disk nepodporuje správu napájení.');
  if (m[2] === 'power/history') return method === 'GET' ? json(200, mockPowerHistory()) : null;
  const profiles: PowerSettings[] = [{ profile: 'performance', standbyMinutes: 0, apm: 254 },
    { profile: 'balanced', standbyMinutes: 0, apm: 254 }, { profile: 'eco', standbyMinutes: 20, apm: 128 }];
  const view = (): DiskPower => ({
    diskId: id, systemDisk: true, state: mockPower.state, checkedAt: new Date().toISOString(), apmSupported: true,
    apmLevel: mockPower.settings.apm ?? 254, settings: mockPower.settings, stored: mockPower.stored, profiles,
    standbyChoices: [0, 10, 20, 30, 60, 120, 180, 240, 300],
  });
  if (m[2] === 'standby' && method === 'POST') {
    const was = mockPower.state === 'standby';
    mockPower.state = 'standby';
    setTimeout(() => { mockPower.state = 'active'; }, 8000);
    return json(200, { diskId: id, wasStandby: was });
  }
  if (m[2] !== 'power') return null;
  if (method === 'GET') return json(200, view());
  if (method === 'PUT') {
    const b = body as Partial<PowerSettings> & { confirmSystemDisk?: boolean };
    const fixed = profiles.find((p) => p.profile === b.profile);
    const s: PowerSettings | undefined = fixed ?? (b.profile === 'custom'
      ? { profile: 'custom', standbyMinutes: b.standbyMinutes ?? 0, apm: b.apm ?? null } : undefined);
    if (!s) return bad('profile', 'Neznámý profil napájení.');
    if (s.standbyMinutes > 0 && !b.confirmSystemDisk) {
      return err(409, 'confirm_required', 'Potvrďte uspávání systémového disku.');
    }
    mockPower = { ...mockPower, settings: s, stored: true };
    return json(200, view());
  }
  return null;
}

/* ---------- users (W2.1) ---------- */
let mockUsers: UserInfo[] = [
  { name: 'jana', uid: 1001, fullName: 'Jana Nováková', groups: ['rodina'], role: 'admin', disabled: false,
    password: { linux: true, samba: 'unavailable', web: true, changedOutside: false, changed: new Date(start - 3 * day).toISOString() } },
  { name: 'petr', uid: 1002, fullName: '', groups: [], role: 'user', disabled: true,
    password: { linux: true, samba: 'unavailable', web: true, changedOutside: true } },
];
let mockGroups: { name: string; gid: number; members: string[] }[] = [{ name: 'rodina', gid: 1003, members: ['jana'] }];
const userList = (): UserList => ({
  samba: false,
  users: mockUsers.map((u) => ({ ...u, groups: mockGroups.filter((g) => g.members.includes(u.name)).map((g) => g.name) })),
  groups: [{ name: 'nas-users', gid: 990, builtin: true, members: mockUsers.map((u) => u.name) },
    ...mockGroups.map((g) => ({ ...g, builtin: false }))].sort((a, b) => a.name.localeCompare(b.name)),
});
const NAME = /^[a-z][a-z0-9_-]{0,31}$/;
const setMembership = (user: string, groups: string[]) => {
  mockGroups = mockGroups.map((g) => ({ ...g, members: groups.includes(g.name)
    ? [...new Set([...g.members, user])].sort() : g.members.filter((m) => m !== user) }));
};

function usersRoute(path: string, method: string, body: Record<string, unknown>): Response | null {
  const name = String(body.name ?? '');
  const pw = typeof body.password === 'string' ? body.password : '';
  if (path === '/users' && method === 'GET') return json(200, userList());
  if (path === '/groups' && method === 'GET') return json(200, { groups: userList().groups });
  if (path === '/users' && method === 'POST') {
    if (!NAME.test(name)) return bad('name', 'Jméno nesplňuje pravidla.');
    if (name.startsWith('nas-')) return bad('name', 'Jména začínající nas- jsou vyhrazená.');
    if (mockUsers.some((u) => u.name === name) || mockGroups.some((g) => g.name === name)) return bad('name', 'Uživatel s tímto jménem už existuje.');
    if (!pw) return bad('password', 'Heslo nesmí být prázdné.');
    mockUsers = [...mockUsers, { name, uid: 1000 + mockUsers.length + 4, fullName: String(body.fullName ?? '').trim(), groups: [],
      role: (body.role as Role | undefined) ?? 'user', disabled: false,
      password: { linux: true, samba: 'unavailable' as const, web: true, changedOutside: false, changed: new Date().toISOString() } }]
      .sort((a, b) => a.name.localeCompare(b.name));
    setMembership(name, (body.groups as string[] | undefined) ?? []);
    return json(201, userList());
  }
  if (path === '/groups' && method === 'POST') {
    if (!NAME.test(name) || name.startsWith('nas-')) return bad('name', 'Neplatné jméno skupiny.');
    if (mockGroups.some((g) => g.name === name) || mockUsers.some((u) => u.name === name)) return bad('name', 'Skupina s tímto jménem už existuje.');
    mockGroups = [...mockGroups, { name, gid: 1004 + mockGroups.length, members: [] }];
    return json(201, userList());
  }
  const g = /^\/groups\/([^/]+)(\/members)?$/.exec(path);
  if (g) {
    if (!mockGroups.some((x) => x.name === g[1])) return err(404, 'not_found', 'Skupina neexistuje nebo ji nelze spravovat.');
    if (g[2] && method === 'PUT') {
      const members = (body.members as string[] | undefined) ?? [];
      mockGroups = mockGroups.map((x) => (x.name === g[1] ? { ...x, members: [...members].sort() } : x));
      return json(200, userList());
    }
    if (!g[2] && method === 'DELETE') { mockGroups = mockGroups.filter((x) => x.name !== g[1]); return json(200, userList()); }
    return null;
  }
  const m = /^\/users\/([^/]+)(?:\/(password|disabled))?$/.exec(path);
  if (!m) return null;
  const u = mockUsers.find((x) => x.name === m[1]);
  if (!u) return err(404, 'not_found', 'Uživatel neexistuje nebo ho nelze spravovat.');
  const patch = (p: Partial<UserInfo>) => { mockUsers = mockUsers.map((x) => (x.name === u.name ? { ...x, ...p } : x)); };
  if (m[2] === 'password' && method === 'PUT') {
    if (!pw) return bad('password', 'Heslo nesmí být prázdné.');
    patch({ password: { ...u.password, web: true, changedOutside: false, changed: new Date().toISOString() } });
    return json(200, { samba: false });
  }
  if (m[2] === 'disabled' && method === 'PUT') { patch({ disabled: !!body.disabled }); return json(200, userList()); }
  if (!m[2] && method === 'PUT') {
    patch({ fullName: String(body.fullName ?? '').trim(), ...(body.role ? { role: body.role as Role } : {}) });
    setMembership(u.name, (body.groups as string[] | undefined) ?? []);
    return json(200, userList());
  }
  if (!m[2] && method === 'DELETE') {
    mockUsers = mockUsers.filter((x) => x.name !== u.name);
    setMembership(u.name, []);
    return json(200, userList());
  }
  return null;
}

/* ---------- shared folders (W2.3) ---------- */
const mockVolumes: ShareVolume[] = [
  { path: '/srv/data', fsType: 'ext4', totalBytes: 2e12, availBytes: 1.2e12, acl: false },
  { path: '/srv/usb1', fsType: 'ext4', totalBytes: 5e11, availBytes: 4.1e11, acl: true },
];
let mockShares: Share[] = [
  { name: 'Fotky', description: 'Rodinné fotky', volume: '/srv/data', folder: 'Fotky', readOnly: false, hidden: false,
    recycle: { enabled: true, days: 30 },
    permissions: [{ kind: 'group', name: 'rodina', access: 'rw' }, { kind: 'user', name: 'petr', access: 'none' }] },
  { name: 'Archiv', description: '', volume: '/srv/usb1', folder: 'Archiv', readOnly: true, hidden: true,
    recycle: { enabled: false, days: 30 }, permissions: [{ kind: 'group', name: 'nas-users', access: 'ro' }, { kind: 'user', name: 'jana', access: 'ro' }] },
];
const rank = (a: ShareAccess) => (a === 'rw' ? 2 : a === 'ro' ? 1 : 0);
function effective(s: Share): UserAccess[] {
  return mockUsers.map((u) => {
    const direct = s.permissions.find((p) => p.kind === 'user' && p.name === u.name);
    const groups = userList().groups.filter((g) => g.members.includes(u.name));
    let access: ShareAccess = 'none'; let via = '';
    if (direct) { access = direct.access; via = 'user'; } else {
      for (const p of s.permissions) {
        if (p.kind === 'group' && groups.some((g) => g.name === p.name) && rank(p.access) > rank(access)) { access = p.access; via = `group:${p.name}`; }
      }
    }
    if (s.readOnly && access === 'rw') access = 'ro';
    return { user: u.name, access, via };
  });
}
const shareInfo = (s: Share): ShareInfo => ({
  ...s, path: `${s.volume}/${s.folder}`, exists: true,
  enforcement: mockVolumes.find((v) => v.path === s.volume)?.acl ? 'acl' : 'smb',
  effective: effective(s),
  conflicts: s.permissions.filter((p) => p.kind === 'user' && p.access === 'none' &&
    s.permissions.some((g) => g.kind === 'group' && userList().groups.some((x) => x.name === g.name && x.members.includes(p.name))))
    .map((p) => `Uživatel ${p.name}: bez přístupu přebíjí oprávnění skupiny.`),
});
const shareList = (): ShareList => ({
  shares: mockShares.map(shareInfo), volumes: mockVolumes, smb: { installed: false, included: false },
  principals: { users: mockUsers.map((u) => u.name), groups: userList().groups.map((g) => g.name) },
});
const SHARE_NAME = /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/;

// Services app (W2.4): catalog order like services.Catalog in the backend.
const allActs: ServiceAction[] = ['start', 'stop', 'restart', 'reload', 'enable', 'disable'];
const mockUnits: (ServiceInfo & { allowed: ServiceAction[]; canReload: boolean })[] = [
  ['smbd.service', 'Samba (SMB)', false, allActs, false, null],
  ['nfs-server.service', 'NFS', false, allActs, false, null],
  ['ssh.service', 'SSH', true, ['restart', 'reload'] as ServiceAction[], true, { pid: 220, mem: 6.2e6, ports: [['tcp', '0.0.0.0', 22], ['tcp', '::', 22]] }],
  ['smartmontools.service', 'SMART dohled', false, allActs, true, { pid: 190, mem: 2.1e6, ports: [] }],
  ['cron.service', 'Plánovač (cron)', false, allActs, false, { pid: 181, mem: 1.4e6, ports: [] }],
  ['systemd-timesyncd.service', 'Čas (NTP)', false, allActs, false, { pid: 160, mem: 3.0e6, ports: [] }],
  ['systemd-resolved.service', 'DNS', false, ['restart'] as ServiceAction[], false,
    { pid: 3265, mem: 7.8e6, ports: [['udp', '127.0.0.53', 53], ['tcp', '127.0.0.53', 53], ['udp', '127.0.0.54', 53], ['tcp', '127.0.0.54', 53]] }],
  ['systemd-networkd.service', 'Síť', true, [] as ServiceAction[], false, { pid: 150, mem: 5.5e6, ports: [] }],
  ['nsa310-fan.service', 'Ventilátor', true, ['restart'] as ServiceAction[], false, { pid: 0, mem: 0, ports: [] }],
  ['nsa310-leds.service', 'LED', false, allActs, false, { pid: 0, mem: 0, ports: [] }],
  ['nsa310-button.service', 'Tlačítko COPY', false, allActs, false, { pid: 230, mem: 1.9e6, ports: [] }],
  ['nas-revival.service', 'Webová správa', true, [] as ServiceAction[], false, { pid: 13634, mem: 24e6, ports: [['tcp', '0.0.0.0', 8443]] }],
  ['nas-revival-helper.service', 'Pomocná služba správy', true, [] as ServiceAction[], false, { pid: 13600, mem: 9e6, ports: [] }],
].map(([name, label, prot, allowed, canReload, run]) => {
  const r = run as { pid: number; mem: number; ports: [string, string, number][] } | null;
  const oneshot = name === 'nsa310-fan.service' || name === 'nsa310-leds.service';
  return {
    name: name as string, label: label as string, protected: prot as boolean, allowed: allowed as ServiceAction[], canReload: canReload as boolean,
    description: r ? `${label} (demo)` : '', state: !r ? 'not-installed' : name === 'nsa310-leds.service' ? 'done' : 'active',
    subState: r ? (oneshot ? 'exited' : 'running') : '', enabled: r ? 'enabled' : '', since: r ? new Date(Date.now() - 86400e3).toISOString() : null,
    pid: r?.pid || undefined, memoryBytes: r && r.pid ? r.mem : null, cpuPercent: r && r.pid ? 0.3 : null,
    ports: (r?.ports ?? []).map(([proto, address, port]) => ({ proto: proto as 'tcp' | 'udp', address, port })), actions: [],
  };
});
function unitActions(u: (typeof mockUnits)[number]): ServiceAction[] {
  if (u.state === 'not-installed') return [];
  const running = u.state === 'active';
  return u.allowed.filter((a) => a === 'restart' || (a === 'start' && !running) || (a === 'stop' && running)
    || (a === 'reload' && running && u.canReload) || (a === 'enable' && u.enabled === 'disabled') || (a === 'disable' && u.enabled === 'enabled'));
}
const unitView = (u: (typeof mockUnits)[number]): ServiceInfo => {
  const { allowed: _a, canReload: _c, ...v } = u;
  return { ...v, cpuPercent: v.cpuPercent != null ? Math.round(Math.random() * 20) / 10 : null, actions: unitActions(u) };
};

// SMB (W2.5): demo data uses documentation addresses only.
const mockSmb: SmbService = {
  installed: true, running: true, netbiosRunning: false, managed: true,
  settings: { enabled: true, workgroup: 'WORKGROUP', description: 'NAS Revival', minProtocol: 'SMB2_10', netbios: false },
};
const mockSmbStatus = (): SmbStatus => (!mockSmb.running ? { sessions: [], openFiles: [], truncated: false } : {
  sessions: [
    { id: '1', user: 'jana', machine: '192.0.2.21', dialect: 'SMB3_11', encryption: 'AES-128-GCM', signing: 'AES-128-GMAC', since: ago(5400), shares: ['dokumenty', 'fotky'] },
    { id: '2', user: 'petr', machine: '192.0.2.35', dialect: 'SMB3_00', encryption: '', signing: 'AES-128-CMAC', since: ago(600), shares: ['dokumenty'] },
  ],
  openFiles: [
    { share: 'dokumenty', path: 'faktury/2026-11.pdf', user: 'jana', access: 'R', since: ago(300) },
    { share: 'fotky', path: 'dovolena/IMG_0042.jpg', user: 'jana', access: 'RW', since: ago(60) },
  ],
  truncated: false,
});

// NFS (W2.6): exports reference the mock shares by name; documentation addresses only.
const mockNfs: NfsService = {
  installed: true, running: true, managed: true, missing: [], shares: [],
  settings: { enabled: true, exports: [{ share: 'Fotky', squash: 'root', clients: [{ host: '192.0.2.40', readOnly: false }] }] },
};
interface MockTask { id: string; name: string; type: string; enabled: boolean; schedule: Record<string, unknown>; smart?: { diskId: string; test: string }; backup?: Record<string, unknown> }
interface MockRun { start: string; end: string; result: string; message: string; bytes?: number; usedBytes?: number }
let mockTasks: MockTask[] = [
  { id: 'recycle', name: 'Vysypání košů', type: 'recycle-cleanup', enabled: true, schedule: { kind: 'daily', time: '03:30' } },
  { id: 'a1b2c3d4', name: 'SMART dlouhý test', type: 'smart', enabled: true, schedule: { kind: 'monthly', time: '02:00', day: 1 },
    smart: { diskId: 'wwn-0x5000c50000000001', test: 'long' } },
  { id: 'export', name: 'Záloha ze serveru', type: 'backup-pull', enabled: true, schedule: { kind: 'daily', time: '06:45' },
    backup: { host: '192.0.2.10', port: 22, user: 'root', paths: ['nas-export', 'hrcm'], keepDays: 35,
      fresh: { file: 'nas-export/last-ok', maxAgeHours: 26 }, bwlimitKiB: 0, excludes: ['*.tmp'] } },
  { id: 'night', name: 'Noční vypnutí', type: 'shutdown', enabled: false, schedule: { kind: 'weekly', time: '01:00', weekdays: ['sun'] } },
];
const mockRuns: Record<string, MockRun[]> = {
  recycle: [{ start: new Date(Date.now() - 20 * 3600e3).toISOString(), end: new Date(Date.now() - 20 * 3600e3 + 42e3).toISOString(), result: 'ok', message: 'Odstraněno 12 souborů (84 MB)' }],
  a1b2c3d4: [{ start: new Date(Date.now() - 9 * 86400e3).toISOString(), end: new Date(Date.now() - 9 * 86400e3 + 5e3).toISOString(), result: 'skipped', message: 'Na disku už běží test SMART.' }],
};
mockRuns.export = [{ start: new Date(Date.now() - 5 * 3600e3).toISOString(), end: new Date(Date.now() - 5 * 3600e3 + 95e3).toISOString(), result: 'ok',
  message: 'Staženo 12,3 MB, verzí 35.', bytes: 12_900_000, usedBytes: 4_800_000_000 }];
const mockRunning = new Set<string>();
const taskView = () => mockTasks.map((t) => ({
  ...t, running: mockRunning.has(t.id), lastRun: mockRuns[t.id]?.[0] ?? null,
  nextRun: t.enabled ? new Date(Date.now() + 3 * 3600e3).toISOString() : null,
}));

const MOCK_FP = 'SHA256:q3Zk0m7H1nP2yB8cV5xT9aLwR4eDfG6uJhS1oXiNtEw';
let mockShareOn = false;
function backupRoute(path: string, method: string, body: Record<string, unknown>, search = ''): Response | null {
  if (path === '/backup/share' && method === 'GET') return json(200, { enabled: mockShareOn, name: 'zalohy', path: '/srv/data/versions' });
  if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
  if (path === '/backup/share' && method === 'PUT') {
    mockShareOn = body.enabled === true;
    return json(200, { enabled: mockShareOn, name: 'zalohy', path: '/srv/data/versions' });
  }
  if (path === '/backup/key' && method === 'GET') {
    const pub = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMockMockMockMockMockMockMockMockMockMockMock nas-backup@nas';
    return json(200, { publicKey: pub, fingerprint: 'SHA256:Zx8Wq1LrT5uYb2NcVd9HeA3oKpMf7GsJi4RtXyU0aBc',
      authorizedKeysHint: `from="192.0.2.5",restrict,command="rrsync -ro /var/backups" ${pub}` });
  }
  if (path === '/backup/hostkey/scan' && method === 'POST') {
    const host = String(body.host ?? '');
    if (!host || host.startsWith('-')) return bad('host', 'Neplatný server.');
    if (host === '192.0.2.99') return err(502, 'scan_failed', 'Server neodpovídá.');
    return json(200, { keys: [
      { type: 'ssh-ed25519', key: 'AAAAC3NzaC1lZDI1NTE5AAAAIMockHostKeyMockHostKeyMockHostKeyMockHost', fingerprint: MOCK_FP },
      { type: 'ssh-rsa', key: 'AAAAB3NzaC1yc2EAAAADAQABAAABAQMockRsaKey', fingerprint: 'SHA256:Lm2Pq9Rt4Vx7Yb1Cd6Fg8Hj3Kn5Ws0ZaEiOuTyUrQeA' },
    ], known: false, changed: host === '192.0.2.11' });
  }
  const vm = /^\/backup\/([^/]+)\/versions(?:\/([^/]+)\/(browse|file))?$/.exec(path);
  if (vm && method === 'GET') {
    if (!vm[2]) {
      return json(200, { complete: true, versions: [
        { date: '2026-11-25', files: 1240, bytes: 4_800_000_000, addedBytes: 12_900_000 },
        { date: '2026-11-24', files: 1236, bytes: 4_790_000_000, addedBytes: 31_000_000 },
        { date: '2026-11-23', files: 1230, bytes: 4_760_000_000, addedBytes: 2_400_000_000 },
      ] });
    }
    if (vm[3] === 'file') return new Response(new Blob(['mock']), { status: 200 });
    const q = new URLSearchParams(search);
    const dir = q.get('path') ?? '';
    const now = new Date().toISOString();
    const tree: Record<string, { name: string; type: string; size: number; modified: string }[]> = {
      '': [{ name: 'nas-export', type: 'dir', size: 0, modified: now }, { name: 'readme.txt', type: 'file', size: 1234, modified: now },
        { name: 'latest', type: 'link', size: 0, modified: now }],
      'nas-export': [{ name: 'db', type: 'dir', size: 0, modified: now }, { name: 'last-ok', type: 'file', size: 20, modified: now }],
      'nas-export/db': [{ name: 'dump.sql.gz', type: 'file', size: 8_400_000, modified: now }],
    };
    const entries = tree[dir];
    if (!entries) return err(404, 'not_found', 'Složka ve verzi neexistuje.');
    return json(200, { path: dir, entries, offset: 0, total: entries.length, truncated: false });
  }
  if (path === '/backup/hostkey/trust' && method === 'POST') return json(200, {});
  if (path === '/backup/test' && method === 'POST') {
    const host = String(body.host ?? '');
    if (host === '192.0.2.99') return json(200, { ok: false, message: 'Připojení vypršelo.' });
    return json(200, { ok: true, entries: ['last-ok', 'db', 'files', 'photos'], message: '', fullAccess: host === '192.0.2.11' });
  }
  return null;
}

function tasksRoute(path: string, method: string, body: unknown): Response | null {
  if (path === '/tasks' && method === 'GET') return json(200, { tasks: taskView() });
  if (path === '/tasks' && method === 'PUT') {
    const list = (body as { tasks?: MockTask[] }).tasks ?? [];
    if (list.length > 32) return bad('tasks', 'Úloh může být nejvýše 32.');
    for (const t of list) if (!t.name?.trim()) return bad('name', 'Zadejte název úlohy.');
    mockTasks = list.map((t) => ({ ...t, id: t.id || Math.random().toString(16).slice(2, 10) }));
    for (const id of Object.keys(mockRuns)) if (!mockTasks.some((t) => t.id === id)) delete mockRuns[id];
    return json(200, { tasks: taskView() });
  }
  const m = /^\/tasks\/([^/]+)\/(run|stop|history)$/.exec(path);
  if (!m) return null;
  const id = decodeURIComponent(m[1] ?? '');
  if (!mockTasks.some((t) => t.id === id)) return err(404, 'not_found', 'Úloha neexistuje.');
  if (m[2] === 'history' && method === 'GET') return json(200, { runs: mockRuns[id] ?? [] });
  if (m[2] === 'run' && method === 'POST') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    if (mockRunning.has(id)) return json(202, {});
    mockRunning.add(id);
    const began = Date.now();
    setTimeout(() => {
      if (!mockRunning.delete(id)) return;
      (mockRuns[id] ??= []).unshift({ start: new Date(began).toISOString(), end: new Date().toISOString(), result: 'ok', message: 'Dokončeno.' });
    }, 5000);
    return json(202, {});
  }
  if (m[2] === 'stop' && method === 'POST') {
    mockRunning.delete(id);
    (mockRuns[id] ??= []).unshift({ start: new Date().toISOString(), end: new Date().toISOString(), result: 'stopped', message: 'Zastaveno uživatelem.' });
    return json(202, {});
  }
  return null;
}

const nfsShares = () => mockShares.map((s) => ({ name: s.name, path: `${s.volume}/${s.folder}`, readOnly: s.readOnly,
  groupWrite: s.permissions.some((p) => p.kind === 'group' && p.name === 'nas-nfs' && p.access === 'rw') }));
const mockNfsStatus = (): NfsStatus => ({
  clients: mockNfs.running && mockNfs.settings.enabled ? [{ address: '192.0.2.40', name: 'Linux NFSv4.2 pve', version: '4.2', confirmed: true }] : [],
});
const nfsState = (): NfsService => {
  const names = new Set(mockShares.map((s) => s.name));
  return { ...mockNfs, shares: nfsShares(), missing: mockNfs.settings.exports.map((e) => e.share).filter((n) => !names.has(n)) };
};

function nfsRoute(path: string, method: string, body: unknown): Response | null {
  if (path === '/services/nfs' && method === 'GET') return json(200, nfsState());
  if (path === '/services/nfs/status' && method === 'GET') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    return json(200, mockNfsStatus());
  }
  if (path === '/services/nfs' && method === 'PUT') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    const b = body as NfsSettings;
    const names = new Set(mockShares.map((s) => s.name));
    for (const e of b.exports ?? []) {
      if (!names.has(e.share)) return bad('exports', `Sdílená složka ${e.share} neexistuje.`);
      if (!e.clients?.length) return bad('exports', `Export ${e.share} nemá žádného klienta.`);
      if (e.clients.some((c) => c.host.includes('*') || c.host.endsWith('/0'))) return bad('exports', 'Klient musí být IP adresa nebo síť.');
    }
    mockNfs.settings = { enabled: !!b.enabled, exports: b.exports ?? [] };
    mockNfs.running = mockNfs.settings.enabled;
    mockNfs.managed = true;
    return json(200, nfsState());
  }
  return null;
}

const mockSsh: SshService = {
  settings: { port: 22, passwordAuth: true, sftpGroups: [] }, managed: false, running: true,
  effective: { ports: [22], passwordAuth: true, rootLogin: 'without-password' }, groups: ['nas-users', 'rodina'], chrootOk: true,
};

const mockKeys: SshKeys = {
  users: [
    { name: 'jana', sftp: false, keys: [{ type: 'ssh-ed25519', fingerprint: 'SHA256:q3xY0bW8m1Jt5fGk2LpZr7vC9nE4sD6aH0uK1oT2iQw', comment: 'jana@notebook' }] },
    { name: 'petr', sftp: false, keys: [] },
  ],
};

function sshKeysRoute(path: string, method: string, body: unknown): Response | null {
  if (path === '/services/ssh/keys' && method === 'GET') {
    for (const u of mockKeys.users) u.sftp = mockSsh.settings.sftpGroups.length > 0;
    return json(200, mockKeys);
  }
  const m = /^\/services\/ssh\/keys\/([a-z][a-z0-9_-]*)$/.exec(path);
  if (!m || (method !== 'POST' && method !== 'DELETE')) return null;
  if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
  const u = mockKeys.users.find((x) => x.name === m[1]);
  if (!u) return err(404, 'not_found', 'Uživatel NASu neexistuje.');
  if (method === 'DELETE') {
    const fp = (body as { fingerprint?: string }).fingerprint;
    if (!u.keys.some((k) => k.fingerprint === fp)) return err(404, 'not_found', 'Klíč neexistuje.');
    u.keys = u.keys.filter((k) => k.fingerprint !== fp);
    return json(200, mockKeys);
  }
  const parts = String((body as { key?: string }).key ?? '').trim().split(/\s+/);
  if (parts.length < 2 || !/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp256)$/.test(parts[0] ?? '')) return bad('key', 'Nejde o platný veřejný klíč SSH.');
  const fingerprint = 'SHA256:' + btoa((parts[1] ?? '').slice(-32)).replace(/=+$/, '');
  if (u.keys.some((k) => k.fingerprint === fingerprint)) return bad('key', 'Tento klíč už uživatel má.');
  if (u.keys.length >= 10) return bad('key', 'Uživatel může mít nejvýše 10 klíčů.');
  u.keys.push({ type: parts[0] ?? '', fingerprint, comment: parts.slice(2).join(' '), ...(parts[0] === 'ssh-rsa' ? { bits: 3072 } : {}) });
  return json(200, mockKeys);
}

function sshRoute(path: string, method: string, body: unknown): Response | null {
  if (path.startsWith('/services/ssh/keys')) return sshKeysRoute(path, method, body);
  if (path !== '/services/ssh') return null;
  if (method === 'GET') return json(200, mockSsh);
  if (method !== 'PUT') return null;
  if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
  const b = body as SshSettings;
  const port = Number(b.port);
  if (!Number.isInteger(port) || (port !== 22 && (port < 1024 || port > 65535)) || port === 2049 || port === 8443) {
    return bad('port', 'Port musí být 22 nebo 1024–65535.');
  }
  const groups = [...new Set(b.sftpGroups ?? [])].sort();
  if (groups.some((g) => !mockSsh.groups.includes(g))) return bad('sftpGroups', 'Skupině nelze povolit SFTP.');
  mockSsh.settings = { port, passwordAuth: !!b.passwordAuth, sftpGroups: groups };
  mockSsh.effective = { ports: [port], passwordAuth: !!b.passwordAuth, rootLogin: 'without-password' };
  mockSsh.managed = true;
  return json(200, mockSsh);
}

function smbRoute(path: string, method: string, body: unknown): Response | null {
  if (path === '/services/smb' && method === 'GET') return json(200, mockSmb);
  if (path === '/services/smb/status' && method === 'GET') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    return json(200, mockSmbStatus());
  }
  if (path === '/services/smb' && method === 'PUT') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    const b = body as SmbSettings;
    const wg = String(b.workgroup ?? '').trim().toUpperCase();
    if (!/^[A-Z0-9][A-Z0-9_-]{0,14}$/.test(wg)) return bad('workgroup', 'Pracovní skupina má 1–15 znaků: písmena bez diakritiky, číslice, - a _.');
    if (!['SMB2_10', 'SMB3_00', 'SMB3_11'].includes(b.minProtocol)) return bad('minProtocol', 'Neznámá nejnižší verze protokolu.');
    mockSmb.settings = { ...b, workgroup: wg, description: String(b.description ?? '').trim() };
    mockSmb.running = b.enabled;
    mockSmb.netbiosRunning = b.enabled && b.netbios;
    mockSmb.managed = true;
    return json(200, mockSmb);
  }
  return null;
}

function servicesRoute(path: string, method: string, url: URL): Response | null {
  if (path === '/services' && method === 'GET') return json(200, { services: mockUnits.map(unitView) });
  const m = /^\/services\/([^/]+)\/([a-z]+)$/.exec(path);
  if (!m) return null;
  const u = mockUnits.find((x) => x.name === decodeURIComponent(m[1] ?? ''));
  if (!u) return err(404, 'not_found', 'Tato služba se nespravuje.');
  if (m[2] === 'log' && method === 'GET') {
    if (sessionRole() !== 'admin') return err(403, 'forbidden', 'K této části nemáte přístup.');
    const n = Number(url.searchParams.get('lines') ?? 100);
    if (!(n >= 1 && n <= 200)) return bad('lines', 'Počet řádků musí být 1 až 200.');
    const now = Date.now();
    return json(200, { name: u.name, entries: Array.from({ length: Math.min(n, 12) }, (_, i) => ({
      time: new Date(now - (12 - i) * 60e3).toISOString(), priority: i === 7 ? 3 : i === 4 ? 4 : 6,
      ident: u.name.replace('.service', ''), pid: u.pid, message: i === 7 ? 'error: demo failure' : `demo message ${i + 1}`,
    })) });
  }
  const a = m[2] as ServiceAction;
  if (method !== 'POST' || !u.allowed.includes(a)) return bad('action', 'Tuto akci služba nepodporuje.');
  if (u.state === 'not-installed') return err(404, 'not_found', 'Služba není nainstalovaná.');
  if (a === 'stop') { u.state = 'inactive'; u.subState = 'dead'; u.pid = undefined; u.memoryBytes = null; u.ports = []; }
  if (a === 'start' || a === 'restart') { u.state = 'active'; u.subState = 'running'; u.pid = u.pid ?? 4242; u.since = new Date().toISOString(); }
  if (a === 'enable') u.enabled = 'enabled';
  if (a === 'disable') u.enabled = 'disabled';
  return json(200, unitView(u));
}

function sharesRoute(path: string, method: string, body: Record<string, unknown>): Response | null {
  if (path === '/shares' && method === 'GET') return json(200, shareList());
  if (path === '/shares/recycle-cleanup' && method === 'POST') return json(200, { removedFiles: 12, removedBytes: 48 * 1024 * 1024 });
  if (path === '/shares' && method === 'POST') {
    const s = body as unknown as Share;
    if (!SHARE_NAME.test(s.name ?? '')) return bad('name', 'Název nesplňuje pravidla.');
    if (['global', 'homes', 'printers', 'print$', 'ipc$', 'netlogon', 'profiles'].includes(s.name.toLowerCase())) return bad('name', 'Tento název je vyhrazený.');
    if (mockShares.some((x) => x.name.toLowerCase() === s.name.toLowerCase())) return bad('name', 'Složka s tímto názvem už existuje.');
    if (!mockVolumes.some((v) => v.path === s.volume)) return bad('volume', 'Svazek není připojený.');
    mockShares = [...mockShares, { ...s, folder: s.name, description: (s.description ?? '').trim() }].sort((a, b) => a.name.localeCompare(b.name));
    return json(201, shareList());
  }
  const m = /^\/shares\/([^/]+)$/.exec(path);
  if (!m) return null;
  const cur = mockShares.find((x) => x.name === m[1]);
  if (!cur) return err(404, 'not_found', 'Sdílená složka neexistuje.');
  if (method === 'PUT') {
    const s = body as unknown as Share;
    if (s.permissions?.some((p) => p.kind === 'group' && p.access === 'none')) return bad('permissions', 'Skupině nelze přístup zakázat.');
    mockShares = mockShares.map((x) => (x.name === cur.name ? { ...cur, description: (s.description ?? '').trim(), readOnly: !!s.readOnly,
      hidden: !!s.hidden, recycle: s.recycle, permissions: s.permissions } : x));
    return json(200, shareList());
  }
  if (method === 'DELETE') {
    if (body.deleteData && body.confirmName !== cur.name) return bad('confirmName', 'Pro smazání dat napište přesný název složky.');
    mockShares = mockShares.filter((x) => x.name !== cur.name);
    return json(200, { ...shareList(), dataRemoval: body.deleteData ? 'started' : 'none' });
  }
  return null;
}

/** Resource Monitor: a deterministic hour of 10 s samples ending now, shaped like the NSA310 (no PSI). */
function monitorSample(t: number): MonitorSample {
  const k = t / 10_000, w = (p: number, a = 1) => (Math.sin(k / p) + 1) / 2 * a;
  const backup = k % 360 > 300 && k % 360 < 330; // a burst of disk and network traffic
  const MiB = 1048576;
  return {
    t, psi: null,
    cpu: { user: Math.round((8 + w(7, 20) + (backup ? 35 : 0)) * 10) / 10, system: Math.round((4 + w(11, 6)) * 10) / 10,
      iowait: backup ? 12 : Math.round(w(5, 2) * 10) / 10, irq: 1.2, steal: 0 },
    memory: { usedBytes: 92 * MiB + w(13, 20 * MiB), cacheBytes: 110 * MiB + w(40, 10 * MiB), availableBytes: 140 * MiB,
      swapUsedBytes: 9 * MiB, swapInBps: 0, swapOutBps: 0 },
    disks: [{ name: 'sda', readBps: backup ? 600_000 : w(9, 20_000), writeBps: backup ? 18 * MiB : 7_000 + w(3, 9_000),
      readIops: backup ? 12 : 0.4, writeIops: backup ? 60 : 1.3, busyPercent: backup ? 71 : 1.8, awaitMs: backup ? 9.4 : 3.1 }],
    net: [{ name: 'enp1s0', rxBps: backup ? 19 * MiB : 2_000 + w(6, 30_000), txBps: 1_500 + w(8, 12_000), errors: 0, drops: k % 97 === 0 ? 1 : 0 }],
  };
}
function monitorHistory(since: number): MonitorHistory {
  const now = Math.floor(Date.now() / 10_000) * 10_000;
  const samples: MonitorSample[] = [];
  for (let t = now - 359 * 10_000; t <= now; t += 10_000) if (t > since) samples.push(monitorSample(t));
  return { intervalSeconds: 10, psiAvailable: false, memTotalBytes: 252043264, swapTotalBytes: 1073737728,
    netTotals: [{ name: 'enp1s0', rxBytes: 48e9, txBytes: 3.1e9, rxErrors: 0, txErrors: 0, drops: 14 }], samples };
}

/** Process list (W3.2): a few typical NSA310 processes; stopping one removes it. */
const MIB = 1048576;
let mockProcs: ProcessInfo[] = [
  { pid: 1, ppid: 0, name: 'systemd', cmd: '/sbin/init', user: 'root', state: 'S', threads: 1, rssBytes: 9 * MIB, cpuPercent: 0.1, start: 1, startedAt: 0, protected: 'init' },
  { pid: 2, ppid: 0, name: 'kthreadd', cmd: '[kthreadd]', user: 'root', state: 'S', threads: 1, rssBytes: 0, cpuPercent: 0, start: 1, startedAt: 0, protected: 'kernel' },
  { pid: 31, ppid: 2, name: 'kworker/0:1-events', cmd: '[kworker/0:1-events]', user: 'root', state: 'I', threads: 1, rssBytes: 0, cpuPercent: 0.2, start: 90, startedAt: 0, protected: 'kernel' },
  { pid: 212, ppid: 1, name: 'systemd-journal', cmd: '/usr/lib/systemd/systemd-journald', user: 'root', state: 'S', threads: 1, rssBytes: 7 * MIB, cpuPercent: 0.3, start: 400, startedAt: 0, protected: 'system' },
  { pid: 402, ppid: 1, name: 'sshd', cmd: 'sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups', user: 'root', state: 'S', threads: 1, rssBytes: 6 * MIB, cpuPercent: 0, start: 900, startedAt: 0, protected: 'system' },
  { pid: 455, ppid: 1, name: 'nas-revival', cmd: '/usr/local/bin/nas-revival -listen :8443 -config /etc/nas-revival -state /var/lib/nas-revival', user: 'nas-revival', state: 'S', threads: 6, rssBytes: 21 * MIB, cpuPercent: 1.6, start: 950, startedAt: 0, protected: 'revival' },
  { pid: 611, ppid: 1, name: 'smbd', cmd: '/usr/sbin/smbd --foreground --no-process-group', user: 'root', state: 'S', threads: 1, rssBytes: 18 * MIB, cpuPercent: 0.4, start: 1500, startedAt: 0 },
  { pid: 2840, ppid: 2801, name: 'rsync', cmd: 'rsync -a --delete --link-dest=/srv/backup/2026-11-25 root@192.0.2.10:nas-export/ /srv/backup/2026-11-26/', user: 'nas-backup', state: 'D', threads: 1, rssBytes: 34 * MIB, cpuPercent: 23.4, start: 88000, startedAt: 0 },
  { pid: 3011, ppid: 3005, name: 'sshd-session', cmd: 'sshd-session: admin@pts/0', user: 'admin', state: 'S', threads: 1, rssBytes: 5 * MIB, cpuPercent: 0, start: 90000, startedAt: 0 },
];
function processList() {
  const boot = Math.floor(Date.now() / 1000) - 3 * 86400;
  return { processes: mockProcs.map((p) => ({ ...p, startedAt: boot + Math.floor(p.start / 100),
    cpuPercent: Math.max(0, Math.round((p.cpuPercent + (p.pid === 2840 ? (Math.random() - 0.5) * 8 : 0)) * 10) / 10) })),
  memTotalBytes: 252043264, sampledAt: new Date().toISOString() };
}


/* ---------- File Station (W4.1) ---------- */
type MockEntry = { name: string; type: string; size: number; modified: string };
const fsNow = new Date().toISOString();
const fsTree: Record<string, Record<string, MockEntry[]>> = {
  Dokumenty: {
    '': [{ name: 'Faktury', type: 'dir', size: 0, modified: fsNow }, { name: 'Smlouvy', type: 'dir', size: 0, modified: fsNow },
      { name: 'poznámky k NASu s dlouhým názvem souboru.txt', type: 'file', size: 2_345, modified: fsNow },
      { name: 'zástupce', type: 'link', size: 0, modified: fsNow }],
    Faktury: [{ name: '2026', type: 'dir', size: 0, modified: fsNow }, { name: 'faktura-001.pdf', type: 'file', size: 184_000, modified: fsNow }],
    'Faktury/2026': [],
    Smlouvy: [{ name: 'najem.pdf', type: 'file', size: 1_200_000, modified: fsNow }],
  },
  Fotky: { '': [{ name: 'dovolena.jpg', type: 'file', size: 4_800_000, modified: fsNow }] },
};
const fsShares = [
  { name: 'Dokumenty', description: 'Rodinné dokumenty', access: 'rw', recycle: true },
  { name: 'Fotky', description: '', access: 'ro', recycle: false },
];
const fsJoin = (d: string, n: string) => (d ? `${d}/${n}` : n);
const fsParent = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
const fsBase = (p: string) => p.slice(p.lastIndexOf('/') + 1);

function fsRemove(tree: Record<string, MockEntry[]>, p: string): MockEntry | null {
  const dir = tree[fsParent(p)];
  const i = dir?.findIndex((e) => e.name === fsBase(p)) ?? -1;
  if (!dir || i < 0) return null;
  const [e] = dir.splice(i, 1);
  for (const k of Object.keys(tree)) if (k === p || k.startsWith(`${p}/`)) delete tree[k];
  return e ?? null;
}

function filesRoute(path: string, method: string, search: string, body: Record<string, unknown>): Response | null {
  if (sessionUser === 'rescue') return json(200, { shares: [], noAccount: true });
  if (path === '/files' && method === 'GET') return json(200, { shares: fsShares, noAccount: false });
  const m = /^\/files\/([^/]+)\/(list|download|mkdir|rename|move|delete)$/.exec(path);
  if (!m) return null;
  const name = decodeURIComponent(m[1] ?? '');
  const share = fsShares.find((s) => s.name === name);
  const tree = fsTree[name];
  if (!share || !tree) return err(404, 'not_found', 'Sdílená složka neexistuje nebo k ní nemáte přístup.');
  const q = new URLSearchParams(search);
  if (m[2] === 'list') {
    const entries = tree[q.get('path') ?? ''];
    if (!entries) return err(404, 'not_found', 'Složka neexistuje.');
    const sorted = [...entries].sort((a, b) => (a.type === 'dir') !== (b.type === 'dir') ? (a.type === 'dir' ? -1 : 1) : a.name.localeCompare(b.name));
    return json(200, { share: name, access: share.access, path: q.get('path') ?? '', entries: sorted, offset: 0, total: sorted.length, truncated: false });
  }
  if (m[2] === 'download') return new Response(new Blob(['mock']), { status: 200 });
  if (share.access !== 'rw') return err(403, 'forbidden', 'Do této sdílené složky máte jen čtení.');
  const dir = String(body.path ?? '');
  if (m[2] === 'mkdir') {
    const n = String(body.name ?? '');
    const entries = tree[dir];
    if (!entries) return err(404, 'not_found', 'Složka neexistuje.');
    if (entries.some((e) => e.name === n)) return err(409, 'file_exists', 'Položka s tímto názvem už existuje.');
    entries.push({ name: n, type: 'dir', size: 0, modified: new Date().toISOString() });
    tree[fsJoin(dir, n)] = [];
    return new Response(null, { status: 204 });
  }
  if (m[2] === 'rename') {
    const n = String(body.name ?? '');
    const siblings = tree[fsParent(dir)] ?? [];
    if (siblings.some((e) => e.name === n)) return err(409, 'file_exists', 'Položka s tímto názvem už existuje.');
    const e = siblings.find((x) => x.name === fsBase(dir));
    if (!e) return err(404, 'not_found', 'Položka neexistuje.');
    e.name = n;
    return new Response(null, { status: 204 });
  }
  const paths = (body.paths as string[] | undefined) ?? [];
  const failed: { path: string; message: string }[] = [];
  let done = 0;
  for (const p of paths) {
    if (m[2] === 'move') {
      const dest = String(body.dest ?? '');
      const target = tree[dest];
      if (!target || dest === p || dest.startsWith(`${p}/`)) { failed.push({ path: p, message: 'Složku nelze přesunout do ní samotné.' }); continue; }
      if (target.some((e) => e.name === fsBase(p))) { failed.push({ path: p, message: 'Položka s tímto názvem už existuje.' }); continue; }
      const e = fsRemove(tree, p);
      if (!e) { failed.push({ path: p, message: 'Položka neexistuje.' }); continue; }
      target.push(e);
      if (e.type === 'dir') tree[fsJoin(dest, e.name)] = [];
    } else if (!fsRemove(tree, p)) { failed.push({ path: p, message: 'Položka neexistuje.' }); continue; }
    done++;
  }
  return json(200, { done, failed, recycled: m[2] === 'delete' && share.recycle });
}

/** Simulated upload: progress over ~3 s, a name that exists gives 409 unless overwrite=1. */
function mockUpload(url: string, file: Blob, onProgress: (n: number) => void, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const u = new URL(url, location.href);
    const share = decodeURIComponent(u.pathname.split('/')[4] ?? '');
    const dir = u.searchParams.get('path') ?? '', name = u.searchParams.get('name') ?? '';
    const entries = fsTree[share]?.[dir];
    if (!entries) { reject(new ApiError(404, 'not_found', 'Složka neexistuje.')); return; }
    const existing = entries.find((e) => e.name === name);
    if (existing && u.searchParams.get('overwrite') !== '1') { reject(new ApiError(409, 'file_exists', 'Soubor s tímto názvem už existuje.')); return; }
    let sent = 0;
    const step = Math.max(1, Math.ceil(file.size / 15));
    const iv = setInterval(() => {
      sent = Math.min(file.size, sent + step);
      onProgress(sent);
      if (sent >= file.size) {
        clearInterval(iv);
        if (existing) existing.size = file.size; else entries.push({ name, type: 'file', size: file.size, modified: new Date().toISOString() });
        resolve();
      }
    }, 200);
    signal.addEventListener('abort', () => { clearInterval(iv); reject(new DOMException('aborted', 'AbortError')); }, { once: true });
  });
}

export function installMock(): void {
  setUploadImpl(mockUpload);
  const real = window.fetch.bind(window);
  window.fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.href);
    if (!url.pathname.startsWith('/api/v1')) return real(input, init);
    await new Promise((r) => setTimeout(r, 250));
    const path = url.pathname.slice('/api/v1'.length);
    const method = (init?.method ?? 'GET').toUpperCase();
    if (downUntil && Date.now() >= downUntil.from) {
      if (Date.now() < downUntil.to) throw new TypeError('Failed to fetch');
      downUntil = null; loggedIn = false;
    }
    const h = new Headers(init?.headers);
    if (method !== 'GET' && h.get('X-Requested-With') !== 'nas-revival') return err(403, 'csrf', 'Missing header');

    if (path === '/session') {
      if (method === 'POST') {
        const b = JSON.parse(String(init?.body ?? '{}')) as { username?: string; password?: string };
        if (fails >= 5) return err(429, 'rate_limited', 'Too many attempts', { 'Retry-After': '240' });
        if (b.password !== 'demo') { fails++; return err(401, 'invalid_credentials', 'Invalid credentials'); }
        fails = 0; loggedIn = true; sessionUser = b.username ?? 'admin';
        return new Response(null, { status: 204 });
      }
      if (method === 'DELETE') { loggedIn = false; return new Response(null, { status: 204 }); }
      return loggedIn
        ? json(200, { username: sessionUser, role: sessionRole(), expiresAt: new Date(Date.now() + 12 * 3600e3).toISOString() })
        : err(401, 'unauthorized', 'Login required');
    }
    if (!loggedIn) return err(401, 'unauthorized', 'Login required');
    if (path === '/session/password' && method === 'PUT') {
      const b = JSON.parse(String(init?.body ?? '{}')) as { current?: string; password?: string };
      if (b.current !== 'demo') return bad('current', 'Současné heslo nesouhlasí.');
      if (!b.password) return bad('password', 'Heslo nesmí být prázdné.');
      loggedIn = false;
      return json(200, { samba: false });
    }
    // File Station: every role, the share access decides (W4.1).
    if (path.startsWith('/files')) {
      let body: Record<string, unknown> = {};
      try { body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = filesRoute(path, method, url.search, body);
      if (r) return r;
    }
    // Same policy as the backend: viewers read, standard users only their session and files.
    if (sessionRole() === 'user' || (sessionRole() === 'viewer' && method !== 'GET')) {
      return err(403, 'forbidden', sessionRole() === 'viewer' ? 'Účet jen pro čtení nemůže nic měnit.' : 'K této části nemáte přístup.');
    }
    if (path === '/overview') return json(200, overview());
    if (path === '/monitor') return json(200, monitorHistory(Number(url.searchParams.get('since') ?? 0)));
    if (path === '/monitor/processes') return json(200, processList());
    if (path === '/hardware') return json(200, hwState());
    if (path === '/hardware/history') return json(200, hwHistory(Number(url.searchParams.get('since') ?? 0)));
    if (path.startsWith('/hardware/') && method === 'PUT') {
      const b = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>;
      if (path === '/hardware/fan') hwSettings = { ...hwSettings, fanProfile: b.profile as FanProfileId };
      if (path === '/hardware/thresholds') hwSettings = { ...hwSettings, thresholds: b as unknown as HardwareThresholds };
      if (path === '/hardware/leds') hwSettings = { ...hwSettings, leds: b as unknown as LedSettings };
      return json(200, hwSettings);
    }
    const pk = /^\/monitor\/processes\/(\d+)\/kill$/.exec(path);
    if (pk && method === 'POST') {
      const b = JSON.parse(String(init?.body ?? '{}')) as { start?: number; force?: boolean };
      const p = mockProcs.find((x) => x.pid === Number(pk[1]));
      if (!p) return err(404, 'not_found', 'Proces už neběží.');
      if (p.start !== b.start) return err(409, 'process_changed', 'Proces s tímto číslem mezitím skončil a běží jiný. Obnovte seznam.');
      if (p.protected) return err(409, 'process_protected', 'Tento proces je chráněný, NAS ho potřebuje k chodu.');
      mockProcs = mockProcs.filter((x) => x !== p);
      return json(200, { pid: p.pid, name: p.name, signal: b.force ? 'KILL' : 'TERM' });
    }
    if (path === '/smart') return json(200, smart());
    if (path === '/storage') return json(200, storage());
    if (path === '/storage/activity') {
      const n = Number(url.searchParams.get('seconds') ?? 15);
      if (!(n >= 10 && n <= 45)) return err(400, 'invalid', 'Délka měření musí být 10 až 45 sekund.');
      await new Promise((r) => setTimeout(r, Math.min(n, 5) * 1000));
      return json(200, activity(n));
    }
    if (path.startsWith('/storage/disks/')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = smartRoute(path, method, body) ?? powerRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/users') || path.startsWith('/groups')) {
      let body: Record<string, unknown> = {};
      try { body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = usersRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/backup/')) {
      let body: Record<string, unknown> = {};
      try { body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = backupRoute(path, method, body, url.search);
      if (r) return r;
    }
    if (path.startsWith('/tasks')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = tasksRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/services/nfs')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = nfsRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/services/ssh')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = sshRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/services/smb')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = smbRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/services')) {
      const r = servicesRoute(path, method, url);
      if (r) return r;
    }
    if (path.startsWith('/shares')) {
      let body: Record<string, unknown> = {};
      try { body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = sharesRoute(path, method, body);
      if (r) return r;
    }
    if (path.startsWith('/config/')) {
      const r = configRoute(path, method);
      if (r) return r;
    }
    if (path.startsWith('/settings/') || path.startsWith('/system/') || path === '/network' || path.startsWith('/network/')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = settings(path, method, body);
      if (r) return r;
    }
    return err(404, 'not_found', 'Not found');
  };
}
