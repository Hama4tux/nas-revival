#!/usr/bin/env python3
"""Offline ELF/dependency checks. Never executes an ARM binary or runs ldd."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

REQUIRED = ('bin/sh', 'bin/busybox', 'usr/sbin/dropbear', 'usr/bin/dropbearkey',
            'etc/passwd', 'etc/group', 'etc/shadow', 'etc/shells',
            'etc/nas-revival-stage1.json', '.nas-revival-stage1')


def readelf(path, *options):
    return subprocess.check_output(['readelf', *options, str(path)], text=True,
                                   env={**os.environ, 'LC_ALL': 'C'}, stderr=subprocess.STDOUT)


def inside(root, path):
    result = path.resolve()
    if not result.is_relative_to(root):
        raise ValueError(f'Escaping symlink: {path.relative_to(root)}')
    return result


def check_arm_abi(header, attrs, notes):
    if not all(re.search(pattern, header) for pattern in
               (r'Class:\s+ELF32\b', r'Data:.*little endian', r'Machine:\s+ARM\b',
                r'Flags:.*Version5 EABI')):
        raise ValueError('Not ARM little-endian ELF32 EABI5')
    if 'hard-float ABI' in header or 'VFP registers' in attrs:
        raise ValueError('Hard-float ABI')
    arch = re.search(r'Tag_CPU_arch:\s*(\S+)', attrs)
    if not arch or arch[1] not in ('v4T', 'v5T', 'v5TE', 'v5TEJ'):
        raise ValueError('Missing or too-new ARM ISA attribute')
    if re.search(r'Tag_FP_arch:', attrs) or re.search(r'Tag_Advanced_SIMD_arch:', attrs):
        raise ValueError('Floating point/SIMD instructions required')
    for version in re.findall(r'OS: Linux, ABI: (\d+\.\d+\.\d+)', notes):
        if tuple(map(int, version.split('.'))) > (2, 6, 31):
            raise ValueError(f'GNU ABI tag requires newer kernel: {version}')
    return arch[1]


def audit_elf(path):
    header = readelf(path, '-h')
    arch = check_arm_abi(header, readelf(path, '-A'), readelf(path, '-n'))
    if not re.search(r'Type:\s+(EXEC|DYN)\b', header):
        raise ValueError(f'Not a runtime executable/shared object: {path.name}')
    dynamic = readelf(path, '-d')
    if '(RPATH)' in dynamic or '(RUNPATH)' in dynamic:
        raise ValueError(f'Unexpected library search override: {path.name}')
    needed = re.findall(r'\(NEEDED\).*\[([^\]]+)\]', dynamic)
    programs = readelf(path, '-l')
    interpreter = re.search(r'Requesting program interpreter:\s*([^\]]+)\]', programs)
    symbols = readelf(path, '--dyn-syms', '--wide')
    exports, imports = set(), set()
    for line in symbols.splitlines():
        fields = line.split()
        if len(fields) < 8 or not fields[0].rstrip(':').isdigit():
            continue
        name = fields[7].split('@')[0]
        if fields[6] == 'UND' and fields[4] == 'GLOBAL':
            imports.add(name)
        elif fields[6] != 'UND' and fields[4] in ('GLOBAL', 'WEAK'):
            exports.add(name)
    return {'isa': arch, 'needed': needed, 'interpreter': interpreter[1] if interpreter else None,
            'exports': exports, 'imports': imports}


def audit(root, build):
    root, build = Path(root).resolve(), Path(build).resolve()
    for name in REQUIRED:
        path = inside(root, root / name)
        if not path.is_file():
            raise ValueError(f'Missing {name}')
    if (root / '.nas-revival-stage1').read_text() != 'nas-revival-stage1-v1\n':
        raise ValueError('Wrong rootfs marker')
    config = (build / '.config').read_text()
    for option in ('BR2_arm=y', 'BR2_arm926t=y', 'BR2_ARM_SOFT_FLOAT=y',
                   'BR2_TOOLCHAIN_BUILDROOT_UCLIBC=y', 'BR2_PTHREADS_NONE=y',
                   'BR2_KERNEL_HEADERS_CUSTOM_GIT=y', 'BR2_INIT_NONE=y'):
        if option not in config.splitlines():
            raise ValueError(f'Unexpected resolved configuration: {option}')
    if 'BR2_LINUX_KERNEL=y' in config.splitlines():
        raise ValueError('A kernel build is forbidden in Stage 1')
    headers = list((build / 'build').glob('linux-headers-*/usr/include/linux/version.h'))
    if len(headers) != 1 or not re.search(r'LINUX_VERSION_CODE\s+132639\b', headers[0].read_text()):
        raise ValueError('Header API version must be exactly 2.6.31, not newer')
    libc_configs = list((build / 'build').glob('uclibc-*/.config'))
    if len(libc_configs) != 1:
        raise ValueError('Missing generated libc configuration')
    libc = libc_configs[0].read_text().splitlines()
    for forbidden in ('UCLIBC_USE_TIME64=y', 'UCLIBC_HAS_THREADS_NATIVE=y', 'UCLIBC_HAS_THREADS=y'):
        if forbidden in libc:
            raise ValueError(f'Unexpected libc requirement: {forbidden}')
    options = list((build / 'build').glob('dropbear-*/localoptions.h'))
    if len(options) != 1:
        raise ValueError('Missing generated Dropbear options')
    text = options[0].read_text()
    for name in ('DROPBEAR_SVR_PASSWORD_AUTH', 'DROPBEAR_SVR_PAM_AUTH'):
        definitions = re.findall(r'^\s*#define\s+' + name + r'\s+(\d+)\s*$', text, re.M)
        if not definitions or definitions[-1] != '0':
            raise ValueError(f'Password authentication not compiled out: {name}')
    records, size, digests = {}, 0, {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            inside(root, path)
            continue
        if path.is_dir():
            continue
        if not path.is_file():
            raise ValueError(f'Special file must be created only at runtime: {path}')
        raw = path.read_bytes()
        relative = str(path.relative_to(root))
        if (raw.lstrip().startswith(b'-----BEGIN') and b'PRIVATE KEY-----' in raw[:160]) or 'host_key' in path.name:
            raise ValueError(f'Private key material in rootfs: {relative}')
        size += len(raw)
        digests[relative] = hashlib.sha256(raw).hexdigest()
        if raw.startswith(b'\x7fELF'):
            records[relative] = audit_elf(path)
    if not records:
        raise ValueError('No ELF binaries to validate')
    for name in ('bin/busybox', 'usr/sbin/dropbear'):
        relative = str(inside(root, root / name).relative_to(root))
        if relative not in records:
            raise ValueError(f'Required program is not a checked ELF: {name}')
    for name, record in records.items():
        closure, queue = set(), list(record['needed'])
        if record['interpreter']:
            queue.append(record['interpreter'])
        elif name.startswith(('lib/', 'usr/lib/')):
            # Shared libc can import its interpreter's symbols without DT_NEEDED.
            queue.append('/lib/ld-uClibc.so.0')
        while queue:
            library = queue.pop()
            candidates = [root / library.lstrip('/')] if library.startswith('/') else [
                root / 'lib' / library, root / 'usr/lib' / library]
            found = next((inside(root, p) for p in candidates if p.exists()), None)
            if found is None:
                raise ValueError(f'{name}: missing loader/library {library}')
            key = str(found.relative_to(root))
            if key not in records:
                raise ValueError(f'{name}: dependency is not checked ELF: {key}')
            if key not in closure:
                closure.add(key)
                queue.extend(records[key]['needed'])
        available = set(record['exports'])
        for key in closure:
            available.update(records[key]['exports'])
        missing = record['imports'] - available
        if missing:
            raise ValueError(f'{name}: unresolved strong symbols: {sorted(missing)}')
    return {
        'elf_count': len(records), 'rootfs_regular_bytes': size,
        'isa_and_dependency_checks': 'passed', 'kernel_headers': '2.6.31.8 (API 2.6.31)',
        'kernel_runtime_compatibility': 'UNVERIFIED: static ELF checks do not prove syscall/runtime behavior',
        'binaries': {name: {k: v for k, v in r.items() if k not in ('exports', 'imports')}
                     for name, r in records.items()}, 'sha256': digests,
    }


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('rootfs')
    parser.add_argument('build_output')
    args = parser.parse_args()
    print(json.dumps(audit(args.rootfs, args.build_output), indent=2))
