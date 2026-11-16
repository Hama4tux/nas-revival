"""Workstation tests. All mount/chroot effects are simulated in temporary files."""
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / 'scripts/stage1'
sys.path.insert(0, str(SCRIPTS))
from audit_rootfs import audit_elf, check_arm_abi, inside, readelf
from finalize import finalize
import build


class BuildGuardTests(unittest.TestCase):
    def test_ram_backed_paths_are_refused(self):
        tmpfs = next((m for m in ('/dev/shm', '/tmp', '/run')
                      if build.filesystem_type(m) == 'tmpfs'), None)
        if tmpfs is None:
            self.skipTest('no tmpfs mount found')
        with self.assertRaisesRegex(ValueError, 'RAM'):
            build.check_build_paths({'Download cache': Path(tmpfs) / 'not-yet/dl'})
        build.check_build_paths({'Output': ROOT})

    def test_job_count_is_bounded_by_memory(self):
        gib = build.GIB
        self.assertEqual(build.job_count(8, 3 * gib), 2)
        self.assertEqual(build.job_count(4, 64 * gib), 4)
        self.assertEqual(build.job_count(32, 64 * gib), 8)
        with self.assertRaisesRegex(ValueError, 'close applications'):
            build.job_count(8, gib)

    def test_headers_come_from_hashed_tarball_not_git(self):
        import json
        external = ROOT / 'userspace/stage1'
        config = (external / 'configs/nsa310_stage1_defconfig').read_text()
        sources = json.loads((external / 'sources.json').read_text())
        self.assertNotIn('CUSTOM_GIT', config)
        self.assertIn(f'_TARBALL_LOCATION="{sources["linux_headers_url"]}"', config)
        self.assertIn('BR2_DOWNLOAD_FORCE_CHECK_HASHES=y', config)
        hashes = (external / 'patches/linux-headers/linux-headers.hash').read_text()
        self.assertIn(f'sha256  {sources["linux_headers_sha256"]}  linux-2.6.31.8.tar.xz', hashes)


class ElfAuditTests(unittest.TestCase):
    def test_native_elf_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'ARM'):
            audit_elf(Path('/bin/true'))

    @unittest.skipUnless(shutil.which('clang'), 'existing clang required; do not install it for tests')
    def test_real_arm_object_attributes_and_newer_kernel_note(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            c = path / 'probe.c'
            c.write_text('int probe(void) { return 0; }\n')
            for march, abi, accepted in [('armv5te', 'soft', True), ('armv7-a', 'soft', False),
                                         ('armv7-a', 'hard', False)]:
                obj = path / f'{march}-{abi}.o'
                subprocess.run(['clang', '--target=arm-linux-gnueabi', f'-march={march}',
                                f'-mfloat-abi={abi}', '-c', str(c), '-o', str(obj)], check=True)
                header, attrs = readelf(obj, '-h'), readelf(obj, '-A')
                if accepted:
                    self.assertEqual(check_arm_abi(header, attrs, ''), 'v5TE')
                    with self.assertRaisesRegex(ValueError, 'newer kernel'):
                        check_arm_abi(header, attrs, 'OS: Linux, ABI: 3.2.0')
                    with self.assertRaisesRegex(ValueError, 'runtime executable'):
                        audit_elf(obj)
                else:
                    with self.assertRaises(ValueError):
                        check_arm_abi(header, attrs, '')

    def test_escaping_symlink_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'bad').symlink_to('/etc/passwd')
            with self.assertRaisesRegex(ValueError, 'Escaping'):
                inside(root, root / 'bad')

    def test_finalizer_removes_init_and_does_not_create_keys(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / 'target'
            (root / 'bin').mkdir(parents=True)
            (root / 'bin/busybox').write_bytes(b'fixture')
            (root / 'etc/init.d').mkdir(parents=True)
            (root / 'etc/init.d/S50dropbear').write_text('unsafe autostart fixture')
            (root / 'etc/dropbear').symlink_to('/var/run/dropbear')
            finalize(root)
            self.assertFalse((root / 'etc/init.d').exists())
            self.assertFalse((root / 'etc/dropbear').is_symlink())
            self.assertEqual(list((root / 'etc/dropbear').iterdir()), [])
            self.assertIn('root:!', (root / 'etc/shadow').read_text())
            self.assertFalse((root / 'root/.ssh/authorized_keys').exists())


class ControllerSafetyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.base = Path(self.tmp.name)
        self.root = self.base / 'nas-revival-stage1'
        self.root.mkdir(mode=0o700)
        self.proc = self.base / 'proc'
        (self.proc / 'self').mkdir(parents=True)
        (self.proc / 'net').mkdir()
        (self.proc / 'mounts').write_text(f'/dev/test {self.base} ext4 rw 0 0\n')
        (self.proc / 'self/mountinfo').write_text('')
        (self.proc / 'cpuinfo').write_text('CPU architecture : 5TE\nProcessor : Feroceon\n')
        (self.proc / 'net/tcp').write_text('sl local_address rem_address st\n')
        self.fakebin = self.base / 'bin'
        self.fakebin.mkdir()
        fake = self.base / 'fake.py'
        fake.write_text(f'''#!/usr/bin/python3
import os,sys
from pathlib import Path
base=Path({str(self.base)!r}); proc=base/'proc'
name=Path(sys.argv[0]).name; args=sys.argv[1:]
if name=='id': print(0)
elif name=='uname': print('armv5tel' if args==['-m'] else '2.6.31.8')
elif name=='stat': print('0:600' if args[-1].endswith('authorized_keys') else '0:700')
elif name=='sleep': pass
elif name=='mount':
 kind=args[args.index('-t')+1]; point=args[-1]
 with (base/'calls').open('a') as f: f.write('mount '+kind+' '+point+'\\n')
 if kind=='devpts': sys.exit(1)
 lines=(proc/'self/mountinfo').read_text().splitlines()
 ident=100+len(lines)
 lines.append(f'{{ident}} 1 0:1 / {{point}} rw - {{kind}} {{kind}} rw')
 (proc/'self/mountinfo').write_text('\\n'.join(lines)+'\\n')
elif name=='umount':
 point=args[-1]
 with (base/'calls').open('a') as f: f.write('umount '+point+'\\n')
 lines=[l for l in (proc/'self/mountinfo').read_text().splitlines() if l.split()[4]!=point]
 (proc/'self/mountinfo').write_text('\\n'.join(lines)+('\\n' if lines else ''))
elif name=='chroot':
 with (base/'calls').open('a') as f: f.write('UNEXPECTED chroot\\n')
 sys.exit(99)
elif name=='mknod':
 Path(args[0]).write_bytes(b'simulated device, not a real node')
else: sys.exit(98)
''')
        fake.chmod(0o755)
        for name in ('id', 'uname', 'stat', 'sleep', 'mount', 'umount', 'chroot', 'mknod'):
            (self.fakebin / name).symlink_to(fake)
        text = (SCRIPTS / 'control-stage1.sh').read_text()
        # Only this temporary test copy has injected process/mount views and fake commands.
        text = text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin', f'PATH={self.fakebin}:/usr/bin:/bin')
        text = text.replace('PROC=/proc', f'PROC={self.proc}')
        self.controller = self.base / 'controller.sh'
        self.controller.write_text(text)
        elf = bytearray(52)
        elf[:6] = b'\x7fELF\x01\x01'
        elf[18] = 40
        elf[39] = 5
        for name in ('bin/busybox', 'usr/sbin/dropbear', 'usr/bin/dropbearkey'):
            p = self.root / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(elf)
        (self.root / 'bin/sh').symlink_to('busybox')
        for name in ('etc/passwd', 'etc/group', 'etc/shells', 'etc/stage1-files.sha256'):
            p = self.root / name
            p.parent.mkdir(exist_ok=True)
            p.write_text('')
        (self.root / '.nas-revival-stage1').write_text('nas-revival-stage1-v1\n')
        for name in ('proc', 'dev', 'run', 'root/.ssh', 'etc/dropbear'):
            (self.root / name).mkdir(parents=True, exist_ok=True)
        (self.root / 'root/.ssh/authorized_keys').write_text('ssh-ed25519 fixture\n')
        # A valid checksum manifest; no target binary is ever executed in these tests.
        import hashlib
        (self.root / 'etc/stage1-files.sha256').write_text(
            hashlib.sha256(elf).hexdigest() + '  bin/busybox\n')

    def tearDown(self):
        self.tmp.cleanup()

    def run_control(self, action, *args):
        return subprocess.run(['sh', str(self.controller), action, str(self.root), *args],
                              text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def calls(self):
        p = self.base / 'calls'
        return p.read_text().splitlines() if p.exists() else []

    def test_preflight_performs_no_mount_or_chroot(self):
        result = self.run_control('preflight')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls(), [])
        self.assertFalse((self.root / '.stage1-state').exists())

    def test_existing_port_refused_before_any_mutation(self):
        (self.proc / 'net/tcp').write_text('0: 00000000:08AE 00000000:0000 0A\n')
        result = self.run_control('start', '192.0.2.2', '--accept-unverified-kernel')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('already has a listener', result.stderr)
        self.assertEqual(self.calls(), [])
        self.assertFalse((self.root / '.stage1-state').exists())

    def test_unverified_kernel_requires_explicit_test_acknowledgement(self):
        result = self.run_control('probe')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_dropbear_hard_float_is_rejected_independently_of_busybox(self):
        path = self.root / 'usr/sbin/dropbear'
        data = bytearray(path.read_bytes())
        data[37] = 4
        path.write_bytes(data)
        result = self.run_control('preflight')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('hard-float', result.stderr)
        self.assertEqual(self.calls(), [])

    def test_preexisting_mount_is_never_adopted_or_unmounted(self):
        (self.proc / 'self/mountinfo').write_text(f'77 1 0:1 / {self.root}/proc rw - proc proc rw\n')
        result = self.run_control('probe', '--accept-unverified-kernel')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_private_pty_failure_rolls_back_only_own_mounts_in_reverse_order(self):
        result = self.run_control('start', '192.0.2.2', '--accept-unverified-kernel')
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls()
        self.assertEqual(calls[-3:], [f'umount {self.root}/dev', f'umount {self.root}/run',
                                    f'umount {self.root}/proc'])
        self.assertFalse(any('chroot' in c for c in calls))
        self.assertEqual((self.proc / 'self/mountinfo').read_text(), '')
        self.assertFalse((self.root / '.stage1-lock').exists())

    def test_explicit_no_pty_probe_avoids_devpts_and_cleans_up_after_shell_failure(self):
        result = self.run_control('probe', '--accept-unverified-kernel', '--no-pty')
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls()
        self.assertIn('UNEXPECTED chroot', calls)  # Fake execution stops here, not an ARM process.
        self.assertFalse(any(c.startswith('mount devpts ') for c in calls))
        self.assertFalse((self.root / 'dev/ptmx').exists())
        self.assertEqual((self.proc / 'self/mountinfo').read_text(), '')

    def test_stop_rejects_replaced_mount_id(self):
        state = self.root / '.stage1-state'
        state.mkdir(mode=0o700)
        (state / 'mounts').write_text(f'77|proc|{self.root}/proc\n')
        (self.proc / 'self/mountinfo').write_text(f'88 1 0:1 / {self.root}/proc rw - proc proc rw\n')
        result = self.run_control('stop')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('foreign/replaced', result.stderr)
        self.assertEqual(self.calls(), [])

    def test_stop_removes_owned_mount(self):
        state = self.root / '.stage1-state'
        state.mkdir(mode=0o700)
        (state / 'mounts').write_text(f'77|proc|{self.root}/proc\n')
        (self.proc / 'self/mountinfo').write_text(f'77 1 0:1 / {self.root}/proc rw - proc proc rw\n')
        result = self.run_control('stop')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls(), [f'umount {self.root}/proc'])

    def test_installer_rejects_bad_hash_key_and_member_without_creating_destination(self):
        import hashlib
        import io
        import tarfile
        installer = self.base / 'installer.sh'
        text = (SCRIPTS / 'install-stage1.sh').read_text()
        text = text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin', f'PATH={self.fakebin}:/usr/bin:/bin')
        text = text.replace('/proc/mounts', str(self.proc / 'mounts'))
        installer.write_text(text)
        parent = self.base / 'install'
        parent.mkdir()
        destination = parent / 'nas-revival-stage1'
        key = self.base / 'administrator.pub'
        archive = self.base / 'rootfs.tar.gz'
        for condition in ('wrong-hash', 'invalid-key', 'traversal'):
            with self.subTest(condition=condition):
                key.write_text('ssh-rsa AAAA\n' if condition == 'invalid-key' else 'ssh-ed25519 AAAA\n')
                with tarfile.open(archive, 'w:gz') as tar:
                    entry = tarfile.TarInfo('../escape' if condition == 'traversal' else 'fixture')
                    entry.size = 1
                    tar.addfile(entry, io.BytesIO(b'x'))
                digest = '0' * 64 if condition == 'wrong-hash' else hashlib.sha256(archive.read_bytes()).hexdigest()
                result = subprocess.run(['sh', str(installer), str(archive), digest,
                                         str(destination), str(key)], text=True,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(destination.exists(), result.stderr)
                self.assertFalse((self.base / 'escape').exists())


if __name__ == '__main__':
    unittest.main()
