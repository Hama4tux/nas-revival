#!/bin/bash
# Build the NSA310 kernel: mainline 6.12 LTS, mvebu_v5_defconfig + NAS options, kirkwood-nsa310a.dtb appended.
# Output in $OUT: uImage (for the NAND slot / USB stick), modules-$V.tar.gz, kernel-$V.config, SHA256SUMS.
# Needs: arm-linux-gnueabi-gcc, make, bc, bison, flex, libssl-dev, u-boot-tools (mkimage), curl.
# Usage: scripts/debian/build-kernel.sh [version]   (env OUT=dir, default output/kernel; JOBS=n)
set -euo pipefail
V="${1:-6.12.119}"
OUT="$(realpath -m "${OUT:-output/kernel}")"
JOBS="${JOBS:-$(nproc)}"
mkdir -p "$OUT"; cd "$OUT"

[ -f "linux-$V.tar.xz" ] || curl -fsSLO "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-$V.tar.xz"
curl -fsSL https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc | grep " linux-$V.tar.xz$" | sha256sum -c -
rm -rf "linux-$V"; tar xf "linux-$V.tar.xz"; cd "linux-$V"

export ARCH=arm CROSS_COMPILE=arm-linux-gnueabi-
make -s mvebu_v5_defconfig
./scripts/config \
  -e EXT4_FS -e EXT4_FS_POSIX_ACL -e USB_STORAGE -e USB_EHCI_HCD -e USB_EHCI_HCD_ORION -e SATA_MV \
  -e MD -e BLK_DEV_MD -e MD_RAID1 -e DEVTMPFS -e DEVTMPFS_MOUNT -e CGROUPS -e AUTOFS_FS \
  -e TMPFS_POSIX_ACL -e FHANDLE -m SENSORS_LM85 -m R8169 -m NFSD -e NFSD_V4 \
  -e LEDS_TRIGGER_DISK -e LEDS_TRIGGER_NETDEV -e LEDS_TRIGGER_PANIC -e USB_LEDS_TRIGGER_USBPORT \
  -e ARM_APPENDED_DTB -e ARM_ATAG_DTB_COMPAT -e ARM_ATAG_DTB_COMPAT_CMDLINE_FROM_BOOTLOADER
make -s olddefconfig
nice make -s -j"$JOBS" zImage modules dtbs

rm -rf "$OUT/modroot"; make -s INSTALL_MOD_PATH="$OUT/modroot" INSTALL_MOD_STRIP=1 modules_install
# The stock U-Boot 1.1.4 cannot pass a device tree: append it to the kernel.
cat arch/arm/boot/zImage arch/arm/boot/dts/marvell/kirkwood-nsa310a.dtb > "$OUT/zImage-nsa310a.dtb"
mkimage -A arm -O linux -T kernel -C none -a 0x00008000 -e 0x00008000 \
  -n "Linux-$V-nsa310a" -d "$OUT/zImage-nsa310a.dtb" "$OUT/uImage" >/dev/null
cp .config "$OUT/kernel-$V.config"
tar -C "$OUT/modroot" -czf "$OUT/modules-$V.tar.gz" lib
cd "$OUT"; sha256sum uImage "modules-$V.tar.gz" > SHA256SUMS
ls -l uImage "modules-$V.tar.gz"
# A slot is 10 MiB; refuse an image that would not fit.
[ "$(stat -c %s uImage)" -le $((10 * 1024 * 1024)) ] || { echo "uImage larger than the 10 MiB NAND slot" >&2; exit 1; }
echo "== kernel $V done: $OUT/uImage"
