/** Web role (W2.2, ADR 0004): nas-admin, nas-viewer, otherwise standard user. */
export type Role = 'admin' | 'viewer' | 'user';
export interface SessionInfo { username: string; role: Role; expiresAt: string }
export type SensorStatus = 'ok' | 'unknown' | 'unsupported' | 'fault';
export type ServiceState = 'active' | 'inactive' | 'failed' | 'done' | 'unknown';

export interface Overview {
  generatedAt: string;
  host: { hostname: string; os: string; kernel: string; uptimeSeconds: number | null; boardModel: string };
  cpu: {
    model: string; cores: number; load1: number | null; load5: number | null; load15: number | null;
    usagePercent: number | null;
  };
  memory: { totalBytes: number; availableBytes: number | null; swapTotalBytes: number | null; swapFreeBytes: number | null };
  sensors: {
    status: SensorStatus;
    temperatures: { id: string; label: string; milliCelsius: number | null }[];
    fans: { id: string; label: string; rpm: number | null }[];
  };
  network: {
    name: string; up: boolean; speedMbps: number | null; rxBytes: number | null; txBytes: number | null; addresses: string[];
  }[];
  storage: {
    disks: {
      name: string; model: string; sizeBytes: number; rotational: boolean; removable: boolean;
      /** null for removable disks, which are not asked. */
      powerState: PowerState | null;
    }[];
    filesystems: {
      mount: string; device: string; fsType: string; totalBytes: number; usedBytes: number; availableBytes: number;
    }[];
  };
  services: { name: string; label: string; state: ServiceState; since?: string | null }[];
  backups: BackupStatus[];
  health: SystemHealth;
}

/** Last result of one backup-pull task (BKP-11). lastResult is empty when it never ran. */
export interface BackupStatus {
  id: string; name: string; enabled: boolean;
  lastResult: 'ok' | 'failed' | 'skipped' | 'stopped' | '';
  lastEnd: string | null; message: string; usedBytes: number;
}

export type HealthStatus = 'ok' | 'warn' | 'crit' | 'unknown';
export type ProblemCode =
  | 'sensorsFault' | 'sensorsUnknown' | 'fanStopped' | 'smartFailed' | 'smartSectors'
  | 'fsCritical' | 'fsWarn' | 'serviceFailed' | 'collectFailed'
  | 'backupFailed' | 'backupStale' | 'tempWarn' | 'tempCrit' | 'diskTempWarn' | 'diskTempCrit'
  | 'diskLoadCycles' | 'diskStartStop';
export interface HealthProblem { severity: 'warn' | 'crit'; code: ProblemCode; subject?: string }
/** One entry of the notification centre (W4.5): a health problem that appeared. */
export interface NotificationItem {
  id: string; severity: 'warn' | 'crit'; code: ProblemCode; subject?: string;
  firstAt: string; updatedAt: string; resolvedAt: string | null; count: number; read: boolean;
}
export interface NotificationList { items: NotificationItem[]; unread: number; active: number }
/** Evaluated on the NAS by the shared collector (MON-01). */
export interface SystemHealth {
  status: HealthStatus; checkedAt: string; smartCheckedAt: string | null; problems: HealthProblem[];
}

export interface Smart {
  checkedAt: string;
  error?: string;
  disks: {
    name: string; model: string; health: 'passed' | 'failed' | 'unknown';
    temperatureCelsius: number | null; powerOnHours: number | null;
    reallocatedSectors: number | null; pendingSectors: number | null; standby: boolean;
  }[];
}

export interface NetConfig {
  mode: 'dhcp' | 'static';
  address: string | null; prefixLength: number | null; gateway: string | null;
  dns: string[]; useDhcpDns: boolean;
}
export interface NetworkSettings {
  interface: string;
  config: NetConfig;
  current: { addresses: string[]; gateway: string | null; dns: string[] };
  pending: { deadline: string; previous: NetConfig; next: NetConfig } | null;
}
export interface NetInterface {
  name: string; mac: string; mtu: number; state: string; carrier: boolean | null;
  speedMbps: number | null; duplex: '' | 'full' | 'half'; linkChanges: number | null; addresses: string[];
}
export interface NetRoute {
  family: 'inet' | 'inet6'; destination: string; gateway?: string; device?: string;
  protocol?: string; scope?: string; source?: string; metric: number | null;
}
export interface NetworkInfo {
  hostname: string; domains: string[]; interfaces: NetInterface[]; routes: NetRoute[]; checkedAt: string;
}
export type DiagTest = 'ping' | 'gateway' | 'dns' | 'internet' | 'ports' | 'connections';
export interface DiagResult {
  test: DiagTest; target?: string; ok: boolean; message?: string; durationMs: number;
  ping?: { address: string; sent: number; received: number; lossPercent: number; minMs: number | null; avgMs: number | null; maxMs: number | null };
  addresses?: string[];
  steps?: { name: 'dns' | 'connect'; ok: boolean; detail: string; durationMs: number }[];
  ports?: { proto: 'tcp' | 'udp'; address: string; port: number; process: string }[];
  connections?: { localPort: number; peer: string; peerPort: number; process: string }[];
  truncated?: boolean;
}
export interface TimeSettings {
  ntpServers: string[]; defaultServers: boolean; synchronized: boolean;
  serverName: string | null; serverAddress: string | null; timezone: string; now: string;
}
export interface TlsInfo {
  subject: string; issuer: string; selfSigned: boolean; dnsNames: string[]; ipAddresses: string[];
  notBefore: string; notAfter: string; sha256: string; keyType: string;
}

/** GET /system/info */
export interface SystemInfo {
  version: string;
  hostname: string;
  boardModel: string | null;
  os: string;
  debianVersion: string | null;
  kernel: string;
  architecture: string;
  cpuModel: string;
  cores: number;
  memTotalBytes: number;
  bootTime: string | null;
  uptimeSeconds: number;
  lastPackageChange: string | null;
}

export type CapabilityStatus = 'verified' | 'unverified' | 'unsupported';

/** GET /system/capabilities */
export interface SystemCapabilities {
  profileId: string;
  profileName: string;
  sataBays: number | null;
  usbPorts: number | null;
  capabilities: { id: string; status: CapabilityStatus }[];
}

/* ---------- storage (GET /storage) ---------- */

export type DiskRole = 'system' | 'boot' | 'swap' | 'data';

export interface StorageMount { point: string; readOnly: boolean; bind: boolean; root: string }

export interface StoragePartition {
  name: string; number: number; sizeBytes: number;
  partUuid: string | null; partLabel: string | null;
  fsType: string | null; fsUuid: string | null; label: string | null;
  swap: boolean; mounts: StorageMount[];
  usage: { totalBytes: number; usedBytes: number; availableBytes: number } | null;
}

export interface StorageDisk {
  id: string; idStable: boolean; name: string;
  model: string | null; vendor: string | null; serial: string | null; firmware: string | null;
  bus: 'ata' | 'usb' | 'nvme' | 'mmc' | 'unknown';
  type: 'hdd' | 'sshd' | 'ssd' | 'flash' | 'unknown';
  rotationRpm: number | null; sizeBytes: number; removable: boolean; smart: boolean; readOnly: boolean;
  partitionTable: string | null; system: boolean; roles: DiskRole[]; sharedWithData: boolean;
  partitions: StoragePartition[];
}

export interface StorageReport { disks: StorageDisk[] }

export type SmartLevel = 'ok' | 'info' | 'warn' | 'crit';

export interface SmartAttribute {
  id: number; name: string; value: number | null; worst: number | null; thresh: number | null;
  raw: string; rawValue: number; count: number; prefailure: boolean; whenFailed: '' | 'past' | 'now';
  key: boolean; level: SmartLevel;
}

export interface SmartSelfTestStatus {
  value: number; text: string; running: boolean; remainingPercent: number | null; passed: boolean | null;
}

export interface SmartSelfTestEntry {
  type: 'short' | 'long' | 'conveyance' | 'other'; typeText: string; status: string; passed: boolean;
  remainingPercent: number | null; lifetimeHours: number;
}

export interface SmartDetail {
  checkedAt: string; standby: boolean; health: 'passed' | 'failed' | 'unknown';
  model: string | null; family: string | null; temperatureCelsius: number | null; powerOnHours: number | null;
  powerCycles: number | null; selfTestsSupported: boolean; shortMinutes: number | null; longMinutes: number | null;
  selfTest: SmartSelfTestStatus | null; attributes: SmartAttribute[]; history: SmartSelfTestEntry[];
  error?: 'smartctl_unavailable' | 'smartctl_failed';
}

export type SmartTestType = 'short' | 'long';
export interface SmartTestStarted { diskId: string; type: SmartTestType; estimatedMinutes: number | null }

export type PowerProfile = 'performance' | 'balanced' | 'eco' | 'custom';
export type PowerState = 'active' | 'standby' | 'sleeping' | 'unknown';
export interface PowerSettings { profile: PowerProfile; standbyMinutes: number; apm: number | null }
export interface DiskPower {
  diskId: string; systemDisk: boolean; state: PowerState; checkedAt: string;
  apmSupported: boolean | null; apmLevel: number | null;
  settings: PowerSettings; stored: boolean; profiles: PowerSettings[]; standbyChoices: number[];
}
export interface DiskStandbyResult { diskId: string; wasStandby: boolean }
/** GET /storage/disks/{id}/power/history (PWR-04); times are Unix ms. */
export interface PowerEvent { t: number; state: PowerState }
export interface PowerCounter { t: number; loadCycles: number | null; startStop: number | null }
export interface PowerGrowth {
  loadCyclesPerDay: number | null; startStopPerDay: number | null; spanHours: number;
  loadCycleLimit: number; startStopLimit: number; loadCyclesWarn: boolean; startStopWarn: boolean;
  latestLoadCycles: number | null; latestStartStop: number | null; latestCounterAt: number | null;
}
export interface DiskPowerHistory {
  events: PowerEvent[]; counters: PowerCounter[]; wakes24h: number; wakes7d: number;
  standbyPercent24h: number | null; trackedSince: number | null; sampleSeconds: number; growth: PowerGrowth;
}

export type SystemPowerAction = 'reboot' | 'shutdown';
export type PowerBlockerCode = 'smart_test' | 'copy_running' | 'backup_running' | 'task_running' | 'package_manager' | 'network_pending' | 'check_failed';
export interface PowerBlocker { code: PowerBlockerCode; diskId?: string }
export type PowerEffect = 'web_offline' | 'connections_drop' | 'sessions_end' | 'back_in_minutes' | 'power_button';
export interface SystemPowerPreview {
  action: SystemPowerAction; hostname: string; confirmToken: string; expiresAt: string;
  effects: PowerEffect[]; blockers: PowerBlocker[];
}
export interface SystemPowerResult { action: SystemPowerAction; forced: boolean; blockers: PowerBlocker[] }

/* ---------- config history (GET /config/history, /config/history/{n}, POST /config/rollback/{n}) ---------- */

export type ConfigFileId = 'network' | 'time' | 'disk-power' | 'shares' | 'smb' | 'nfs' | 'ssh';
export type ConfigEntryKind = 'baseline' | 'change' | 'manual' | 'rollback' | 'import';
export interface ConfigFileState { id: ConfigFileId; path: string; present: boolean; sha256?: string; size: number }
export interface ConfigEntry {
  id: number; time: string; user: string; kind: ConfigEntryKind; module?: ConfigFileId; rollbackOf?: number;
  files: ConfigFileState[]; changed: ConfigFileId[];
}
export interface ConfigDrift { id: ConfigFileId; path: string; state: 'ok' | 'modified' | 'pending' }
export interface ConfigHistory { entries: ConfigEntry[]; drift: ConfigDrift[]; maxEntries: number; maxBytes: number; usedBytes: number }
export interface ConfigDiffLine { op: ' ' | '-' | '+'; text: string }
export interface ConfigFileDiff {
  id: ConfigFileId; path: string; same: boolean; present: boolean; currentPresent: boolean;
  lines: ConfigDiffLine[] | null; tooLarge: boolean;
}
export interface ConfigDetail { entry: ConfigEntry; files: ConfigFileDiff[]; drift: ConfigFileId[] }
export interface ConfigRollbackResult {
  rollbackOf: number; restored: ConfigFileId[];
  networkPending: { deadline: string } | null;
}
/** POST /config/import/preview and /config/import (W4.4, CFG-05). The export file itself stays opaque. */
export interface ConfigImportPreview {
  created: string; hostname: string; otherHost: boolean; files: ConfigFileDiff[]; drift: ConfigFileId[];
}
export interface ConfigImportResult { restored: ConfigFileId[]; networkPending: { deadline: string } | null }

/** GET /storage/activity (W1.9): one on-demand measurement. */
export interface ActivityCounters { readOps: number; readBytes: number; writeOps: number; writeBytes: number }
export interface ActivityDevice extends ActivityCounters {
  name: string; parent: string; mount: string; fsType: string; swap: boolean; journalCommits: number | null;
}
export interface ActivityHint { code: string; value?: string }
export interface ActivityDisk extends ActivityCounters {
  id: string; name: string; system: boolean; partitions: ActivityDevice[]; hints: ActivityHint[];
}
export interface ActivityProcess {
  pid: number; comm: string; unit: string; readBytes: number | null; writeBytes: number | null; files: string[];
}
export interface ActivityFile { path: string; device: string; sizeDelta: number | null; processes: string[] }
export interface DiskActivity {
  startedAt: string; seconds: number; processIo: boolean; devices: ActivityDevice[];
  swapInPages: number; swapOutPages: number; processes: ActivityProcess[]; files: ActivityFile[];
  filesTruncated: boolean; notScanned: string[]; disks: ActivityDisk[];
}

/** GET /users (W2.1): NAS accounts (Linux users in nas-users) and editable groups. */
export type SambaPassword = 'set' | 'missing' | 'unavailable';
export interface UserInfo {
  name: string; uid: number; fullName: string; groups: string[]; role: Role; disabled: boolean;
  password: { linux: boolean; samba: SambaPassword; web: boolean; changedOutside: boolean; changed?: string };
}
export interface GroupInfo { name: string; gid: number; members: string[]; builtin: boolean }
export interface UserList { users: UserInfo[]; groups: GroupInfo[]; samba: boolean }
export interface PasswordResult { samba: boolean }

/** GET /shares (W2.3): shared folders, data volumes and who may use them. */
export type ShareAccess = 'rw' | 'ro' | 'none';
export type PrincipalKind = 'user' | 'group';
export interface SharePermission { kind: PrincipalKind; name: string; access: ShareAccess }
export interface Share {
  name: string; description: string; volume: string; folder: string; readOnly: boolean; hidden: boolean;
  recycle: { enabled: boolean; days: number }; permissions: SharePermission[];
  /** Destination of backups: read-only for everybody, only the backup runner writes. */
  backupTarget?: boolean;
}
export interface UserAccess { user: string; access: ShareAccess; via: string }
export interface ShareInfo extends Share { path: string; exists: boolean; enforcement: 'acl' | 'smb'; effective: UserAccess[]; conflicts: string[] }
export interface ShareVolume { path: string; fsType: string; totalBytes: number; availBytes: number; acl: boolean }
export interface ShareList {
  shares: ShareInfo[]; volumes: ShareVolume[]; smb: { installed: boolean; included: boolean };
  principals: { users: string[]; groups: string[] };
}
export interface ShareDeleteResult extends ShareList { dataRemoval: 'none' | 'started' }
export interface RecycleCleanupResult { removedFiles: number; removedBytes: number }

/** Services app (GET /services, SVC-10..12). */
export type UnitState = 'active' | 'inactive' | 'failed' | 'activating' | 'deactivating' | 'done' | 'not-installed' | 'unknown';
export type ServiceAction = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable';
export interface ServicePort { proto: 'tcp' | 'udp'; address: string; port: number }
export interface ServiceInfo {
  name: string; label: string; description: string; state: UnitState; subState: string;
  /** systemd UnitFileState: enabled, disabled, static, masked, … ('' when not installed). */
  enabled: string; since: string | null; pid?: number;
  memoryBytes: number | null; cpuPercent: number | null; ports: ServicePort[];
  result?: string; exitStatus?: number; protected: boolean; actions: ServiceAction[];
}
export interface ServiceList { services: ServiceInfo[] }
export interface ServiceLogEntry { time: string; priority: number; ident: string; pid?: number; message: string }
export interface ServiceLog { name: string; entries: ServiceLogEntry[] }

// SMB service (W2.5, SMB-01/02).
export type SmbProtocol = 'SMB2_10' | 'SMB3_00' | 'SMB3_11';
export interface SmbSettings {
  enabled: boolean; workgroup: string; description: string; minProtocol: SmbProtocol; netbios: boolean;
}
export interface SmbService {
  installed: boolean; settings: SmbSettings; running: boolean; netbiosRunning: boolean; managed: boolean;
}
export interface SmbSession {
  id: string; user: string; machine: string; dialect: string; encryption: string; signing: string; since: string; shares: string[];
}
export interface SmbOpenFile { share: string; path: string; user: string; access: string; since: string }
export interface SmbStatus { sessions: SmbSession[]; openFiles: SmbOpenFile[]; truncated: boolean }

// NFS service (W2.6, NFS-01, BKP-12).
export type NfsSquash = 'root' | 'all';
export interface NfsClient { host: string; readOnly: boolean }
export interface NfsExport { share: string; clients: NfsClient[]; squash: NfsSquash }
export interface NfsSettings { enabled: boolean; exports: NfsExport[] }
export interface NfsShare { name: string; path: string; readOnly: boolean; groupWrite: boolean }
export interface NfsService {
  installed: boolean; settings: NfsSettings; running: boolean; managed: boolean; missing: string[]; shares: NfsShare[];
}
export interface NfsConnected { address: string; name: string; version: string; confirmed: boolean }
export interface NfsStatus { clients: NfsConnected[] }

// SSH/SFTP (W2.7, SSH-01).
export interface SshSettings { port: number; passwordAuth: boolean; sftpGroups: string[] }
export interface SshEffective { ports: number[]; passwordAuth: boolean; rootLogin: string }
export interface SshService {
  settings: SshSettings; managed: boolean; running: boolean; effective: SshEffective; groups: string[]; chrootOk: boolean;
}

// SSH keys of NAS users (W2.7b, SSH-02).
export interface SshKey { type: string; bits?: number; fingerprint: string; comment: string }
export interface SshKeyUser { name: string; keys: SshKey[]; sftp: boolean; invalid?: number }
export interface SshKeys { users: SshKeyUser[] }

export type TaskType = 'smart' | 'recycle-cleanup' | 'backup-pull' | 'reboot' | 'shutdown';
export type TaskWeekday = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun';
export interface TaskSchedule {
  kind: 'daily' | 'weekly' | 'monthly';
  time: string;
  weekdays?: TaskWeekday[];
  day?: number;
}
export interface BackupParams {
  host: string; port: number; user: string; paths: string[]; keepDays: number;
  fresh?: { file: string; maxAgeHours: number };
  bwlimitKiB: number; excludes: string[];
}
export interface TaskDef {
  id: string; name: string; type: TaskType; enabled: boolean; schedule: TaskSchedule;
  smart?: { diskId: string; test: 'short' | 'long' };
  backup?: BackupParams;
}
export type TaskResult = 'ok' | 'failed' | 'skipped' | 'stopped';
export interface TaskRun { start: string; end: string; result: TaskResult; message: string; bytes?: number; usedBytes?: number }
export interface BackupKey { publicKey: string; fingerprint: string; authorizedKeysHint: string }
export interface BackupHostKey { type: string; key: string; fingerprint: string }
export interface BackupScan { keys: BackupHostKey[]; known?: boolean; changed?: boolean }
export interface BackupTestResult { ok: boolean; entries?: string[]; message?: string; fullAccess?: boolean }
export interface Task extends TaskDef { running: boolean; nextRun: string | null; lastRun: TaskRun | null }
export interface TaskList { tasks: Task[] }
export interface TaskHistory { runs: TaskRun[] }

export interface BackupVersion { date: string; files: number; bytes: number; addedBytes: number }
export interface BackupVersionList { versions: BackupVersion[]; complete: boolean }
export interface BackupEntry { name: string; type: 'dir' | 'file' | 'link' | 'other'; size: number; modified: string; badName?: boolean }
export interface BackupBrowse { path: string; entries: BackupEntry[]; offset: number; total: number; truncated: boolean }
/** GET /backup/restore (W4.3): the current or last restore of a version into a share. */
export type RestoreState = 'idle' | 'scanning' | 'copying' | 'done' | 'failed' | 'cancelled';
export interface RestoreStatus {
  state: RestoreState; job?: string; date?: string; path: string; share?: string; dest?: string; user?: string;
  totalFiles: number; totalBytes: number; files: number; bytes: number; skipped: number; message?: string;
  started?: string; finished?: string;
}

/** GET /monitor (W3.1): samples every intervalSeconds, the last hour, oldest first. */
export interface MonitorCpu { user: number; system: number; iowait: number; irq: number; steal: number }
export interface MonitorMemory {
  usedBytes: number; cacheBytes: number; availableBytes: number; swapUsedBytes: number; swapInBps: number; swapOutBps: number;
}
export interface MonitorPsi { cpuSome: number; memorySome: number; memoryFull: number; ioSome: number; ioFull: number }
export interface MonitorDisk {
  name: string; readBps: number; writeBps: number; readIops: number; writeIops: number; busyPercent: number; awaitMs: number;
}
export interface MonitorNet { name: string; rxBps: number; txBps: number; errors: number; drops: number }
export interface MonitorSample {
  t: number; cpu: MonitorCpu; memory: MonitorMemory; psi: MonitorPsi | null; disks: MonitorDisk[]; net: MonitorNet[];
}
export interface MonitorNetTotal { name: string; rxBytes: number; txBytes: number; rxErrors: number; txErrors: number; drops: number }
export interface MonitorHistory {
  intervalSeconds: number; psiAvailable: boolean; memTotalBytes: number; swapTotalBytes: number;
  netTotals: MonitorNetTotal[]; samples: MonitorSample[];
}

/** GET /monitor/processes (W3.2). `start` = start time in clock ticks, sent back with a kill. */
export type ProcessProtection = 'init' | 'kernel' | 'system' | 'revival';
export interface ProcessInfo {
  pid: number; ppid: number; name: string; cmd: string; user: string; state: string; threads: number;
  rssBytes: number; cpuPercent: number; start: number; startedAt: number; protected?: ProcessProtection;
}
export interface ProcessList { processes: ProcessInfo[]; memTotalBytes: number; sampledAt: string }
export interface ProcessKillResult { pid: number; name: string; signal: 'TERM' | 'KILL' }

/** GET /hardware (W3.4). Temperatures in °C; `c` null while a disk sleeps. */
export type FanProfileId = 'quiet' | 'balanced' | 'performance';
export interface HardwareThresholds { boardWarnC: number; boardCritC: number; diskWarnC: number; diskCritC: number }
export interface NightMode { enabled: boolean; start: string; end: string }
export interface LedSettings { night: NightMode; faultBlink: boolean }
export interface HardwareSettings { fanProfile: FanProfileId; thresholds: HardwareThresholds; leds: LedSettings }
export interface FanProfile { id: FanProfileId; zoneMinC: number; zoneMaxC: number; pwmMin: number }
export interface FanState {
  mode: 'auto' | 'manual' | 'full' | 'unknown'; pwm: number | null; pwmPercent: number | null;
  zoneMinC: number | null; zoneMaxC: number | null; pwmMin: number | null; profile: FanProfileId | '';
}
export interface HardwareSensor { id: string; label: string; kind: 'board' | 'disk'; c: number | null; standby?: boolean }
export interface HardwareLed { name: string; trigger: string; on: boolean; red: boolean }
export interface HardwareState {
  settings: HardwareSettings; profiles: FanProfile[]; zoneCritC: number; fan: FanState; fanRpm: number | null;
  sensors: HardwareSensor[]; leds: HardwareLed[];
}
/** GET /hardware/history: minute points, the highest temperature of each minute; the last point is still running. */
export interface HardwarePoint { t: number; temps: Record<string, number>; fanRpm: number | null; pwmPercent: number | null }
export interface HardwareHistory { intervalSeconds: number; points: HardwarePoint[] }

/** File Station (W4.1): GET /files, /files/{share}/list, move/delete results. */
export interface FileShare { name: string; description: string; access: 'rw' | 'ro'; recycle: boolean }
export interface FileShareList { shares: FileShare[]; noAccount: boolean }
export interface FileList extends BackupBrowse { share: string; access: 'rw' | 'ro' }
export interface FileItemError { path: string; message: string }
export interface FileBatch { done: number; failed: FileItemError[]; recycled: boolean }

/** GET /storage/usb (W4.2). blocked = why the partition cannot be mounted ("" = it can). */
export interface UsbPartition {
  name: string; sizeBytes: number; fsType: string; label: string; mountPoint: string; ours: boolean; readOnly: boolean;
  usedBytes: number; totalBytes: number; blocked: string;
}
export interface UsbDisk { id: string; name: string; model: string; vendor: string; sizeBytes: number; partitions: UsbPartition[] }
export interface UsbList { disks: UsbDisk[] }
