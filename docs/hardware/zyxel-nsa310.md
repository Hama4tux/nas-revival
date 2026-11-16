# ZyXEL NSA310: evidence and unknowns

Initial snapshot and subsequent discovery review: 2026-11-16. This agent performed local offline analysis only. **Source declarations, owner-verified measurements and local backup observations are distinguished below.**

## Bootstrap owner-reported state (historical)

The owner identifies the unit as the original NSA310 and reports vendor firmware V4.70(AFK.1), a working legacy HTTPS administration interface, unavailable SSH/Telnet and a physically installed SATA disk. JBOD initialization was started; completion and data condition are unknown. These reports have not been independently verified. Private network identifiers are deliberately omitted.

## Bootstrap evidence matrix (historical; superseded where stated below)

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

The [device manifest](../../devices/zyxel-nsa310/hardware.yaml) remains an inactive bootstrap profile; the discovery update below does not qualify it for deployment. Do not enter private serial numbers, MACs, raw environment exports or firmware keys into public history.

## Hardware discovery update — 2026-11-16

The owner supplied these verified observations after bootstrap. This agent did not connect to the NAS to repeat the measurements.

| Property | Owner-verified observation |
| --- | --- |
| Model | ZyXEL NSA310, model ID A203 |
| CPU / architecture | Marvell Feroceon 88FR131 / ARMv5TE; exact SoC part, PCB revision and clock remain unresolved |
| RAM | 256 MB physical; 247952 kB reported to the kernel, not a current free-memory measurement |
| NAND | 128 MiB; erase block 131072 bytes; page 2048 bytes; OOB 64 bytes per page |
| Software | Linux 2.6.31.8, BusyBox 1.17.2, firmware V4.70(AFK.1) |
| SATA | Seagate ST3000VX000-9YW1, 3 TB; ext4 on `/dev/md0`; member layout and disk health are not inferred |
| Access | Working root shell through **Telnet**, SMB and vendor web administration; no UART adapter available |
| Current boot | Working NAND boot; offline bootloader identification below; alternative boot untested |

Access credentials are kept in the project's private, Git-ignored `secret.env`. This task did not read or use it. A Linux Telnet shell is not a U-Boot console; Telnet is not a production security recommendation.

## Local backup evidence

Private sibling directory: `../nas-revival-backup/`. It contains `mtd0.raw`–`mtd8.raw`, SHA256SUMS and layout/environment/kernel metadata. The owner reports all nine hashes passed. This task independently rechecked **mtd0 only** before/after analysis and checked all nine file lengths without analyzing the other dumps. No binary or full metadata export is committed.

```text
mtd0: 00100000 00020000 "uboot"
mtd1: 00080000 00020000 "uboot_env"
mtd2: 00080000 00020000 "key_store"
mtd3: 00080000 00020000 "info"
mtd4: 00a00000 00020000 "etc"
mtd5: 00a00000 00020000 "kernel_1"
mtd6: 02fc0000 00020000 "rootfs1"
mtd7: 00a00000 00020000 "kernel_2"
mtd8: 02fc0000 00020000 "rootfs2"
```

Sizes total 134217728 bytes. `mtd0.raw` has 1081344 bytes: 512 interleaved records of 2048 data + 64 OOB bytes. A separate private data-only analysis copy has 1048576 bytes. OOB removal is not ECC correction and the derived file is not a restore image.

Offline banner: **U-Boot 1.1.4 (Feb 22 2011 - 10:31:35), Marvell version 3.4.19**. Compiled USB/FAT/ext2/legacy-image paths are present; functional USB boot and access to the pre-OS console without UART remain unverified. See [U-Boot analysis](../research/uboot-analysis.md).

### Address discrepancy

The saved environment has `kernel_addr=480000` in a `nand read.e` command. Interpreted as hexadecimal, this is `0x480000`. The saved kernel `mtdparts` explicitly lists contiguous partitions of the sizes above, placing `kernel_1` at `0xC80000` and `etc` at `0x280000`–`0xC80000`. These views disagree. Do not silently correct either, assume the export is active, or use either as a boot/flash instruction. Model ID and CPU core name do not identify the fan-controller/PCB variant.
