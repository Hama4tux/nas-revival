export interface SessionInfo { username: string; expiresAt: string }
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
  health: SystemHealth;
}

export type HealthStatus = 'ok' | 'warn' | 'crit' | 'unknown';
export type ProblemCode =
  | 'sensorsFault' | 'sensorsUnknown' | 'fanStopped' | 'smartFailed' | 'smartSectors'
  | 'fsCritical' | 'fsWarn' | 'serviceFailed' | 'collectFailed';
export interface HealthProblem { severity: 'warn' | 'crit'; code: ProblemCode; subject?: string }
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

export type SystemPowerAction = 'reboot' | 'shutdown';
export type PowerBlockerCode = 'smart_test' | 'copy_running' | 'package_manager' | 'network_pending' | 'check_failed';
export interface PowerBlocker { code: PowerBlockerCode; diskId?: string }
export type PowerEffect = 'web_offline' | 'connections_drop' | 'sessions_end' | 'back_in_minutes' | 'power_button';
export interface SystemPowerPreview {
  action: SystemPowerAction; hostname: string; confirmToken: string; expiresAt: string;
  effects: PowerEffect[]; blockers: PowerBlocker[];
}
export interface SystemPowerResult { action: SystemPowerAction; forced: boolean; blockers: PowerBlocker[] }

/* ---------- config history (GET /config/history, /config/history/{n}, POST /config/rollback/{n}) ---------- */

export type ConfigFileId = 'network' | 'time' | 'disk-power';
export type ConfigEntryKind = 'baseline' | 'change' | 'manual' | 'rollback';
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
