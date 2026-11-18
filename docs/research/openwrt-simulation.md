# OpenWrt build and workstation simulation

Date: 2026-11-18. Release: OpenWrt **25.12.6** (r33311-981c978eff, Linux 6.12.112). Everything below ran on the Linux workstation only. **Nothing was run on the NAS**, nothing was flashed, and the read-only probe is still pending.

QEMU has no Kirkwood (88F6281) machine and OpenWrt has no ARMv5 virtual target, so the real board cannot be emulated as a whole. The simulation is split into layers. Each layer proves something specific, and whatever none of them can prove is left for the temporary RAM boot over UART (M3).

## How to reproduce

```sh
sh scripts/openwrt/fetch.sh               # download + GPG/SHA-256 verification
sh scripts/openwrt/build-image.sh         # Image Builder: NSA310b profile + firmware/openwrt
sh scripts/openwrt/make-nsa310-variant.sh # initramfs with the ADT7476 device tree
sh scripts/openwrt/sim-user.sh            # layer 2: ARMv5TE userspace smoke test
sh scripts/openwrt/sim-vm.sh              # layer 3: full system, storage scenario
python3 -m unittest discover -s tests     # includes uImage/DTB swap tests
```

Downloads, the Image Builder tree, images and VM disks live in `~/.cache/nas-openwrt-sim` (override with `NAS_SIM_DIR`), never in Git. The workstation had no root access, so QEMU 10.2.1, dtc and gawk were unpacked from the Ubuntu 26.04 packages (`apt-get download` + `dpkg -x`) into `$NAS_SIM_DIR/tools`. Nothing was installed system-wide. The scripts prefer system binaries if they are present.

Supply chain: `sha256sums` for each target must carry a valid signature by the OpenWrt Build System key `8A8BC12F46B836C0F9CDB36F1D53D1877742E911` (from `openwrt/keyring`), and every file is then checked against it. Packages that the Image Builder downloads are verified by apk with the keys bundled in the Image Builder.

## Layer 1: static image analysis

| Check | Result | Status |
| --- | --- | --- |
| Official NSA310b initramfs | Legacy uImage, uncompressed payload, load/entry 0x8000, zImage + **appended DTB** (zImage end = DTB offset) | Measured. Suits the stock U-Boot 1.1.4, which cannot pass a DT |
| Kernel config | `ARM_APPENDED_DTB`, `ARM_ATAG_DTB_COMPAT` and **`CMDLINE_FROM_BOOTLOADER`**; `MTD_CMDLINE_PARTS`; `KEXEC=y`; `R8169`, `SATA_MV`, `SENSORS_LM85` and `SENSORS_ADT7475` as modules | Read from the Image Builder kernel `.config` |
| Bootargs from the stock U-Boot | The kernel uses the stock command line (`console=ttyS0,115200 root=/dev/nfs init=/init mtdparts=nand_mtd:…`), not the DT `bootargs`. The stock mtd-id `nand_mtd` does not match the kernel's `orion_nand`, so cmdlinepart ignores the stock layout and the DT partitions apply. With an initramfs, `root=` is not used | Derived from config + strings. Unverified on hardware. In M3, set `bootargs` in RAM only (`setenv` without `saveenv`) |
| NSA310b DT vs Linux `kirkwood-nsa310.dts` | Differences: fan controller (**LM85 vs ADT7476** at i2c 0x2e), `mpp44` pinmux and LED pinctrl/labels. SoC, PCIe r8169, SATA, USB and NAND controller are the same | Source comparison. Which chip is on our board is unknown until the probe runs |
| OpenWrt NAND map | `uboot` 0–0xC0000 (read-only), `uboot_env` 0xC0000–0x140000, `ubi` 0x140000–end | From the DTB |
| Stock NAND map | `uboot` 0–0x100000, `uboot_env` 0x100000–0x180000, then key_store/info/etc/kernel/rootfs | From the private backup metadata |
| Overlap | OpenWrt `uboot_env` lies inside the stock U-Boot partition. In the private mtd0 copy the stock U-Boot ends at 0x7406A and 0xC0000–0x100000 is entirely 0xFF | Offline count only. `fw_setenv` under OpenWrt with the stock U-Boot would write to an area the stock U-Boot never reads, and `ubi` would overwrite the stock environment and all vendor partitions. **NAND installation requires its own plan (M7)** |
| Image size | Custom sysupgrade 7.7 MB against a 126.75 MiB UBI area | Measured |

## Layer 2: ARMv5TE userspace (qemu-user, `-cpu arm926`)

Kirkwood packages are `arm_xscale` (ARMv5TE, soft-float, musl). Binaries from the built rootfs ran under QEMU restricted to an ARM926 core, which has the Feroceon instruction set. None hit an illegal instruction: busybox (`armv5tel`), mdadm 4.3, smartctl 7.5, ucode, dropbear 2025.89, apk 3.0.5, kexec-tools 2.0.28, mkfs.ext4/e2fsck 1.47.3 (filesystem created and checked on a 256 MB image). This covers instruction-set compatibility for userspace only. It does not cover the kernel or drivers.

## Layer 3: full system (armsr/armv7, same release)

QEMU `virt`, one CPU, **256 MB RAM** (247260 kB visible, like the NAS's 247952 kB), virtio NIC with user-mode NAT and no listener on the workstation, and a sparse 3 TB disk standing in for the Seagate. The disk is set up as on the stock NAS: a single-member RAID1 `md0` with ext4. On top run ksmbd (SMB), block-mount and mdadm, plus a clean reboot and a power cut (QEMU killed).

Results (scenario `scripts/openwrt/scenarios/storage.sh`):

- Boot to console 9–10 s, all init scripts done after about 33 s (emulated, not representative of the NAS).
- Memory with the services running: about 18 MB used, about 190 MB available of 247 MB. The RAM budget is not a problem for this stack.
- After a reboot: `md0` assembled, data mounted, SHA-256 of the 64 MB test file intact, SMB listening on 445.
- After a power cut: data mounted and intact after the ext4 journal replay. A 32 MB file written without `sync` was lost, as expected.

### Defects found and fixed in `firmware/openwrt/files`

1. **Data array never mounted at boot.** `fstab` (START=11) runs `block mount` before `mdadm` (START=13) assembles the array. The share is then served from an empty directory on the internal overlay, which on the NAS means NAND fills up and wears. Fix: `etc/init.d/nas-storage` (START=14) runs `block mount` once the arrays exist. Without it the reboot test showed `md0` active but not mounted. With it, mounted.
2. **Array not assembled at all.** The OpenWrt `mdadm` package ships an example `config array` with a fixed UUID on `/dev/md0`. Next to the real array, mdadm aborts with "Duplicate MD device names". Fix: `etc/uci-defaults/90-nas-mdadm` removes that example once. Reproduced before the fix, passed after it.

### Design consequences (not yet implemented)

- The web UI must never let a share point at an unmounted path. The backend should refuse to start SMB/SFTP for a share whose mount is missing.
- Configuration changes must be flushed (`sync`) before reporting success. In the power-cut test, unsynced files on the overlay were lost.
- Arrays and filesystems are referenced by UUID. Device names (`md0` vs `md127`) changed during the tests.

## What remains unproven until M3 (real hardware)

Kernel 6.12 boot on this board via the stock U-Boot (memory, appended DTB, ATAG handoff), r8169 link, sata_mv with the real disk, fan chip identity and behavior, LEDs/buttons, NAND/UBI handling, real CPU/TLS/SMB throughput and temperatures. The ADT7476 initramfs variant (`make-nsa310-variant.sh`) changes only the fan node. It is meant for the case where the probe finds an ADT7476, so that the LM85 driver does not bind to the wrong chip. Until then the fans stay under the chip's power-on defaults, which still has to be checked on the hardware.
