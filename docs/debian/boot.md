# Debian 13 on the NSA310: kernel in NAND slots, system on the internal disk

Status: in use since 2026-11-25 – the NAS boots Debian unattended. The only NAND writes were
`kernel_2`, the first 10 MB of `rootfs2` and one `saveenv`; U-Boot itself (`uboot`), `kernel_1`
and `rootfs1` (stock firmware) are untouched. The serial adapter (CP2102) is attached to the
separate Live Ubuntu laptop, not to the dev VM.

## Layout

| Where | Offset / FS | Content |
|---|---|---|
| NAND `kernel_1` + `rootfs1` | 0xC80000 / 0x1680000 | stock ZyXEL firmware – last-resort fallback |
| NAND `kernel_2` | 0x4640000, 10 MB | **slot A**: current Debian kernel (uImage + appended `kirkwood-nsa310a.dtb`) |
| NAND `rootfs2` start | 0x5040000, 10 MB | **slot B**: previous known-good kernel |
| disk part 1 (30 GB) | ext4 `nasroot` | Debian 13 armel root |
| disk part 2 (1 GB) | swap `nasswap` | swap (256 MB RAM) |
| disk part 3 (rest) | ext4 `nasdata` | `/srv/data` (`nofail`) |
| USB stick (optional) | ext2 `boot` / ext4 `rootfs` | rescue only: kernels and an old frozen root copy; `/boot` is mounted with `nofail,x-systemd.device-timeout=10s` so the NAS boots without the stick |

NAND has one factory bad block at 0x2640000 (inside `rootfs1`); `nand read.e`/`write.e` skip bad blocks.

## Boot chain (saved U-Boot environment)

```
bootcmd   = run bootdeb
bootdeb   = setenv bootargs $(debargs); run slotA; run slotB; setenv bootargs $(stockargs); run stockboot
slotA     = mw.l 0x2000000 0 0x100; nand read.e 0x2000000 0x4640000 0xA00000; bootm 0x2000000
slotB     = mw.l 0x2000000 0 0x100; nand read.e 0x2000000 0x5040000 0xA00000; bootm 0x2000000
stockboot = nand read.e 0x2000000 $(kernel_addr) 0xA00000; bootm 0x2000000
debargs   = console=ttyS0,115200 root=PARTUUID=<nasroot PARTUUID> rootwait rw
stockargs = <original bootargs, unchanged>
```

Verified on the console: `bootm` on an invalid image prints "Bad Magic Number" (or a CRC error)
and returns, so the chain continues; the RAM header is cleared before each read so a stale image
from a warm reset is never booted. `kernel_addr` is overridden at runtime by the ZyXEL U-Boot
(stored 480000, effective 0xc80000) and was saved unchanged.

Stock U-Boot 1.1.4 notes: single-quoted values keep `;` and `$(…)` literally; console lines that
are too long are truncated ("Too many args"), so keep each `setenv` short; `nand erase` without
arguments erases the whole chip – always pass offset and size.

## Writing a kernel to a slot (from U-Boot)

```
usb start
ext2load usb 0:1 0x2000000 /uImage
iminfo 0x2000000
nand erase 0x4640000 0xA00000
nand write.e 0x2000000 0x4640000 0xA00000
```

Read back with `mw.l 0x2000000 0 0x100; nand read.e …; iminfo 0x2000000`.

## Writing a kernel to a slot (from Linux)

Qualified 2026-11-25. The kernel uses soft Hamming ECC (1 bit per 256 B, `eccpos` 40–63, non-SMC
byte order, `CONFIG_MTD_NAND_ECC_SW_HAMMING_SMC` unset), which is exactly what the stock U-Boot
`nandEcc=1bit` writes: all 128 pages of a U-Boot-written slot matched (`scripts/debian/ecc-check.py`
on a `nanddump -n -o` dump), Linux read both slots with 0 corrected / 0 failed bits, and erasing and
rewriting slot B from Linux produced a raw NAND image (data + OOB) identical to the one U-Boot wrote.

```
flash_erase /dev/mtd8 0 80            # slot B = first 10 MB of rootfs2 (80 × 128 KiB)
nandwrite -p /dev/mtd8 uImage
nanddump -l <size> -f check /dev/mtd8 # compare sha256 with uImage; check corrected_bits/ecc_failures
```

Slot A is `/dev/mtd7` (`kernel_2`). Only ever write one slot at a time and verify it before
touching the other one.

## Console access and recovery

`scripts/debian/uboot-session.py` stops autoboot (2 s) and runs commands from a file;
`scripts/debian/uboot-boot.py <PARTUUID>` boots the kernel from the USB stick (RAM only).
To return to the stock boot: `setenv bootcmd 'run stockboot'; saveenv` (or the original
`nand read.e 0x2000000 $(kernel_addr) 0xA00000; bootm 0x2000000`). Copies of the environment
before the change are kept outside git (`nas-revival-backup/`).
