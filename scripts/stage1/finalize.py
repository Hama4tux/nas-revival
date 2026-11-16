#!/usr/bin/env python3
"""Post-build hook: change only the generated target tree, never the host OS."""
import json
import os
from pathlib import Path
import shutil
import sys


def finalize(target):
    root = Path(target).resolve()
    if root == Path('/') or root.name != 'target' or not (root / 'bin/busybox').is_file():
        raise ValueError('Expected a populated Buildroot output/target directory')
    # Buildroot supplies network/init files for a bootable system; this is a chroot.
    for name in ('etc/init.d', 'etc/network', 'etc/dropbear', 'etc/resolv.conf',
                 'etc/hostname', 'sbin/init', 'usr/bin/dropbearconvert', 'usr/bin/scp',
                 'usr/bin/dbclient', 'usr/bin/ssh'):
        path = root / name
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.is_dir():
            shutil.rmtree(path)
    files = {
        '.nas-revival-stage1': 'nas-revival-stage1-v1\n',
        'etc/passwd': 'root:x:0:0:NAS Revival:/root:/bin/sh\n',
        'etc/group': 'root:x:0:\ntty:x:5:\n',
        'etc/shadow': 'root:!:0:0:99999:7:::\n',
        'etc/shells': '/bin/sh\n',
        'etc/hosts': '127.0.0.1 localhost\n::1 localhost\n',
        'etc/resolv.conf': '# Intentionally empty; no host DNS copied or changed.\n',
    }
    for name, contents in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(contents)
        path.chmod(0o600 if name == 'etc/shadow' else 0o644)
    for name in ('root', 'root/.ssh', 'etc/dropbear', 'proc', 'dev', 'run', 'tmp'):
        path = root / name
        if path.is_symlink():
            path.unlink()
        path.mkdir(parents=True, exist_ok=True)
        path.chmod(0o700 if name in ('root', 'root/.ssh', 'etc/dropbear') else
                   0o1777 if name == 'tmp' else 0o755)
    metadata = {
        'schema': 1, 'stage': 1, 'architecture': 'armv5te-soft-eabi',
        'kernel_headers': '2.6.31.8', 'kernel_runtime_verified': False,
        'authentication': 'public-key-only', 'port': 2222,
        'libc': 'uclibc-ng', 'init': None,
    }
    (root / 'etc/nas-revival-stage1.json').write_text(json.dumps(metadata, indent=2) + '\n')
    # Relative links can be extracted safely into a fresh directory on the NAS.
    for path in root.rglob('*'):
        if path.is_symlink() and os.readlink(path).startswith('/'):
            destination = root / os.readlink(path).lstrip('/')
            relative = os.path.relpath(destination, path.parent)
            path.unlink()
            path.symlink_to(relative)


if __name__ == '__main__':
    finalize(sys.argv[1])
