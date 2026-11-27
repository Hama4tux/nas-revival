#!/bin/bash
# Build the NSA310 kernel: mainline 6.12 LTS, mvebu_v5_defconfig + NAS options, kirkwood-nsa310a.dtb appended.
# Output in $OUT: uImage (for the NAND slot / USB stick), modules-<release>.tar.gz, kernel-<release>.config, SHA256SUMS.
# Needs: arm-linux-gnueabi-gcc, make, bc, bison, flex, libssl-dev, u-boot-tools (mkimage), curl.
# Usage: scripts/debian/build-kernel.sh [version]   (env OUT=dir, default output/kernel; JOBS=n; LOCALVERSION, default -nr1)
set -euo pipefail
V="${1:-6.12.119}"
# Own release name, so the modules of a new build never overwrite those of the kernel kept in slot B.
LOCALVERSION="${LOCALVERSION:--nr1}"
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
  -e ARM_APPENDED_DTB -e ARM_ATAG_DTB_COMPAT -e ARM_ATAG_DTB_COMPAT_CMDLINE_FROM_BOOTLOADER \
  -e NETFILTER -e NF_CONNTRACK -e NF_TABLES -e NF_TABLES_IPV4 -e NFT_CT -e NFT_REJECT -e NFT_REJECT_IPV4 \
  -e NFT_LIMIT -e NFT_LOG -e NF_LOG_SYSLOG \
  -m EXFAT_FS -m NTFS3_FS -e TASKSTATS -e TASK_XACCT -e TASK_IO_ACCOUNTING -e PSI \
  --set-str LOCALVERSION "$LOCALVERSION" -d LOCALVERSION_AUTO
make -s olddefconfig
# Firewall (W5.2) built in: the helper runs nft with ProtectKernelModules=yes and cannot load modules.
# scripts/config silently drops options whose dependencies are missing: check the result.
for o in NETFILTER NF_CONNTRACK NF_TABLES NF_TABLES_IPV4 NFT_CT NFT_REJECT_IPV4 NFT_LIMIT NFT_LOG EXFAT_FS NTFS3_FS TASK_IO_ACCOUNTING PSI; do
  grep -Eq "^CONFIG_$o=[ym]" .config || { echo "CONFIG_$o missing from .config" >&2; exit 1; }
done
nice make -s -j"$JOBS" zImage modules dtbs
# Only known after the build (include/config/kernel.release), e.g. 6.12.119-nr1.
KREL="$(make -s kernelrelease)"

rm -rf "$OUT/modroot"; make -s INSTALL_MOD_PATH="$OUT/modroot" INSTALL_MOD_STRIP=1 modules_install
# The build link points into this workstation; update-kernel.sh refuses links in the tarball.
rm -f "$OUT/modroot/lib/modules/$KREL/build" "$OUT/modroot/lib/modules/$KREL/source"
# The stock U-Boot 1.1.4 cannot pass a device tree: append it to the kernel.
cat arch/arm/boot/zImage arch/arm/boot/dts/marvell/kirkwood-nsa310a.dtb > "$OUT/zImage-nsa310a.dtb"
mkimage -A arm -O linux -T kernel -C none -a 0x00008000 -e 0x00008000 \
  -n "Linux-$KREL-nsa310a" -d "$OUT/zImage-nsa310a.dtb" "$OUT/uImage" >/dev/null
cp .config "$OUT/kernel-$KREL.config"
# Owned by root, and rooted at usr/ so extracting into / cannot touch the /lib -> usr/lib symlink.
tar -C "$OUT/modroot" --owner=0 --group=0 --numeric-owner --transform "s,^lib,usr/lib," -czf "$OUT/modules-$KREL.tar.gz" lib
cd "$OUT"; sha256sum uImage "modules-$KREL.tar.gz" > SHA256SUMS
ls -l uImage "modules-$KREL.tar.gz"
# A slot is 10 MiB; refuse an image that would not fit.
[ "$(stat -c %s uImage)" -le $((10 * 1024 * 1024)) ] || { echo "uImage larger than the 10 MiB NAND slot" >&2; exit 1; }
echo "== kernel $KREL done: $OUT/uImage"
