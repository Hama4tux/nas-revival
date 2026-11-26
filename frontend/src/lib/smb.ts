import type { SmbService, SmbSettings } from '../api/types';

export type SmbHealth = 'notInstalled' | 'off' | 'ok' | 'notRunning' | 'runningWhileOff' | 'unmanaged';

/** Overall state of the SMB service: settings against what systemd reports. */
export function smbHealth(s: SmbService): SmbHealth {
  if (!s.installed) return 'notInstalled';
  if (s.settings.enabled && !s.managed) return 'unmanaged';
  if (s.settings.enabled && !s.running) return 'notRunning';
  if (!s.settings.enabled && s.running) return 'runningWhileOff';
  return s.settings.enabled ? 'ok' : 'off';
}

export const workgroupOk = (w: string) => /^[A-Z0-9][A-Z0-9_-]{0,14}$/.test(w.trim().toUpperCase());

/** Same rules as smb.Validate: no control characters, % (substitution) or \ (line continuation). */
export function descriptionOk(d: string): boolean {
  const v = d.trim();
  // eslint-disable-next-line no-control-regex
  return [...v].length <= 64 && !/[\u0000-\u001f\u007f-\u009f%\\]/.test(v);
}

/** Saving disconnects clients: SMB is switched off, or NetBIOS changes the ports (smbd restarts). */
export function disconnects(cur: SmbService, next: SmbSettings): boolean {
  if (!cur.running) return false;
  return !next.enabled || cur.settings.netbios !== next.netbios;
}

export const sameSettings = (a: SmbSettings, b: SmbSettings) =>
  a.enabled === b.enabled && a.workgroup.trim().toUpperCase() === b.workgroup.trim().toUpperCase() &&
  a.description.trim() === b.description.trim() && a.minProtocol === b.minProtocol && a.netbios === b.netbios;

/** UNC path for Windows, smb:// URL for macOS and Linux. */
export const shareAddress = (host: string, share: string) => ({ unc: `\\\\${host}\\${share}`, url: `smb://${host}/${share}` });
