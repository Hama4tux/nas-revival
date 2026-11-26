// Development-only fake API. Imported dynamically from main.tsx behind import.meta.env.VITE_MOCK,
// so it never reaches the production bundle.
import type { Role, DiskPower, NetConfig, PowerSettings, NetworkSettings, Overview, Smart, SmartAttribute, SmartDetail, StorageReport, SystemPowerAction, PowerBlocker, TimeSettings, TlsInfo, ConfigEntry, ConfigDiffLine, UserInfo, UserList } from '../api/types';

const start = Date.now();
let loggedIn = false;
let fails = 0;
/** Log in as "viewer" or "user" (any other name = admin) to try the roles. */
let sessionUser = 'admin';
const sessionRole = (): Role => (sessionUser === 'viewer' ? 'viewer' : sessionUser === 'user' ? 'user' : 'admin');

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
    health: {
      status: 'crit', checkedAt: new Date().toISOString(), smartCheckedAt: ago(-5),
      problems: [
        { severity: 'crit', code: 'serviceFailed', subject: 'Tlačítko COPY' },
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

function powerRoute(path: string, method: string, body: unknown): Response | null {
  const m = /^\/storage\/disks\/([^/]+)\/(power|standby)$/.exec(path);
  if (!m) return null;
  const id = decodeURIComponent(m[1] ?? '');
  if (id !== 'wwn-0x5000c50000000001') return err(404, 'power_unsupported', 'Disk nepodporuje správu napájení.');
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
    if ([...pw].length < 12) return bad('password', 'Heslo musí mít alespoň 12 znaků.');
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
    if ([...pw].length < 12) return bad('password', 'Heslo musí mít alespoň 12 znaků.');
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

export function installMock(): void {
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
      if ([...(b.password ?? '')].length < 12) return bad('password', 'Heslo musí mít alespoň 12 znaků.');
      loggedIn = false;
      return json(200, { samba: false });
    }
    // Same policy as the backend: viewers read, standard users only their session.
    if (sessionRole() === 'user' || (sessionRole() === 'viewer' && method !== 'GET')) {
      return err(403, 'forbidden', sessionRole() === 'viewer' ? 'Účet jen pro čtení nemůže nic měnit.' : 'K této části nemáte přístup.');
    }
    if (path === '/overview') return json(200, overview());
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
    if (path.startsWith('/config/')) {
      const r = configRoute(path, method);
      if (r) return r;
    }
    if (path.startsWith('/settings/') || path.startsWith('/system/')) {
      let body: unknown = {};
      try { body = init?.body ? JSON.parse(String(init.body)) : {}; } catch { return err(400, 'invalid', 'Neplatný JSON'); }
      const r = settings(path, method, body);
      if (r) return r;
    }
    return err(404, 'not_found', 'Not found');
  };
}
