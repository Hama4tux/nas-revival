import { describe, expect, it } from 'vitest';
import type { StorageDisk } from '../api/types';
import { diskTitle, diskWarnings, unpartitionedBytes } from './storage';

const GiB = 1024 ** 3;
const disk = (p: Partial<StorageDisk> = {}): StorageDisk => ({
  id: 'wwn-0x1', idStable: true, name: 'sda', model: 'ST1000LM014-SSHD', vendor: null, serial: 'X', firmware: 'LVD4',
  bus: 'ata', type: 'sshd', rotationRpm: 5400, sizeBytes: 100 * GiB, removable: false, smart: true, readOnly: false,
  partitionTable: 'gpt', system: true, roles: ['system'], sharedWithData: false, partitions: [], ...p,
});
const part = (name: string, sizeBytes: number) => ({
  name, number: 1, sizeBytes, partUuid: null, partLabel: null, fsType: 'ext4', fsUuid: null, label: null,
  swap: false, mounts: [], usage: null,
});

describe('diskWarnings', () => {
  it('is empty for a healthy dedicated disk', () => expect(diskWarnings(disk())).toEqual([]));
  it('lists shared system/data first', () =>
    expect(diskWarnings(disk({ sharedWithData: true, idStable: false, readOnly: true }))).toEqual(['sharedWithData', 'unstableId', 'readOnly']));
});

describe('diskTitle', () => {
  it('joins vendor and model', () => expect(diskTitle(disk({ vendor: "SanDisk'", model: 'Cruzer Fit' }))).toBe("SanDisk' Cruzer Fit"));
  it('falls back to the kernel name', () => expect(diskTitle(disk({ model: null }))).toBe('sda'));
});

describe('unpartitionedBytes', () => {
  it('ignores alignment gaps', () => expect(unpartitionedBytes(disk({ partitions: [part('sda1', 100 * GiB - 2 * 1024 * 1024)] }))).toBe(0));
  it('reports real free space', () => expect(unpartitionedBytes(disk({ partitions: [part('sda1', 60 * GiB)] }))).toBe(40 * GiB));
  it('is zero for a filesystem on the whole disk', () => expect(unpartitionedBytes(disk({ partitions: [part('sda', 100 * GiB)] }))).toBe(0));
  it('is the whole disk without partitions', () => expect(unpartitionedBytes(disk())).toBe(100 * GiB));
});
