#!/bin/bash
# Kernel update on the NSA310 (runs on the NAS as root), docs/debian/boot.md "Updating the kernel".
#   1. slot A (mtd7, kernel_2) holds the running kernel, slot B (first 10 MiB of mtd8) the previous good one;
#   2. if B differs from A, copy A to B and verify it by reading it back – B becomes the known-good fallback;
#   3. install the modules of the new kernel (own release name, so the modules of the B kernel stay);
#   4. write the new uImage to A and verify it by reading it back.
# Does not reboot. U-Boot boots A first and falls back to B only when A has a bad magic or CRC; a kernel that
# hangs while booting needs the serial console: `run slotB` (scripts/debian/uboot-session.py).
# Usage: update-kernel.sh [--check] <uImage> <modules-<release>.tar.gz>
#   --check  only verify the files and show what would be written.
# Tests: tests/test_update_kernel.sh sources it with UPDATE_KERNEL_LIB=1 (functions only).
set -euo pipefail

SLOT_A=/dev/mtd7; SLOT_B=/dev/mtd8
SLOT_BYTES=$((10 * 1024 * 1024)); SLOT_BLOCKS=80   # 80 erase blocks × 128 KiB

die() { echo "$*" >&2; exit 1; }

# uimage_len FILE: total uImage length (64-byte header + data size) or fail when the magic is wrong.
uimage_len() {
  local magic size
  magic="$(od -An -tx1 -N4 "$1" | tr -d ' \n')"
  [ "$magic" = 27051956 ] || return 1
  size="$(od -An -tx1 -j12 -N4 "$1" | tr -d ' \n')"
  echo $((64 + 16#$size))
}
uimage_name() { dd if="$1" bs=1 skip=32 count=32 status=none | tr -d '\0'; }

# modules_release TARBALL LIST: print the one release the tarball installs; refuse anything that is not a
# plain path below usr/lib/modules/<release>/ (no lib/…, no absolute paths, no "..", no links out of it).
modules_release() {
  local rel
  tar -tzf "$1" > "$2"
  rel="$(sed -n 's,^usr/lib/modules/\([^/]*\)/.*,\1,p' "$2" | sort -u)"
  [ -n "$rel" ] && [ "$(printf '%s\n' "$rel" | wc -l)" -eq 1 ] || { echo "$1: expected one usr/lib/modules/<release>/" >&2; return 1; }
  if grep -Evq '^(usr/|usr/lib/|usr/lib/modules/|usr/lib/modules/'"$rel"'/.*)$' "$2" || grep -Eq '(^|/)\.\.(/|$)' "$2"; then
    echo "$1 contains paths outside usr/lib/modules/$rel" >&2; return 1
  fi
  tar -tvzf "$1" > "$2.long"   # a file, not a pipe: grep -q would end tar with SIGPIPE under pipefail
  if grep -Eq '^[lh]' "$2.long"; then echo "$1 contains links" >&2; return 1; fi
  echo "$rel"
}

# install_modules TARBALL RELEASE ROOT: extract into ROOT keeping the /lib -> usr/lib symlink (Debian 13);
# the 2026-11-26 update replaced it with a directory and no program could start (docs/debian/boot.md).
install_modules() {
  local root="${3%/}"
  [ -L "$root/lib" ] || { echo "$root/lib is not a symlink before the update – STOP" >&2; return 1; }
  local target; target="$(readlink "$root/lib")"
  (umask 022; tar -C "${root:-/}" --keep-directory-symlink --no-same-owner --no-same-permissions --no-overwrite-dir -xzf "$1")
  [ -L "$root/lib" ] && [ "$(readlink "$root/lib")" = "$target" ] \
    || { echo "$root/lib is no longer the symlink to $target – STOP, see docs/debian/boot.md" >&2; return 1; }
  [ -d "$root/usr/lib/modules/$2/kernel" ] || { echo "modules $2 missing after extraction" >&2; return 1; }
}

# read_slot DEV LEN OUT: read LEN bytes from the start of a slot.
read_slot() {
  nanddump -q -l "$2" -f "$3.raw" "$1"
  head -c "$2" "$3.raw" > "$3"; rm -f "$3.raw"
}
# write_slot DEV FILE: erase the 10 MiB slot, write FILE, read it back and compare.
write_slot() {
  local len; len="$(stat -c %s "$2")"
  flash_erase -q "$1" 0 "$SLOT_BLOCKS"
  nandwrite -q -p "$1" "$2"
  read_slot "$1" "$len" "$WORK/verify"
  cmp -s "$2" "$WORK/verify" || { echo "VERIFY FAILED on $1" >&2; return 1; }
  echo "  $1 verified: $(sha256sum < "$2" | cut -c1-16)…"
}

[ "${UPDATE_KERNEL_LIB:-}" = 1 ] && return 0

CHECK=0
[ "${1:-}" = --check ] && { CHECK=1; shift; }
[ $# -eq 2 ] || { echo "usage: $0 [--check] <uImage> <modules-<release>.tar.gz>" >&2; exit 2; }
IMG="$(realpath "$1")"; MODS="$(realpath "$2")"
[ "$(id -u)" -eq 0 ] || die "run as root"
grep -q '^mtd7: 00a00000 00020000 "kernel_2"$' /proc/mtd && grep -q '^mtd8: 02fc0000 00020000 "rootfs2"$' /proc/mtd \
  || die "unexpected /proc/mtd layout, refusing"

WORK="$(mktemp -d /root/kernel-update.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# New image: valid uImage, fits the slot, matches SHA256SUMS next to it when present.
NEW_LEN="$(uimage_len "$IMG")" || die "$IMG is not a uImage"
[ "$NEW_LEN" -le "$SLOT_BYTES" ] && [ "$(stat -c %s "$IMG")" -eq "$NEW_LEN" ] \
  || die "$IMG: size does not match its header or exceeds the slot"
if [ -f "$(dirname "$IMG")/SHA256SUMS" ]; then
  (cd "$(dirname "$IMG")" && sha256sum -c --ignore-missing --quiet SHA256SUMS) || die "SHA256SUMS mismatch"
fi
REL="$(modules_release "$MODS" "$WORK/mods.list")" || exit 1
case "$(uimage_name "$IMG")" in *"$REL"*) ;; *) die "uImage name '$(uimage_name "$IMG")' does not match modules $REL" ;; esac
[ "$REL" != "$(uname -r)" ] || die "modules $REL are those of the running kernel – build with another LOCALVERSION"
[ -L /lib ] || die "/lib is not a symlink – STOP, see docs/debian/boot.md"

# Current slots.
read_slot $SLOT_A 64 "$WORK/a.hdr"
A_LEN="$(uimage_len "$WORK/a.hdr")" || die "slot A holds no uImage – fix it by hand first"
read_slot $SLOT_A "$A_LEN" "$WORK/a.img"
read_slot $SLOT_B "$A_LEN" "$WORK/b.img"
echo "slot A: $(uimage_name "$WORK/a.img") ($A_LEN B), running $(uname -r)"
echo "slot B: $( (uimage_len "$WORK/b.img" >/dev/null && uimage_name "$WORK/b.img") || echo '(no uImage)')"
echo "new:    $(uimage_name "$IMG") ($NEW_LEN B), modules $REL"
if cmp -s "$WORK/a.img" "$IMG"; then echo "slot A already holds the new kernel, nothing to do"; exit 0; fi
B_SAME=0; cmp -s "$WORK/a.img" "$WORK/b.img" && B_SAME=1
if [ $B_SAME -eq 1 ]; then echo "slot B = slot A (kept as fallback)"; else echo "slot B differs: will copy A → B"; fi
if [ $CHECK -eq 1 ]; then echo "--check: nothing written"; exit 0; fi

if [ $B_SAME -eq 0 ]; then echo "writing slot B (copy of A)…"; write_slot $SLOT_B "$WORK/a.img"; fi
echo "installing modules $REL…"
install_modules "$MODS" "$REL" /
depmod -a "$REL"
echo "writing slot A (new kernel)…"
write_slot $SLOT_A "$IMG"
sync
echo "done: reboot to start $REL; fallback kernel in slot B, its modules in /usr/lib/modules/$(uname -r)"
