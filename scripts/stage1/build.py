#!/usr/bin/env python3
"""Build only a chroot userspace. Requires a pre-existing pinned Buildroot checkout."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

from audit_rootfs import audit

BUILDROOT_COMMIT = '4f924fd52841608d68ac3dcb83fccee8b8c90316'
EPOCH = 1792683600
# Buildroot keeps sources, downloads and object files on disk. On a RAM-backed
# filesystem they compete with the desktop for memory; that froze the build VM.
RAM_FILESYSTEMS = {'tmpfs', 'ramfs'}
GIB = 1024 ** 3
MIN_AVAILABLE_RAM = 2 * GIB
RAM_PER_JOB = 3 * GIB // 2
MIN_FREE_DISK = 10 * GIB


def filesystem_type(path):
    """Type of the mount holding path (or its nearest existing parent)."""
    path = Path(path).resolve()
    while not path.exists():
        path = path.parent
    best, kind = '', ''
    for line in Path('/proc/self/mounts').read_text().splitlines():
        fields = line.split()
        mount = fields[1].replace('\\040', ' ')
        if (path == Path(mount) or Path(mount) in path.parents) and len(mount) > len(best):
            best, kind = mount, fields[2]
    return kind


def available_ram():
    for line in Path('/proc/meminfo').read_text().splitlines():
        if line.startswith('MemAvailable:'):
            return int(line.split()[1]) * 1024
    raise ValueError('MemAvailable not found in /proc/meminfo')


def job_count(cpus, ram):
    """Parallel jobs bounded by CPUs, available RAM and a hard cap of 8."""
    if ram < MIN_AVAILABLE_RAM:
        raise ValueError(f'Only {ram // 2**20} MiB RAM available; close applications first')
    return max(1, min(cpus or 1, ram // RAM_PER_JOB, 8))


def check_build_paths(paths):
    for name, path in paths.items():
        kind = filesystem_type(path)
        if kind in RAM_FILESYSTEMS:
            raise ValueError(f'{name} {path} is on {kind} (RAM); use a disk path, e.g. ~/build')


def run(command, **kwargs):
    subprocess.run(command, check=True, **kwargs)


def pack(root, destination):
    with destination.open('xb') as stream:
        with gzip.GzipFile(fileobj=stream, filename='', mode='wb', mtime=EPOCH) as compressed:
            with tarfile.open(fileobj=compressed, mode='w|', format=tarfile.PAX_FORMAT) as archive:
                for path in sorted(root.rglob('*')):
                    relative = path.relative_to(root).as_posix()
                    # Target helper uses whitespace-free, bounded paths too.
                    if not relative or any(c.isspace() for c in relative):
                        raise ValueError(f'Unexpected archive path: {relative}')
                    info = archive.gettarinfo(str(path), arcname=relative)
                    if not (info.isdir() or info.isfile() or info.issym()):
                        raise ValueError(f'No special files/hardlinks allowed: {relative}')
                    info.uid = info.gid = 0
                    info.uname = info.gname = 'root'
                    info.mtime = EPOCH
                    info.pax_headers = {}
                    if info.isfile():
                        with path.open('rb') as data:
                            archive.addfile(info, data)
                    else:
                        archive.addfile(info)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('buildroot', help='existing clean checkout at the pinned commit')
    parser.add_argument('output', help='new empty output directory')
    parser.add_argument('--configure-only', action='store_true')
    args = parser.parse_args()
    source, output = Path(args.buildroot).resolve(), Path(args.output).resolve()
    repo = Path(__file__).resolve().parents[2]
    external = repo / 'userspace/stage1'
    if os.geteuid() == 0:
        raise ValueError('Build as an ordinary workstation user, without sudo')
    revision = subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip()
    if revision != BUILDROOT_COMMIT:
        raise ValueError(f'Expected Buildroot {BUILDROOT_COMMIT}, got {revision}')
    dirty = subprocess.check_output(['git', '-C', str(source), 'status', '--porcelain'], text=True)
    if dirty:
        raise ValueError('Buildroot source must be clean; no automatic stash/reset')
    if output == source or output.is_relative_to(source) or any(c.isspace() for c in str(output)):
        raise ValueError('Use a separate output path without whitespace')
    if output.exists() and any(output.iterdir()):
        raise ValueError('Refusing to overwrite an existing build; choose a fresh output path')
    downloads = Path(os.environ.get('BR2_DL_DIR') or Path.home() / '.cache/nas-revival/dl').resolve()
    check_build_paths({'Buildroot source': source, 'Output': output, 'Download cache': downloads})
    jobs = job_count(os.cpu_count(), available_ram())
    output.mkdir(parents=True, exist_ok=True)
    if shutil.disk_usage(output).free < MIN_FREE_DISK:
        raise ValueError(f'Less than {MIN_FREE_DISK // GIB} GiB free for {output}')
    downloads.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, 'LC_ALL': 'C', 'TZ': 'UTC', 'SOURCE_DATE_EPOCH': str(EPOCH),
           'BR2_DL_DIR': str(downloads)}
    make = ['make', '-C', str(source), f'O={output}', f'BR2_EXTERNAL={external}']
    run(make + ['nsa310_stage1_defconfig'], env=env)
    config = (output / '.config').read_text().splitlines()
    for option in ('BR2_TOOLCHAIN_BUILDROOT_UCLIBC=y', 'BR2_ARM_SOFT_FLOAT=y',
                   'BR2_PTHREADS_NONE=y', 'BR2_INIT_NONE=y',
                   'BR2_PACKAGE_DROPBEAR=y', 'BR2_KERNEL_HEADERS_CUSTOM_TARBALL=y',
                   'BR2_DOWNLOAD_FORCE_CHECK_HASHES=y',
                   'BR2_PACKAGE_HOST_LINUX_HEADERS_CUSTOM_REALLY_OLD=y'):
        if option not in config:
            raise ValueError(f'Configuration was not honored: {option}')
    for forbidden in ('BR2_LINUX_KERNEL=y', 'BR2_TOOLCHAIN_BUILDROOT_GLIBC=y',
                      'BR2_TOOLCHAIN_BUILDROOT_MUSL=y', 'BR2_KERNEL_HEADERS_CUSTOM_GIT=y'):
        if forbidden in config:
            raise ValueError(f'Unexpected setting: {forbidden}')
    if args.configure_only:
        print('Configuration generated. No toolchain/rootfs build or runtime validation performed.')
        return
    print(f'Building with -j{jobs}; downloads cached in {downloads}')
    run(make + [f'-j{jobs}'], env=env)
    root = output / 'target'
    result = audit(root, output)
    # Runtime verification file, relative paths only; no secret or host key in image.
    manifest = root / 'etc/stage1-files.sha256'
    manifest.write_text(''.join(f'{digest}  {name}\n' for name, digest in result['sha256'].items()))
    artifact = output / 'images/rootfs-stage1.tar.gz'
    artifact.parent.mkdir(exist_ok=True)
    pack(root, artifact)
    result['archive_bytes'] = artifact.stat().st_size
    result['archive_sha256'] = hashlib.sha256(artifact.read_bytes()).hexdigest()
    (artifact.parent / 'audit-stage1.json').write_text(json.dumps(result, indent=2) + '\n')
    print(f'Built: {artifact}\nArchive SHA256: {result["archive_sha256"]}')
    print('ARM checks passed. Compatibility with the vendor 2.6.31.8 kernel remains UNVERIFIED.')


if __name__ == '__main__':
    main()
