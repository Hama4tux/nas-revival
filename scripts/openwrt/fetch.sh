#!/bin/sh
# Download pinned OpenWrt release files for kirkwood (NSA310b profile) and
# armsr/armv7 (QEMU simulation) and verify them: GPG signature of sha256sums
# against the pinned OpenWrt key fingerprint, then SHA-256 of every file.
# Workstation only; nothing touches the NAS.
set -eu
. "$(dirname "$0")/env.sh"

GNUPGHOME=$SIM_DIR/gnupg; export GNUPGHOME
mkdir -p "$GNUPGHOME" && chmod 700 "$GNUPGHOME"
if ! gpg --list-keys "$OPENWRT_GPG_FPR" >/dev/null 2>&1; then
    curl -fsS "$OPENWRT_KEY_URL" | gpg --quiet --import
fi

fetch_target() {  # fetch_target TARGET/SUBTARGET FILE...
    target=$1; shift
    dir=$DL_DIR/${target%%/*}
    mkdir -p "$dir"
    cd "$dir"
    curl -fsS -O "$OPENWRT_BASE/$target/sha256sums" -O "$OPENWRT_BASE/$target/sha256sums.asc"
    status=$(gpg --status-fd 1 --verify sha256sums.asc sha256sums 2>/dev/null)
    echo "$status" | grep -q "VALIDSIG .* $OPENWRT_GPG_FPR\$" || {
        echo "sha256sums for $target: signature not made by $OPENWRT_GPG_FPR" >&2; exit 1; }
    for f in "$@"; do
        grep -q " \*$f\$" sha256sums || { echo "$f not listed in sha256sums" >&2; exit 1; }
        [ -f "$f" ] || curl -fsS -O "$OPENWRT_BASE/$target/$f"
        grep " \*$f\$" sha256sums | sha256sum -c --quiet
    done
    echo "verified $target: $*"
}

P=openwrt-$OPENWRT_VERSION
fetch_target kirkwood/generic \
    "$P-kirkwood-generic-zyxel_nsa310b-initramfs-uImage" \
    "$P-kirkwood-generic-zyxel_nsa310b-squashfs-sysupgrade.bin" \
    profiles.json \
    "openwrt-imagebuilder-$OPENWRT_VERSION-kirkwood-generic.Linux-x86_64.tar.zst"
fetch_target armsr/armv7 \
    "$P-armsr-armv7-generic-kernel.bin" \
    "$P-armsr-armv7-generic-squashfs-rootfs.img.gz"
