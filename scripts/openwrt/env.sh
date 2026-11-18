# Shared settings for the OpenWrt workstation scripts. Source, do not execute.
# Everything is downloaded and built outside the repository (Git must never
# contain images, Image Builder trees or private device data).
OPENWRT_VERSION=25.12.6
OPENWRT_BASE=https://downloads.openwrt.org/releases/$OPENWRT_VERSION/targets
# OpenWrt Build System (Nitrokey3) primary key, from github.com/openwrt/keyring
# (gpg/0x1D53D1877742E911.asc). Its subkey ...0606D1D9 signs 25.12 sha256sums.
OPENWRT_GPG_FPR=8A8BC12F46B836C0F9CDB36F1D53D1877742E911
OPENWRT_KEY_URL=https://raw.githubusercontent.com/openwrt/keyring/master/gpg/0x1D53D1877742E911.asc

SIM_DIR=${NAS_SIM_DIR:-$HOME/.cache/nas-openwrt-sim}
DL_DIR=$SIM_DIR/dl
IB_DIR=$SIM_DIR/ib/openwrt-imagebuilder-$OPENWRT_VERSION-kirkwood-generic.Linux-x86_64
OUT_DIR=$SIM_DIR/out
# Host tools unpacked from Ubuntu packages without root (see docs/research/openwrt-simulation.md).
TOOLS_DIR=${NAS_SIM_TOOLS:-$SIM_DIR/tools}
QEMU_LIBS=$TOOLS_DIR/usr/lib/x86_64-linux-gnu

REPO_DIR=$(cd "$(dirname "$0")/../.." && pwd)

tool() {  # tool NAME ARGS...: run a host tool, preferring the system one
    name=$1; shift
    if command -v "$name" >/dev/null 2>&1; then "$name" "$@"
    else LD_LIBRARY_PATH=$QEMU_LIBS "$TOOLS_DIR/usr/bin/$name" "$@"; fi
}
