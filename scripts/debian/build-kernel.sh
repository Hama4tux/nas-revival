#!/bin/bash
# Build the NSA310 kernel: mainline 6.12 LTS, mvebu_v5_defconfig + NAS options, kirkwood-nsa310a.dtb appended.
# Output in $OUT: uImage (for the NAND slot / USB stick), modules-<release>.tar.gz, kernel-<release>.config, SHA256SUMS.
# Needs: arm-linux-gnueabi-gcc, libc6-dev-armel-cross (Kconfig CC_CAN_LINK must be the same everywhere), make, bc, bison, flex, libssl-dev, u-boot-tools (mkimage), curl.
# Usage: scripts/debian/build-kernel.sh [version]   (env OUT=dir, default output/kernel; JOBS=n; LOCALVERSION)
# Version and suffix default to scripts/release/versions.conf.
# Reproducible: the same source and toolchain give the same uImage and modules tarball bytes. Build time,
# user and host are fixed (SOURCE_DATE_EPOCH, default the commit time of HEAD), the tarball is sorted with
# fixed owners and times and gzip stores no name or time (docs/RELEASE.md).
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=../release/versions.conf
. "$REPO/scripts/release/versions.conf"
V="${1:-$KERNEL_VERSION}"
LOCALVERSION="${LOCALVERSION:-$KERNEL_LOCALVERSION}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$REPO" log -1 --format=%ct)}"
export SOURCE_DATE_EPOCH KBUILD_BUILD_TIMESTAMP="$(date -u -d "@$SOURCE_DATE_EPOCH" '+%a %b %e %H:%M:%S UTC %Y')"
export KBUILD_BUILD_USER=nas-revival KBUILD_BUILD_HOST=release KBUILD_BUILD_VERSION=1
OUT="$(realpath -m "${OUT:-output/kernel}")"
JOBS="${JOBS:-$(nproc)}"
mkdir -p "$OUT"; cd "$OUT"

[ -f "linux-$V.tar.xz" ] || curl -fsSLO "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-$V.tar.xz"
curl -fsSL https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc | grep " linux-$V.tar.xz$" | sha256sum -c -
rm -rf "linux-$V"; tar xf "linux-$V.tar.xz"; cd "linux-$V"

export ARCH=arm CROSS_COMPILE=arm-linux-gnueabi-
# RUSTC=false: Kconfig records the version of any rustc on the build host (no Rust is built), which made the
# workstation's .config differ from CI's.
kmake() { make RUSTC=false "$@"; }
kmake -s mvebu_v5_defconfig
./scripts/config \
  -e EXT4_FS -e EXT4_FS_POSIX_ACL -e USB_STORAGE -e USB_EHCI_HCD -e USB_EHCI_HCD_ORION -e SATA_MV \
  -e MD -e BLK_DEV_MD -e MD_RAID1 -e DEVTMPFS -e DEVTMPFS_MOUNT -e CGROUPS -e AUTOFS_FS \
  -e TMPFS_POSIX_ACL -e FHANDLE -m SENSORS_LM85 -m R8169 -m NFSD -e NFSD_V4 \
  -e LEDS_TRIGGER_DISK -e LEDS_TRIGGER_NETDEV -e LEDS_TRIGGER_PANIC -e USB_LEDS_TRIGGER_USBPORT \
  -e ARM_APPENDED_DTB -e ARM_ATAG_DTB_COMPAT -e ARM_ATAG_DTB_COMPAT_CMDLINE_FROM_BOOTLOADER \
  -e NETFILTER -e NF_CONNTRACK -e NF_TABLES -e NF_TABLES_IPV4 -e NFT_CT -e NFT_REJECT -e NFT_REJECT_IPV4 \
  -e NFT_LIMIT -e NFT_LOG -e NF_LOG_SYSLOG \
  -m EXFAT_FS -m NTFS3_FS -e TASKSTATS -e TASK_XACCT -e TASK_IO_ACCOUNTING -e PSI \
  --set-str LOCALVERSION "$LOCALVERSION" -d LOCALVERSION_AUTO \
  -d DEBUG_INFO -d DEBUG_INFO_DWARF_TOOLCHAIN_DEFAULT -d DEBUG_INFO_DWARF4 -d DEBUG_INFO_DWARF5 -e DEBUG_INFO_NONE
kmake -s olddefconfig
# Firewall (W5.2) built in: the helper runs nft with ProtectKernelModules=yes and cannot load modules.
# scripts/config silently drops options whose dependencies are missing: check the result.
for o in NETFILTER NF_CONNTRACK NF_TABLES NF_TABLES_IPV4 NFT_CT NFT_REJECT_IPV4 NFT_LIMIT NFT_LOG EXFAT_FS NTFS3_FS TASK_IO_ACCOUNTING PSI; do
  grep -Eq "^CONFIG_$o=[ym]" .config || { echo "CONFIG_$o missing from .config" >&2; exit 1; }
done
# No debug info: it holds the absolute build path, which changes the build IDs in the image and every module
# (not reproducible). The NAS never used it; modules were stripped anyway.
grep -q '^CONFIG_DEBUG_INFO_NONE=y' .config || { echo "debug info still enabled" >&2; exit 1; }
nice make RUSTC=false -s -j"$JOBS" zImage modules dtbs
# Only known after the build (include/config/kernel.release), e.g. 6.12.119-nr1.
KREL="$(kmake -s kernelrelease)"

rm -rf "$OUT/modroot"; kmake -s INSTALL_MOD_PATH="$OUT/modroot" INSTALL_MOD_STRIP=1 modules_install
# The build link points into this workstation; update-kernel.sh refuses links in the tarball.
rm -f "$OUT/modroot/lib/modules/$KREL/build" "$OUT/modroot/lib/modules/$KREL/source"
# The stock U-Boot 1.1.4 cannot pass a device tree: append it to the kernel. mkimage takes the header
# time from SOURCE_DATE_EPOCH.
cat arch/arm/boot/zImage arch/arm/boot/dts/marvell/kirkwood-nsa310a.dtb > "$OUT/zImage-nsa310a.dtb"
mkimage -A arm -O linux -T kernel -C none -a 0x00008000 -e 0x00008000 \
  -n "Linux-$KREL-nsa310a" -d "$OUT/zImage-nsa310a.dtb" "$OUT/uImage" >/dev/null
cp .config "$OUT/kernel-$KREL.config"
# Owned by root, and rooted at usr/ so extracting into / cannot touch the /lib -> usr/lib symlink.
# Sorted, fixed times and modes from the umask-independent install, gzip without name and time: reproducible.
tar -C "$OUT/modroot" --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
  --mode=u+rw,go+r,go-w --format=gnu --transform "s,^lib,usr/lib," -cf - lib | gzip -9n > "$OUT/modules-$KREL.tar.gz"
cd "$OUT"; sha256sum uImage "modules-$KREL.tar.gz" > SHA256SUMS
ls -l uImage "modules-$KREL.tar.gz"
# A slot is 10 MiB; refuse an image that would not fit.
[ "$(stat -c %s uImage)" -le $((10 * 1024 * 1024)) ] || { echo "uImage larger than the 10 MiB NAND slot" >&2; exit 1; }
echo "$KREL" > "$OUT/kernel.release"
echo "== kernel $KREL done: $OUT/uImage"
