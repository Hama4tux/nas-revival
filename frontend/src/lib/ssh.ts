import type { SshService, SshSettings } from '../api/types';

export type SshHealth = 'ok' | 'notRunning' | 'unmanaged';

/** Overall state: sshd running and the drop-in rendered by the web UI. */
export function sshHealth(s: SshService): SshHealth {
  if (!s.running) return 'notRunning';
  return s.managed ? 'ok' : 'unmanaged';
}

/** Same rule as sshd.Validate: 22 or 1024–65535, not NFS or the web UI. */
export function portOk(p: number): boolean {
  if (!Number.isInteger(p)) return false;
  if (p !== 22 && (p < 1024 || p > 65535)) return false;
  return p !== 2049 && p !== 8443;
}

export const sameSettings = (a: SshSettings, b: SshSettings) =>
  a.port === b.port && a.passwordAuth === b.passwordAuth &&
  [...a.sftpGroups].sort().join(',') === [...b.sftpGroups].sort().join(',');

/** Warnings for the save button: what the change does to people connecting. */
export function sshWarnings(cur: SshService, next: SshSettings): ('port' | 'noPassword' | 'sftpOff')[] {
  const w: ('port' | 'noPassword' | 'sftpOff')[] = [];
  if (next.port !== cur.settings.port) w.push('port');
  if (next.sftpGroups.length > 0 && !next.passwordAuth) w.push('noPassword');
  if (cur.settings.sftpGroups.some((g) => !next.sftpGroups.includes(g))) w.push('sftpOff');
  return w;
}

/** sftp command line for the port in use (no -P for 22). */
export const sftpCommand = (host: string, port: number, user = 'uzivatel') =>
  port === 22 ? `sftp ${user}@${host}` : `sftp -P ${port} ${user}@${host}`;

export type KeyPasteProblem = 'empty' | 'private' | 'putty' | 'multi' | 'format';

/** Quick check of a pasted public key before it is sent; the server checks it fully (sshd.ParseKey). */
export function keyPasteProblem(text: string): KeyPasteProblem | null {
  const t = text.trim();
  if (!t) return 'empty';
  if (t.includes('PRIVATE KEY')) return 'private';
  if (t.includes('BEGIN SSH2 PUBLIC KEY')) return 'putty';
  if (/[\r\n]/.test(t)) return 'multi';
  if (!/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) AAAA[0-9A-Za-z+/=]+( .*)?$/.test(t)) return 'format';
  return null;
}

/** Short type label for the key list: "ed25519", "RSA 4096", "ECDSA", "ed25519 (FIDO)". */
export function keyTypeLabel(type: string, bits?: number): string {
  if (type === 'ssh-ed25519') return 'ed25519';
  if (type === 'ssh-rsa') return bits ? `RSA ${bits}` : 'RSA';
  if (type.startsWith('ecdsa-')) return 'ECDSA';
  if (type.startsWith('sk-ssh-ed25519')) return 'ed25519 (FIDO)';
  if (type.startsWith('sk-ecdsa')) return 'ECDSA (FIDO)';
  return type;
}
