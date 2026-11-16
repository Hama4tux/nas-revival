# Firmware integration

No images, bootloader binaries, build configs or flash scripts exist in phase 1. Follow [ADR-0001](../docs/adr/0001-base-distribution.md).

After qualification, keep small OpenWrt packages/overlays under `openwrt/` with exact source/feed/toolchain locks. If Buildroot is selected instead, use a `buildroot/` br2-external tree. Device-specific kernel/DT changes belong to reviewed per-board integration; generic application modules must not encode GPIO/NAND offsets.

Never copy stock dumps or compile with invented flash geometry. Building an image is not authorization to deploy it.
