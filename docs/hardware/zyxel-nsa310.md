# ZyXEL NSA310: evidence and unknowns

Reviewed 2026-11-16. No live device access occurred. **Source-verified means a statement in upstream code/documentation; it does not mean measured on this NAS.**

## Owner-reported state

The owner identifies the unit as the original NSA310 and reports vendor firmware V4.70(AFK.1), a working legacy HTTPS administration interface, unavailable SSH/Telnet and a physically installed SATA disk. JBOD initialization was started; completion and data condition are unknown. These reports have not been independently verified. Private network identifiers are deliberately omitted.

## Evidence matrix

| Property | Reference evidence / expected value | Status on this unit | How to qualify later |
| --- | --- | --- | --- |
| Model/revision | NSA310, distinct DT variants exist | Owner-reported; exact PCB unknown | Redacted label/PCB evidence and boot log |
| CPU | Kirkwood 88F6281 compatible in Linux DT; ARMv5 candidate | SoC identity and approximately 1.2 GHz unmeasured | Boot banner, CPU information, board marking |
| RAM | DT memory region `0x10000000` = 256 MiB | Upstream declaration only | Boot memory report plus kernel reserved-memory accounting |
| NAND | Approximately 128 MiB in project brief; reference partition span reaches that size | Geometry, ECC, OOB and bad blocks unknown | Read-only MTD/sysfs inventory and qualified dump |
| SATA | Common DT enables two controller ports; project target has one internal bay | One inserted disk reported; controller/disk untested | Inventory without mount/repair; distinguish eSATA from internal bay |
| Ethernet | OpenWrt NSA310b profile includes r8169 | Exact controller/PHY and negotiated rate unknown | Boot enumeration and later read-only driver/link information |
| USB | Kirkwood target includes USB support; reference DT has USB power control | Physical ports and stock USB boot untested | Board/manual evidence, later authorized enumeration |
| LED/buttons/power | Common DT defines GPIO keys/poweroff; board DT defines LEDs | Mapping/polarity and shutdown behavior untested | Profile-specific controlled tests |
| Fan/temperature | NSA310 DT: ADT7476; NSA310a and OpenWrt NSA310b: LM85 | Installed chip and channels unknown | Board identity and read-only bound-driver evidence |
| Bootloader/UART | U-Boot expected; DT console uses ttyS0 at 115200 | Version, commands, wiring and voltage unverified | Qualified UART capture in a future approved phase |
| SMART | Desired via disk/controller support | Unknown | Later read-only SMART capability query, no self-test initially |

Primary references: [Linux NSA310 DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310.dts), [NSA310a DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310a.dts), [common DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa3x0-common.dtsi), [OpenWrt profile](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/image/Makefile), [OpenWrt board DT](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/files/arch/arm/boot/dts/marvell/kirkwood-nsa310b.dts). Source availability and vendor/community limitations are recorded in the [research log](../research/upstream-support.md).

## NAND incompatibility warning

OpenWrt 25.12.6 patches the common DT to change the bootloader/environment boundaries and replace vendor partitions with UBI. The Linux reference layout is therefore **not** the installed OpenWrt layout and neither proves the stock firmware's current map. Do not use offsets from either source for backup/restore on this unit. [Partition patch](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/patches-6.12/107-01-zyxel-nsa3x0-common-nand-partitions.patch).

The [device manifest](../../devices/zyxel-nsa310/hardware.yaml) intentionally leaves measured values null and active profile selection disabled. Do not enter private serial numbers, MACs, raw environment exports or firmware keys into public history.
