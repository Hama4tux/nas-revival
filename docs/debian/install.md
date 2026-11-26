# Install Debian 13 on a stock ZyXEL NSA310

This is the procedure that turned a stock NSA310 into the NAS Revival system on 2026-11-25:
our own Linux 6.12 LTS kernel in two NAND slots, Debian 13 armel on the internal disk, and the
original ZyXEL firmware kept as the last fallback. Technical background: [boot.md](boot.md),
what to do when something breaks: [RECOVERY.md](../RECOVERY.md).

> [!WARNING]
> You are writing to the NAND flash and the bootloader environment of your device. Nothing here
> touches U-Boot itself, and the stock firmware stays bootable, but a mistake can still leave the NAS
> needing the serial console to recover. **Do not start without a working serial console.**
> No warranty – see [LICENSE](../../LICENSE).

> [!NOTE]
> Verified on one **NSA310 (88F6281 A1, RTL8111D, LM85, 256 MB RAM, stock U-Boot 1.1.4)**.
> The NSA310a device tree is used. NSA310b/NSA310S/NSA320 have different hardware – do not use this
> guide on them without checking first.

## What you need

| Item | Notes |
| --- | --- |
| USB–TTL serial adapter, **3.3 V** | e.g. CP2102. Never a 5 V or RS-232 adapter. |
| USB stick | any size; holds the kernel for the first boot and later serves as a rescue stick |
| SATA disk | internal; becomes system (30 GB) + swap (1 GB) + data. **It will be wiped.** |
| Linux workstation (x86) | `gcc-arm-linux-gnueabi`, `make bc bison flex libssl-dev`, `u-boot-tools`, `mmdebstrap`, `qemu-user-static` + `binfmt-support`, `gdisk`, `python3-serial` |
| Time | ~1 hour build, ~30 min on the device |

## Overview

```
 1 backup ─▶ 2 build ─▶ 3 disk + stick ─▶ 4 RAM-only boot ─▶ 5 write slots ─▶ 6 boot chain ─▶ 7 web UI
  (stock)   (workstation)  (workstation)    (console, no writes)  (from Debian)     (one saveenv)
```

Until step 5 nothing on the NAS is changed. Steps 5 and 6 are the only writes, and each one has a
way back.

## 1. Serial console and backup

1. Unplug the NAS, open the case. The console header is **J2** next to the CPU. On our board
   **pin 2 is the NAS TX**. Identify GND (continuity to the case/USB shield) and RX with a
   multimeter on your own board. Connect GND–GND, NAS TX → adapter RXD, NAS RX → adapter TXD.
   **Leave the 3.3 V pin unconnected.**
2. Open the console at **115200 8N1** (`picocom -b 115200 /dev/ttyUSB0`, `screen`, …) and power on.
   You will see `U-Boot 1.1.4` and then the stock firmware boot. Press a key during the 2 s
   countdown to get the `NSA310>>` prompt.
3. Save the U-Boot environment – copy the whole output of `printenv` into a file and keep it.
   You need `bootargs`, `kernel_addr` and `ethaddr` (the factory MAC) later.
4. Back up the NAND. Boot the stock firmware (`boot`); it gives a root shell on the console.
   Plug in a USB stick and dump every partition **with OOB** (`cat /proc/mtd` lists 9 of them):
   ```sh
   for n in 0 1 2 3 4 5 6 7 8; do nanddump -n -o -f /path/to/usb/mtd$n.raw /dev/mtd$n; done
   sha256sum /path/to/usb/mtd*.raw > /path/to/usb/SHA256SUMS
   ```
   If the stock image has no `nanddump`, at least copy `/dev/mtd0`–`mtd8` with `dd`. Keep the dumps
   private: they contain your MAC address, serial numbers and the vendor firmware.

## 2. Build kernel and root file system (workstation)

```sh
git clone https://github.com/Hama4tux/nas-revival && cd nas-revival
scripts/debian/build-kernel.sh                       # → output/kernel/uImage, modules-6.12.119.tar.gz
```

On Ubuntu (or any non-Debian host) download `debian-archive-keyring_*_all.deb` from
[packages.debian.org](https://packages.debian.org/trixie/debian-archive-keyring) and extract it:
`dpkg-deb -x debian-archive-keyring_*.deb kr`. Then:

```sh
KEYRING=kr/usr/share/keyrings/debian-archive-keyring.gpg \
NAS_MAC=<ethaddr from step 1.3> \
ROOT_PW_HASH="$(openssl passwd -6)" \
scripts/debian/build-rootfs.sh output/kernel/modules-6.12.119.tar.gz ~/.ssh/id_ed25519.pub
                                                     # → output/rootfs/rootfs-trixie-armel.tar
```

The root file system contains: systemd-networkd (DHCP), resolved, timesyncd, OpenSSH (key only,
no password logins), Realtek NIC firmware, smartmontools, hdparm, mtd-utils, automatic fan
control (`nsa310-fan`) and front LEDs (`nsa310-leds`). The root password from `ROOT_PW_HASH` is
for the serial console only.

## 3. Prepare the disk and the USB stick (workstation)

Connect the SATA disk to the workstation (USB–SATA adapter). **Check the device name twice.**

```sh
D=/dev/sdX                                            # the NAS disk – will be wiped
sudo sgdisk -Z $D
sudo sgdisk -n1:0:+30G -t1:8300 -c1:nas-root \
            -n2:0:+1G  -t2:8200 -c2:nas-swap \
            -n3:0:0    -t3:8300 -c3:nas-data $D
sudo mkfs.ext4 -L nasroot ${D}1 && sudo mkswap -L nasswap ${D}2 && sudo mkfs.ext4 -L nasdata ${D}3
sudo mount ${D}1 /mnt && sudo tar -C /mnt -xpf output/rootfs/rootfs-trixie-armel.tar && sudo umount /mnt
sudo blkid -s PARTUUID -o value ${D}1                  # note it: the root PARTUUID
```

The USB stick needs an **MBR (dos)** partition table – the stock U-Boot does not read GPT – and
an ext2 first partition with the kernel:

```sh
S=/dev/sdY                                            # the USB stick – will be wiped
echo 'start=2048, size=512MiB, type=83' | sudo sfdisk --label dos $S
sudo mkfs.ext2 -L boot ${S}1
sudo mount ${S}1 /mnt && sudo cp output/kernel/uImage /mnt/uImage && sudo umount /mnt
```

Put the disk back into the NAS, plug the stick into the rear USB port.

## 4. First boot from the stick – RAM only

Nothing is written. At the `NSA310>>` prompt:

```
usb start
ext2load usb 0:1 0x2000000 /uImage
setenv bootargs console=ttyS0,115200 root=PARTUUID=<root PARTUUID> rootwait rw
bootm 0x2000000
```

(`scripts/debian/uboot-boot.py <PARTUUID>` does the same automatically: start it, then power on.)

Debian boots to a login prompt. Check: `ip a` (network up, factory MAC), `lsblk` (disk),
`cat /proc/mtd` (9 NAND partitions). Then log in over SSH: `ssh root@<address from your router>`.
If anything fails, power off – the next power-on boots the stock firmware as before.

## 5. Write the kernel to both NAND slots (from Debian)

Slot A is `kernel_2` (`/dev/mtd7`), slot B is the first 10 MB of `rootfs2` (`/dev/mtd8`, the stock
image's unused second firmware). The stock firmware in `kernel_1`/`rootfs1` stays untouched.
Write one slot, verify it, then the other:

```sh
cp /boot/uImage /root/uImage                          # mounted from the stick (LABEL=boot)
S=$(stat -c %s /root/uImage)
for mtd in /dev/mtd7 /dev/mtd8; do
  flash_erase $mtd 0 80                               # 80 × 128 KiB = 10 MiB
  nandwrite -p $mtd /root/uImage
  nanddump -l $S -f /root/check $mtd
  head -c $S /root/check | sha256sum; sha256sum /root/uImage   # must be identical
done
```

`nanddump` must report no ECC failures. The kernel and U-Boot use the same 1-bit Hamming ECC, so
U-Boot reads what Linux wrote (verified byte by byte, see [boot.md](boot.md)).

## 6. The boot chain – one `saveenv`

Reboot, stop autoboot on the console and enter these lines **one by one** (the stock U-Boot
truncates long lines; single quotes keep `;` and `$(…)` for later):

```
setenv stockargs '<your original bootargs from step 1.3>'
setenv stockboot 'nand read.e 0x2000000 $(kernel_addr) 0xA00000; bootm 0x2000000'
setenv slotA 'mw.l 0x2000000 0 0x100; nand read.e 0x2000000 0x4640000 0xA00000; bootm 0x2000000'
setenv slotB 'mw.l 0x2000000 0 0x100; nand read.e 0x2000000 0x5040000 0xA00000; bootm 0x2000000'
setenv debargs 'console=ttyS0,115200 root=PARTUUID=<root PARTUUID> rootwait rw'
setenv bootdeb 'setenv bootargs $(debargs); run slotA; run slotB; setenv bootargs $(stockargs); run stockboot'
printenv
```

Check `printenv` carefully, then first test without saving: `run bootdeb`. When Debian comes up,
reboot, stop autoboot again, re-enter the lines (they were in RAM only) and make it permanent:

```
setenv bootcmd 'run bootdeb'
saveenv
reset
```

The NAS now boots slot A; if that image is unreadable it boots slot B, and if both are unreadable,
the stock firmware. Never run `nand erase` without offset and size – it erases the whole chip.

**Undo:** `setenv bootcmd 'run stockboot'; saveenv` returns to the stock boot.

## 7. Web administration

On the workstation `scripts/webui/build.sh` builds a single static ARMv5 binary with the UI
embedded. Install it with the steps in [deploy/debian/README.md](../../deploy/debian/README.md)
and open `https://<nas>:8443`. In the web UI you can then set a static address (with automatic
rollback if you lose the connection), NTP, certificate, disk power and users.

## Known limits

- A kernel that loads but **hangs** is not caught by the slot chain – keep the console at hand
  when you put a new kernel into slot A (boot-once is planned, see [OPEN.md](../../OPEN.md)).
- Without the disk the kernel waits for its root forever (`rootwait`).
- The NSA310 does not power on by itself after a power cut – press the power button.
- Debian 13 is the last release for armel; the kernel is maintained by this project.
