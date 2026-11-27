#!/bin/bash
# Build a NAS Revival release reproducibly: kernel (uImage + modules), the nas-revival binary, manifest.json,
# BUILDINFO and SHA256SUMS in output/release/<version>/ (docs/RELEASE.md). Nothing is signed here: the owner
# rebuilds locally and signs with scripts/release/sign-release.sh (ADR 0017).
# Usage: scripts/release/release.sh <version>      version = YYYY.MM.DD or YYYY.MM.DD.N (tag v<version>)
# Env:   OUT=dir (default output/release/<version>), JOBS=n, ALLOW_DIRTY=1 (test builds only; the manifest
#        then names the commit with -dirty and the result is not a release).
# Needs the kernel tools of build-kernel.sh, Go (GOTOOLCHAIN is pinned to go.mod), Node 22 and git.
# Tests: tests/test_release.sh sources it with RELEASE_LIB=1 (functions only).
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BOARD="zyxel,nsa310a"
ARCH="armel-armv5te"

die() { echo "$*" >&2; exit 1; }

# valid_version V: YYYY.MM.DD[.N] with a real month and day; the NAS compares versions number by number.
valid_version() {
  [[ "$1" =~ ^20[0-9]{2}\.(0[1-9]|1[0-2])\.(0[1-9]|[12][0-9]|3[01])(\.[1-9][0-9]{0,2})?$ ]]
}

# artifact_json FILE TYPE: one manifest entry (names and types are fixed by this script, no escaping needed).
artifact_json() {
  printf '    {"name": "%s", "type": "%s", "size": %s, "sha256": "%s"}' \
    "$(basename "$1")" "$2" "$(stat -c %s "$1")" "$(sha256sum "$1" | cut -d' ' -f1)"
}

# write_manifest DIR VERSION COMMIT EPOCH KREL: manifest.json over the artifacts in DIR. Every byte is fixed
# by the inputs (no build time), so a rebuild gives the same manifest and the signature covers all hashes.
write_manifest() {
  local dir="$1" ver="$2" commit="$3" epoch="$4" krel="$5"
  {
    printf '{\n  "schema": 1,\n  "product": "nas-revival",\n  "version": "%s",\n' "$ver"
    printf '  "board": "%s",\n  "arch": "%s",\n  "commit": "%s",\n' "$BOARD" "$ARCH" "$commit"
    printf '  "source_date_epoch": %s,\n  "kernel_release": "%s",\n  "artifacts": [\n' "$epoch" "$krel"
    artifact_json "$dir/nas-revival-armv5" app; printf ',\n'
    artifact_json "$dir/uImage" kernel; printf ',\n'
    artifact_json "$dir/modules-$krel.tar.gz" kernel-modules; printf ',\n'
    artifact_json "$dir/kernel-$krel.config" kernel-config; printf '\n'
    printf '  ]\n}\n'
  } > "$dir/manifest.json"
}

# buildinfo: the toolchain, so a hash mismatch between CI and the local rebuild can be traced to a tool.
buildinfo() {
  local p
  echo "commit $1"
  echo "source_date_epoch $2"
  for p in gcc-arm-linux-gnueabi binutils-arm-linux-gnueabi u-boot-tools; do
    echo "dpkg $p $(dpkg-query -W -f='${Version}' "$p" 2>/dev/null || echo missing)"
  done
  echo "cc $(arm-linux-gnueabi-gcc --version | head -1)"
  echo "go $(cd "$REPO/backend" && "$GO" version)"
  echo "node $(node --version) npm $(npm --version)"
}

[ "${RELEASE_LIB:-}" = 1 ] && return 0

VER="${1:-}"; valid_version "$VER" || die "usage: $0 <YYYY.MM.DD[.N]>"
OUT="$(realpath -m "${OUT:-$REPO/output/release/$VER}")"
cd "$REPO"
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  [ "${ALLOW_DIRTY:-}" = 1 ] || die "working tree has uncommitted changes; a release builds a commit"
  COMMIT="$(git rev-parse HEAD)-dirty"
else
  COMMIT="$(git rev-parse HEAD)"
fi
SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"; export SOURCE_DATE_EPOCH
# Exactly the Go of go.mod (a newer local Go would give other bytes); downloaded once when missing.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' backend/go.mod)"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"; export GO
[ -x "$GO" ] || die "Go not found; set GO=/path/to/go"
if ! command -v npm >/dev/null && [ -f "$HOME/.nvm/nvm.sh" ]; then
  # shellcheck disable=SC1091
  source "$HOME/.nvm/nvm.sh"
fi

rm -rf "$OUT"; mkdir -p "$OUT/kernel-build" "$OUT/webui-build"
# Reuse a kernel tarball downloaded earlier; build-kernel.sh checks it against kernel.org's sha256sums.
. scripts/release/versions.conf
[ ! -f "output/kernel/linux-$KERNEL_VERSION.tar.xz" ] || cp "output/kernel/linux-$KERNEL_VERSION.tar.xz" "$OUT/kernel-build/"
OUT="$OUT/kernel-build" scripts/debian/build-kernel.sh
NR_VERSION="$VER" OUT="$OUT/webui-build" scripts/webui/build.sh
KREL="$(cat "$OUT/kernel-build/kernel.release")"
cp "$OUT/kernel-build/uImage" "$OUT/kernel-build/modules-$KREL.tar.gz" "$OUT/kernel-build/kernel-$KREL.config" "$OUT/"
cp "$OUT/webui-build/nas-revival-armv5" "$OUT/"
rm -rf "$OUT/kernel-build" "$OUT/webui-build"

write_manifest "$OUT" "$VER" "$COMMIT" "$SOURCE_DATE_EPOCH" "$KREL"
buildinfo "$COMMIT" "$SOURCE_DATE_EPOCH" > "$OUT/BUILDINFO"
(cd "$OUT" && sha256sum manifest.json nas-revival-armv5 uImage "modules-$KREL.tar.gz" "kernel-$KREL.config" > SHA256SUMS)
cat "$OUT/SHA256SUMS"
echo "== release $VER ($COMMIT) in $OUT"
