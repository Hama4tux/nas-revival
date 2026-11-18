#!/bin/sh
# Layer 2 simulation: run binaries from the built kirkwood rootfs under
# qemu-user restricted to an ARM926 (ARMv5TE, the Feroceon instruction set).
# An instruction the NAS CPU lacks ends in SIGILL here. Run build-image.sh first.
set -eu
. "$(dirname "$0")/env.sh"

root=$IB_DIR/build_dir/target-arm_xscale_musl_eabi/root-kirkwood
[ -x "$root/bin/busybox" ] || { echo "no rootfs at $root; run build-image.sh" >&2; exit 1; }
disk=$SIM_DIR/user-test.img
QEMU_ARM=$(command -v qemu-arm || echo "$TOOLS_DIR/usr/bin/qemu-arm")
fail=0

check() {  # check LABEL CMD ARGS...: binary must not crash with a signal
    label=$1; bin=$2; shift 2
    set +e
    # qemu-user forwards the environment to the guest, so no host
    # LD_LIBRARY_PATH here: it would redirect the ARM dynamic loader.
    out=$(timeout 60 "$QEMU_ARM" -cpu arm926 -L "$root" "$root$bin" "$@" 2>&1 < /dev/null)
    rc=$?
    set -e
    # 126/127: binary missing or not executable; >128: killed by a signal (SIGILL=4)
    if [ $rc -ge 126 ]; then fail=1; echo "FAIL $label (exit $rc)"
    else echo "ok   $label: $(echo "$out" | head -1 | cut -c1-70)"; fi
}

check uname /bin/busybox uname -m
check mdadm /sbin/mdadm --version
check smartctl /usr/sbin/smartctl --version
check ucode /usr/bin/ucode -e 'print(sprintf("%J", {n: 2 ** 10}))'
check dropbear /usr/sbin/dropbear -V
check apk /usr/bin/apk --version
check kexec /usr/sbin/kexec --version
truncate -s 256M "$disk"
check mkfs.ext4 /usr/sbin/mkfs.ext4 -q -F "$disk"
check e2fsck /usr/sbin/e2fsck -fn "$disk"
rm -f "$disk"
exit $fail
