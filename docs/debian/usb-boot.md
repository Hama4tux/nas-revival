# Debian 13 from USB via the stock U-Boot (temporary, RAM only)

Status: used on 2026-11-25; no `saveenv`, NAND unchanged. The command sequence below is
reconstructed from the running system (`/proc/cmdline`, USB layout) and still has to be
confirmed on the console at the next boot.

USB stick (front port only – the stock U-Boot finds it only on the first `usb start` after power-on):

| Partition | FS | Content |
|---|---|---|
| sda1 | ext2, label `boot` | `uImage` (kernel + appended `kirkwood-nsa310a.dtb`), `uImage.noled` = previous kernel |
| sda2 | ext4, label `rootfs` | Debian 13 armel |

Console 115200 8N1 on J2, interrupt the ~3 s countdown (`NSA310>>` prompt), then:

```
usb start
ext2load usb 0:1 0x2000000 /uImage
setenv bootargs console=ttyS0,115200 root=PARTUUID=<rootfs PARTUUID> rootwait rw
bootm 0x2000000
```

Fallback kernel: load `/uImage.noled` instead; it needs the matching modules, so first rename
`/lib/modules/6.12.119` ↔ `/lib/modules/6.12.119.noled` (otherwise r8169 does not load).
