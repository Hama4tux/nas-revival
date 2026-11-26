import type { NfsExport, NfsService, NfsSettings } from '../api/types';

export type NfsHealth = 'notInstalled' | 'off' | 'ok' | 'notRunning' | 'runningWhileOff' | 'unmanaged';

/** Overall state of the NFS service: settings against what systemd reports. */
export function nfsHealth(s: NfsService): NfsHealth {
  if (!s.installed) return 'notInstalled';
  if (s.settings.enabled && !s.managed) return 'unmanaged';
  if (s.settings.enabled && !s.running) return 'notRunning';
  if (!s.settings.enabled && s.running) return 'runningWhileOff';
  return s.settings.enabled ? 'ok' : 'off';
}

const ipv4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

/**
 * Quick check of a client entry before saving: an IPv4/IPv6 address or a
 * network not wider than /16 (IPv6 /48). Host names and * are refused; the
 * server (nfs.ParseHost) has the final word.
 */
export function hostOk(h: string): boolean {
  const v = h.trim();
  const [addr = '', bits, extra] = v.split('/');
  if (extra !== undefined) return false;
  const v4 = ipv4.test(addr);
  const v6 = !v4 && addr.includes(':') && /^[0-9a-fA-F:.]+$/.test(addr);
  if (!v4 && !v6) return false;
  if (v4 && (addr === '0.0.0.0' || Number(addr.split('.')[0]) >= 224)) return false;
  if (bits === undefined) return true;
  if (!/^\d{1,3}$/.test(bits)) return false;
  const n = Number(bits);
  return v4 ? n >= 16 && n <= 32 : n >= 48 && n <= 128;
}

export const exportOk = (e: NfsExport) => e.share !== '' && e.clients.length > 0 && e.clients.every((c) => hostOk(c.host));

const norm = (s: NfsSettings) => JSON.stringify({
  enabled: s.enabled,
  exports: s.exports.map((e) => ({ share: e.share, squash: e.squash, clients: e.clients.map((c) => ({ host: c.host.trim(), readOnly: c.readOnly })) })),
});
export const sameSettings = (a: NfsSettings, b: NfsSettings) => norm(a) === norm(b);

/** Saving stops the server or drops exports: mounted clients lose access. */
export function disconnects(cur: NfsService, next: NfsSettings): boolean {
  if (!cur.running) return false;
  if (!next.enabled) return true;
  return cur.settings.exports.some((e) => {
    const n = next.exports.find((x) => x.share === e.share);
    return !n || e.clients.some((c) => !n.clients.some((d) => d.host.trim() === c.host));
  });
}

/** Proxmox storage command for an export (BKP-12): NFSv4.2, backups only. */
export const proxmoxCommand = (host: string, path: string, id: string) =>
  `pvesm add nfs ${id} --server ${host} --export ${path} --content backup --options vers=4.2`;
