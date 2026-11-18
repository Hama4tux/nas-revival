#!/bin/sh
# Layer 3 simulation: full OpenWrt (same release, armsr/armv7) in QEMU with the
# NSA310 memory budget (256 MB, 1 CPU) and a sparse 3 TB disk in place of SATA.
# Usage: sim-vm.sh [SCENARIO]   (default scenarios/storage.sh)
# Starts from fresh copies every run; the log goes to $SIM_DIR/vm/.
set -eu
. "$(dirname "$0")/env.sh"

scenario=${1:-$(dirname "$0")/scenarios/storage.sh}
vm=$SIM_DIR/vm
mkdir -p "$vm"
P=$DL_DIR/armsr/openwrt-$OPENWRT_VERSION-armsr-armv7-generic
gunzip -c "$P-squashfs-rootfs.img.gz" > "$vm/rootfs.img"
rm -f "$vm/sata.qcow2"
tool qemu-img create -q -f qcow2 "$vm/sata.qcow2" 3T

log=$vm/$(basename "$scenario" .sh).log
LD_LIBRARY_PATH=$QEMU_LIBS python3 "$(dirname "$0")/sim_vm.py" \
    --qemu "$(command -v qemu-system-arm || echo "$TOOLS_DIR/usr/bin/qemu-system-arm")" \
    --kernel "$P-kernel.bin" --rootfs "$vm/rootfs.img" --disk "$vm/sata.qcow2" \
    --script "$scenario" --log "$log"
grep -aE '^(RESULT|### )' "$log" | tr -d '\r'
