#!/bin/bash
# Tests for scripts/debian/update-kernel.sh module handling against a fake root with Debian 13's
# /lib -> usr/lib symlink (the 2026-11-26 update replaced it by a directory). Run: bash tests/test_update_kernel.sh
set -uo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
UPDATE_KERNEL_LIB=1 . "$here/scripts/debian/update-kernel.sh"
set +e
fail=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }

# A modroot as modules_install leaves it (lib/modules/<rel>/…), packed like build-kernel.sh.
modroot() {
  rm -rf "$work/modroot"; mkdir -p "$work/modroot/lib/modules/$1/kernel/net"
  echo new > "$work/modroot/lib/modules/$1/kernel/net/nf_tables.ko"
  echo dep > "$work/modroot/lib/modules/$1/modules.dep"
}
pack_new() { tar -C "$work/modroot" --owner=0 --group=0 --numeric-owner --transform "s,^lib,usr/lib," -czf "$1" lib; }
pack_old() { tar -C "$work/modroot" -czf "$1" lib; }   # format of the tarball that broke /lib
fakeroot_() {
  rm -rf "$work/root"; mkdir -p "$work/root/usr/lib/modules/6.12.119/kernel"
  ln -s usr/lib "$work/root/lib"
  echo loader > "$work/root/usr/lib/ld-linux.so.3"
  echo old > "$work/root/usr/lib/modules/6.12.119/kernel/old.ko"
}

modroot 6.12.119-nr1; pack_new "$work/new.tgz"
check "release of a new-style tarball" "$(modules_release "$work/new.tgz" "$work/l")" "6.12.119-nr1"

fakeroot_
install_modules "$work/new.tgz" 6.12.119-nr1 "$work/root"; check "install exit" "$?" "0"
check "lib still a symlink"        "$(readlink "$work/root/lib")" "usr/lib"
check "loader still reachable"     "$(cat "$work/root/lib/ld-linux.so.3")" "loader"
check "modules of slot-B kernel"   "$(cat "$work/root/usr/lib/modules/6.12.119/kernel/old.ko")" "old"
check "new modules installed"      "$(cat "$work/root/lib/modules/6.12.119-nr1/kernel/net/nf_tables.ko")" "new"

pack_old "$work/old.tgz"
check "old lib/ tarball refused" "$(modules_release "$work/old.tgz" "$work/l" 2>/dev/null; echo "rc=$?")" "rc=1"
fakeroot_
install_modules "$work/old.tgz" 6.12.119-nr1 "$work/root" 2>/dev/null
check "even an old lib/ tarball keeps the symlink" "$(readlink "$work/root/lib")" "usr/lib"

modroot 6.12.119-nr1; ln -s /home/someone/linux "$work/modroot/lib/modules/6.12.119-nr1/build"; pack_new "$work/link.tgz"
check "tarball with a link refused" "$(modules_release "$work/link.tgz" "$work/l" 2>/dev/null; echo "rc=$?")" "rc=1"

modroot 6.12.119-nr1; mkdir -p "$work/modroot/lib/modules/6.12.119"; echo x > "$work/modroot/lib/modules/6.12.119/x"
pack_new "$work/two.tgz"
check "tarball with two releases refused" "$(modules_release "$work/two.tgz" "$work/l" 2>/dev/null; echo "rc=$?")" "rc=1"

fakeroot_; rm "$work/root/lib"; mkdir "$work/root/lib"
check "install refused when lib is already a directory" \
  "$(install_modules "$work/new.tgz" 6.12.119-nr1 "$work/root" 2>/dev/null; echo "rc=$?")" "rc=1"

printf 'HDR' > "$work/bad.img"
check "non-uImage refused" "$(uimage_len "$work/bad.img"; echo "rc=$?")" "rc=1"
printf '\x27\x05\x19\x56%08d\x00\x00\x01\x00' 0 > "$work/hdr.img"
check "uImage length from header" "$(uimage_len "$work/hdr.img")" "320"

exit $fail
