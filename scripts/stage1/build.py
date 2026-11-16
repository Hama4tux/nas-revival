#!/usr/bin/env python3
"""Build only a chroot userspace. Requires a pre-existing pinned Buildroot checkout."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile

from audit_rootfs import audit

BUILDROOT_COMMIT = '4f924fd52841608d68ac3dcb83fccee8b8c90316'
EPOCH = 1792683600


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
    output.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, 'LC_ALL': 'C', 'TZ': 'UTC', 'SOURCE_DATE_EPOCH': str(EPOCH)}
    make = ['make', '-C', str(source), f'O={output}', f'BR2_EXTERNAL={external}']
    run(make + ['nsa310_stage1_defconfig'], env=env)
    config = (output / '.config').read_text().splitlines()
    for option in ('BR2_TOOLCHAIN_BUILDROOT_UCLIBC=y', 'BR2_ARM_SOFT_FLOAT=y',
                   'BR2_PTHREADS_NONE=y', 'BR2_INIT_NONE=y',
                   'BR2_PACKAGE_DROPBEAR=y', 'BR2_KERNEL_HEADERS_CUSTOM_GIT=y',
                   'BR2_PACKAGE_HOST_LINUX_HEADERS_CUSTOM_REALLY_OLD=y'):
        if option not in config:
            raise ValueError(f'Configuration was not honored: {option}')
    for forbidden in ('BR2_LINUX_KERNEL=y', 'BR2_TOOLCHAIN_BUILDROOT_GLIBC=y',
                      'BR2_TOOLCHAIN_BUILDROOT_MUSL=y'):
        if forbidden in config:
            raise ValueError(f'Unexpected setting: {forbidden}')
    if args.configure_only:
        print('Configuration generated. No toolchain/rootfs build or runtime validation performed.')
        return
    run(make + [f'-j{min(os.cpu_count() or 1, 8)}'], env=env)
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
