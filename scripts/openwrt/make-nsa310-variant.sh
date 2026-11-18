#!/bin/sh
# Derive an initramfs image for the ORIGINAL NSA310 from the official NSA310b one:
# only the fan controller node changes (LM85 -> ADT7476, as in Linux
# kirkwood-nsa310.dts). Use it for the temporary RAM boot (M3) if the read-only
# probe reports an ADT7476. Writes to $OUT_DIR only; nothing is flashed.
set -eu
. "$(dirname "$0")/env.sh"

src=$DL_DIR/kirkwood/openwrt-$OPENWRT_VERSION-kirkwood-generic-zyxel_nsa310b-initramfs-uImage
work=$OUT_DIR/nsa310-variant
mkdir -p "$work"

python3 "$(dirname "$0")/uimage.py" extract-dtb "$src" "$work/nsa310b.dtb"
tool dtc -q -I dtb -O dts -o "$work/nsa310b.dts" "$work/nsa310b.dtb"

# Exactly one fan node must change, otherwise the upstream DT moved and the
# patch has to be reviewed instead of applied blindly.
[ "$(grep -c 'compatible = "national,lm85";' "$work/nsa310b.dts")" = 1 ] || {
    echo "expected exactly one LM85 node in the NSA310b DT" >&2; exit 1; }
sed -e 's/compatible = "national,lm85";/compatible = "adi,adt7476";/' \
    -e 's/model = "Zyxel NSA310b";/model = "Zyxel NSA310 (ADT7476)";/' \
    "$work/nsa310b.dts" > "$work/nsa310.dts"
tool dtc -q -I dts -O dtb -o "$work/nsa310.dtb" "$work/nsa310.dts"

out=$OUT_DIR/openwrt-$OPENWRT_VERSION-kirkwood-nsa310-adt7476-initramfs-uImage
python3 "$(dirname "$0")/uimage.py" swap-dtb "$src" "$work/nsa310.dtb" "$out"
python3 "$(dirname "$0")/uimage.py" info "$out"
diff "$work/nsa310b.dts" "$work/nsa310.dts" || true
sha256sum "$out"
