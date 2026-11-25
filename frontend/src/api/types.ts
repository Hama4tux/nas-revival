export interface SessionInfo { username: string; expiresAt: string }
export type SensorStatus = 'ok' | 'unknown' | 'unsupported' | 'fault';
export type ServiceState = 'active' | 'inactive' | 'failed' | 'unknown';

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
    disks: { name: string; model: string; sizeBytes: number; rotational: boolean; removable: boolean }[];
    filesystems: {
      mount: string; device: string; fsType: string; totalBytes: number; usedBytes: number; availableBytes: number;
    }[];
  };
  services: { name: string; label: string; state: ServiceState }[];
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
