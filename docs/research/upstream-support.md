# Upstream support review

Review date: **2026-11-16**. This is source research, not hardware testing. Release tags are reference snapshots, not a build lock or an endorsement of their entire security state. No firmware was downloaded or built.

## Verified primary evidence

| ID | Source inspected | Finding and limit |
| --- | --- | --- |
| S1 | [OpenWrt 25.12.6 Kirkwood download index](https://downloads.openwrt.org/releases/25.12.6/targets/kirkwood/generic/) | Publishes NSA310b and NSA310S artifacts, including initramfs; availability does not qualify this original NSA310 or its stock bootloader |
| S2 | [Release image definitions](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/image/Makefile) | `zyxel_nsa310b` and `zyxel_nsa310s` are separate; b profile selects SATA, ext4, r8169, lm85 and button modules |
| S3 | [Kirkwood target definition](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/Makefile) | ARM, `CPU_TYPE=xscale`, kernel series 6.12, USB/NAND/ramdisk features; xscale here is a build target name, not an assertion that the board CPU is Intel XScale |
| S4 | [OpenWrt NSA310b DT](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/files/arch/arm/boot/dts/marvell/kirkwood-nsa310b.dts) | LM85 and red/green USB LED variant, 256 MiB reference RAM, `zyxel,nsa310b` with NSA310 fallback compatible |
| S5 | [Linux 6.12 NSA310 DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310.dts) and [NSA310a DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310a.dts) | Distinct ADT7476/LM85 descriptions; Linux board support exists independently of distribution image availability |
| S6 | [Linux common DT](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa3x0-common.dtsi) and [OpenWrt partition patch](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/patches-6.12/107-01-zyxel-nsa3x0-common-nand-partitions.patch) | SATA, UART, USB power, buttons, poweroff and reference NAND declarations; OpenWrt changes the partition map |
| S7 | [OpenWrt U-Boot package](https://github.com/openwrt/openwrt/blob/v25.12.6/package/boot/uboot-kirkwood/Makefile) | NSA310 and NSA310S bootloader targets are separate; package uses upstream 2020.04 plus downstream integration; neither package presence nor version proves safe replacement or absence of vulnerabilities |
| S8 | [Debian 13 ARMEL limitations](https://www.debian.org/releases/trixie/release-notes/issues.html#last-release-for-armel) | Last ARMEL release; no ARMEL installer, packaged kernels limited to listed Raspberry Pi models; a Kirkwood experiment needs a compatible separately maintained kernel |
| S9 | [Debian 13 release/support information](https://www.debian.org/releases/trixie/) | General support to 2028 and LTS to 2030 are release-wide statements; LTS architecture coverage is reduced, so do not promise ARMEL support through 2030 |
| S10 | [Buildroot ARM configuration](https://github.com/buildroot/buildroot/blob/2025.02.19/arch/Config.in.arm) and [release page](https://buildroot.org/download.html) | ARMv5/EABI/soft-float configuration exists; release page lists 2025.02.19 LTS and 2026.08.1 stable at review time; no NSA310 BSP was qualified |
| S11 | [Buildroot manual](https://buildroot.org/downloads/manual/manual.html) | Cross-build system and br2-external customization, not a target package update service; NAS Revival would own product releases/security maintenance |
| S12 | [uhttpd](https://github.com/openwrt/openwrt/blob/v25.12.6/package/network/services/uhttpd/Makefile), [rpcd](https://github.com/openwrt/openwrt/blob/v25.12.6/package/system/rpcd/Makefile), [ucode upstream](https://github.com/ucode-lang/ucode/blob/master/README.md) | Existing small HTTP/RPC/scripting integration supports a backend experiment; size, isolation and authentication still require design and measurement |
| S13 | [Linux ADT7476 support](https://docs.kernel.org/hwmon/adt7475.html) and [LM85 support](https://docs.kernel.org/hwmon/lm85.html) | Existing sensor/fan drivers; presence of driver support does not identify which chip or channels are installed |
| S14 | [U-Boot printenv](https://docs.u-boot.org/en/latest/usage/cmd/printenv.html) and [bootm](https://docs.u-boot.org/en/latest/usage/cmd/bootm.html) | Environment inspection and boot format reference; modern documentation does not establish commands supported by the vendor U-Boot |
| S15 | [Linux NAND documentation](https://docs.kernel.org/driver-api/mtdnand.html) | ECC and bad-block handling matter; a logical data copy alone is not a universal raw NAND recovery image |

The inspected GitHub source contents have dates and SHA-256 digests in [source-evidence.json](source-evidence.json). This records research provenance, not authenticated release downloads. HTML indexes were inspected online; no image checksum or signature was verified.

## Hardware coverage assessment

SATA/USB and board peripherals have upstream descriptions, and the OpenWrt profile names relevant modules. Ethernet must be identified: r8169 in the b profile is evidence for that integration, not grounds to choose a Marvell Ethernet driver from the SoC name. SMART, poweroff, fan safety, USB boot, disk detection and sustained network transfer all remain untested on the owned unit. Confirm selected kernel configuration as well as DTS.

## Historical and incomplete references

- [OpenWrt NSA310b wiki](https://openwrt.org/toh/zyxel/nsa310b) was blocked by anti-bot protection. We used primary release/source files instead; do not claim the wiki instructions were validated.
- [OpenWrt NSA3xx community guide](https://openwrt.org/toh/zyxel/nsa3xx) appeared in search results with historical 21.02.1 bootloader guidance. Its instructions are not accepted as current or safe and were not executed.
- [Doozan Kirkwood kernel thread](https://forum.doozan.com/read.php?2,12096) and [U-Boot thread](https://forum.doozan.com/read.php?3,12381) are candidate maintainer/community references. Direct fetch returned 403; current archives, signatures and board coverage remain unverified. Do not reuse installation commands from them without a new review.
- The attempted [ZyXEL download archive](https://download.zyxel.com/NSA310/datasheet/NSA310_5.pdf) was inaccessible; that guessed filename is not an established canonical document. A [manufacturer-authored datasheet mirrored by a reseller](https://www.zyxelguard.com/datasheets/Storage/NSA310.pdf) appeared in indexed results, but the full PDF could not be fetched. Treat its one-bay/Gigabit/USB/eSATA claims as leads, not a complete primary-document verification. Obtain the original manual/datasheet before pinout work.

## Interpretation

The evidence supports an OpenWrt-first feasibility study. It does not support flashing a generic NSA310b image to every NSA310. Variant differences, altered NAND layout, stock U-Boot compatibility and current security lifecycle are blocking qualification questions. Debian's architecture sunset is a separate strategic constraint; Buildroot preserves configuration control while transferring more maintenance work to this project.
