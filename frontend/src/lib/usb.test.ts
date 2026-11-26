import { describe, expect, it } from 'vitest';
import type { UsbDisk, UsbPartition } from '../api/types';
import { canEject, usbFsLine, usbState, usbTitle } from './usb';

const part = (p: Partial<UsbPartition>): UsbPartition => ({
  name: 'sdb1', sizeBytes: 1, fsType: 'vfat', label: '', mountPoint: '', ours: false, readOnly: false, usedBytes: 0, totalBytes: 0, blocked: '', ...p,
});
const disk = (ps: UsbPartition[]): UsbDisk => ({ id: 'usb-x', name: 'sdb', model: '', vendor: '', sizeBytes: 1, partitions: ps });

describe('usb helpers', () => {
  it('derives the state', () => {
    expect(usbState(part({}))).toBe('ready');
    expect(usbState(part({ mountPoint: '/srv/usb/x', ours: true }))).toBe('mounted');
    expect(usbState(part({ mountPoint: '/run/nsa310-copy/sdb1', blocked: 'Připojeno jinde' }))).toBe('blocked');
    expect(usbState(part({ blocked: 'exFAT' }))).toBe('blocked');
  });
  it('names disks and filesystems', () => {
    expect(usbTitle(disk([]))).toBe('sdb');
    expect(usbTitle({ ...disk([]), vendor: 'SanDisk', model: 'Cruzer Fit' })).toBe('SanDisk Cruzer Fit');
    expect(usbFsLine(part({ label: 'FOTKY' }))).toBe('FAT · FOTKY');
    expect(usbFsLine(part({ fsType: 'ext4' }))).toBe('ext4');
  });
  it('refuses eject while another job holds a partition', () => {
    expect(canEject(disk([part({ mountPoint: '/srv/usb/x', ours: true })]))).toBe(true);
    expect(canEject(disk([part({ mountPoint: '/run/nsa310-copy/sdb1' })]))).toBe(false);
  });
});
