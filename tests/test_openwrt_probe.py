"""Static safety checks for the read-only OpenWrt qualification probe."""
import re
import subprocess
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/openwrt/probe-readonly.sh'

# Commands that could change device state; none may appear in the probe.
FORBIDDEN = (
    'fw_setenv', 'nandwrite', 'flash_erase', 'flash_eraseall', 'mtd_debug', 'dd', 'mount',
    'umount', 'insmod', 'rmmod', 'modprobe', 'i2cset', 'i2cget', 'i2cdetect', 'smartctl',
    'hdparm', 'mkfs', 'fdisk', 'reboot', 'poweroff', 'halt', 'kexec -l', 'kexec -e',
    'ifconfig', 'ip link', 'ip addr', 'route', 'echo 1', 'rm', 'mv', 'cp', 'tee', 'chmod',
    'telnetd', 'kill', 'sysctl -w',
)


class ProbeSafety(unittest.TestCase):
    def setUp(self):
        text = SCRIPT.read_text()
        # Ignore comments so the documentation header may name forbidden actions.
        self.code = '\n'.join(line.split('#', 1)[0] for line in text.splitlines())

    def test_shell_syntax(self):
        subprocess.run(['sh', '-n', str(SCRIPT)], check=True)

    def test_no_state_changing_commands(self):
        for command in FORBIDDEN:
            pattern = r'(^|[\s;|&(`$])' + re.escape(command) + r'($|[\s;|&)])'
            self.assertIsNone(re.search(pattern, self.code, re.M), command)

    def test_no_redirection_to_files(self):
        # Only redirections to /dev/null or file descriptors are allowed.
        for match in re.finditer(r'(?<![0-9&])>>?\s*([^\s&;|]+)', self.code):
            self.assertEqual(match.group(1), '/dev/null', match.group(0))

    def test_mac_and_serial_redacted(self):
        sample = 'HWaddr 00:19:CB:12:34:56\nserial=ABC123\nSerial Number: S1E9XYZ\n'
        script = SCRIPT.read_text()
        redact = script[script.index('redact() {'):script.index('section() {')]
        result = subprocess.run(['sh', '-c', redact + '\nredact'], input=sample,
                                capture_output=True, text=True, check=True).stdout
        self.assertNotIn('12:34:56', result)
        self.assertNotIn('ABC123', result)
        self.assertNotIn('S1E9XYZ', result)


if __name__ == '__main__':
    unittest.main()
