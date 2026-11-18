#!/bin/sh
# Build the NSA310 test image with the verified OpenWrt Image Builder:
# zyxel_nsa310b profile + firmware/openwrt/packages.txt + firmware/openwrt/files.
# Output goes to $OUT_DIR (outside Git). Run fetch.sh first. No device access.
set -eu
. "$(dirname "$0")/env.sh"

archive=$DL_DIR/kirkwood/openwrt-imagebuilder-$OPENWRT_VERSION-kirkwood-generic.Linux-x86_64.tar.zst
[ -d "$IB_DIR" ] || { mkdir -p "$(dirname "$IB_DIR")"; tar --zstd -xf "$archive" -C "$(dirname "$IB_DIR")"; }

packages=$(sed -e 's/#.*//' "$REPO_DIR/firmware/openwrt/packages.txt" | xargs)

# The Image Builder requires GNU awk; use the unpacked one if the host lacks it.
if ! awk --version 2>/dev/null | grep -q GNU; then
    mkdir -p "$SIM_DIR/bin"
    printf '#!/bin/sh\nLD_LIBRARY_PATH=%s exec %s "$@"\n' "$QEMU_LIBS" "$TOOLS_DIR/usr/bin/gawk" > "$SIM_DIR/bin/awk"
    chmod +x "$SIM_DIR/bin/awk"
    PATH=$SIM_DIR/bin:$PATH
fi

mkdir -p "$OUT_DIR"
make -C "$IB_DIR" image PROFILE=zyxel_nsa310b BIN_DIR="$OUT_DIR" \
    PACKAGES="$packages" FILES="$REPO_DIR/firmware/openwrt/files"
ls -l "$OUT_DIR"
