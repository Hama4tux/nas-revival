# Firmware integration

No images, bootloader binaries or flash scripts are kept in Git. Follow [ADR-0001](../docs/adr/0001-base-distribution.md).

`openwrt/packages.txt` and the `openwrt/files/` overlay feed the OpenWrt 25.12.6 Image Builder test build (`scripts/openwrt/build-image.sh`); images are written outside the repository. Each overlay file states the simulation finding it fixes, see [OpenWrt simulation](../docs/research/openwrt-simulation.md).

After qualification, keep small OpenWrt packages/overlays under `openwrt/` with exact source/feed/toolchain locks. If Buildroot is selected instead, use a `buildroot/` br2-external tree. Device-specific kernel/DT changes belong to reviewed per-board integration; generic application modules must not encode GPIO/NAND offsets.

Never copy stock dumps or compile with invented flash geometry. Building an image is not authorization to deploy it.
