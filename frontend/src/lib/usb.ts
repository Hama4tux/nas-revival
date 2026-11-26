import type { UsbDisk, UsbPartition } from '../api/types';
import { cs } from '../i18n/cs';

export type UsbState = 'mounted' | 'blocked' | 'ready';

export const usbState = (p: UsbPartition): UsbState =>
  p.mountPoint !== '' && p.ours ? 'mounted' : p.blocked !== '' ? 'blocked' : 'ready';

/** Title of a USB disk: vendor + model, else the kernel name. */
export const usbTitle = (d: UsbDisk) => [d.vendor, d.model].filter((s) => s.trim() !== '').join(' ') || d.name;

/** Short line describing a partition's filesystem. */
export function usbFsLine(p: UsbPartition): string {
  const fs = p.fsType === 'vfat' ? 'FAT' : p.fsType === '' ? cs.usb.noFs : p.fsType;
  return p.label ? `${fs} · ${p.label}` : fs;
}

/** A disk can be ejected unless another job (COPY button, manual mount) holds one of its partitions. */
export const canEject = (d: UsbDisk) => !d.partitions.some((p) => p.mountPoint !== '' && !p.ours);
