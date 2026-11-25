# Debian 13 on the NSA310: kernel from USB, system on the internal disk

Status: verified on the console on 2026-11-25; no `saveenv`, NAND unchanged. The serial
adapter (CP2102) is attached to the separate Live Ubuntu laptop, not to the dev VM.

The stock U-Boot 1.1.4 has no SATA/GPT support, so the kernel stays on a USB stick and only
the root file system lives on the internal disk.

| Device | FS / label | Content |
|---|---|---|
| USB stick part 1 | ext2 `boot` | `/boot`: `uImage` (kernel + appended `kirkwood-nsa310a.dtb`), `uImage.noled` = previous kernel |
| USB stick part 2 | ext4 `rootfs` | old USB root, frozen copy kept as fallback |
| disk part 1 (30 GB) | ext4 `nasroot` | Debian 13 armel root |
| disk part 2 (1 GB) | swap `nasswap` | swap (256 MB RAM) |
| disk part 3 (rest) | ext4 `nasdata` | `/srv/data` (`nofail`) |

The stock U-Boot finds the stick only in the front port and only on the first `usb start`
after power-on. Console 115200 8N1 on J2; interrupt the ~3 s countdown (`NSA310>>`), then:

```
usb start
ext2load usb 0:1 0x2000000 /uImage
setenv bootargs console=ttyS0,115200 root=PARTUUID=<nas-root PARTUUID> rootwait rw
bootm 0x2000000
```

`scripts/debian/uboot-boot.py <PARTUUID>` does this automatically when started before
power-on. For the fallback USB root use the stick's part 2 PARTUUID. The fallback kernel
`/uImage.noled` needs `/lib/modules/6.12.119.noled` renamed back first, otherwise r8169 does
not load.
