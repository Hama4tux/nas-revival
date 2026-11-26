# Moving the system to a new internal disk (USB stick, no console)

Replaces the internal disk (e.g. the 2.5" 1 TB by a 3.5" 2 TB) without a serial console, without a
second computer and without touching NAND or the U-Boot environment. Scripts:
[`scripts/debian/disk-swap/`](../../scripts/debian/disk-swap/).

## How it works

U-Boot always boots the kernel in NAND with `root=PARTUUID=<system partition>`
([boot.md](boot.md)). `prepare-usb.sh` turns a USB stick into a copy of the running system and, as
its last step, gives the stick's partition that same PARTUUID. After the swap the new disk has no
partition with it, so the kernel mounts the stick. On the stick the service `nas-revival-install`:

1. picks the only SATA disk and refuses if it already holds a NAS Revival system or data partition
   (label `nasroot`/`nasdata`, GPT name `nas-root`/`nas-data`) – it never wipes a NAS disk;
2. checks the payload (`sha256sum`), then erases everything on the disk (old ZFS pools included:
   `wipefs` on every partition and the disk, GPT zapped, 16 MiB zeroed at both ends);
3. partitions it like the old disk – `nas-root` (same size, **same PARTUUID**), `nas-swap`,
   `nas-data` (the rest) – and creates the file systems with the labels the fstab uses
   (`nasroot`, `nasswap`, `nasdata` with 0 % reserved blocks);
4. unpacks the snapshot of `/` and of `/srv/data` (owners, ACLs and extended attributes kept);
5. gives the stick a new random PARTUUID, so it no longer answers to the system one, and reboots.

The NAS then boots from the new disk with the same configuration, accounts, SSH host keys and
address. COPY LED: green blinking = installing, green = done (reboots), red = error (the stick system
stays up with SSH; log in `/nas-revival-install/install.log` on the stick). On the stick system
everything that touches disks, data or packages is masked (web UI and helper, COPY button, smartd,
cron, NFS/SMB, apt and maintenance timers); SSH, network, fan and LEDs run.

The stick is wiped (its old rescue copy and kernels are lost; the previous kernel stays in NAND slot
B). Its size must exceed two copies of the system plus the data plus 10 % – data larger than the
stick cannot move this way. The old disk is not modified and stays a complete backup.

## Procedure

1. On the NAS (as root), with only the stick to be used plugged in:
   `scripts/debian/disk-swap/prepare-usb.sh /dev/sdX --wipe-stick` (copy the three files of
   `scripts/debian/disk-swap/` to the NAS first). The web UI is stopped for the snapshot (about a
   minute) and started again.
2. `poweroff` right away. Until the disk is swapped, do not boot with both the old disk and the stick:
   the kernel could pick either (the installer then refuses and nothing happens; remove the stick).
3. Replace the disk, keep the stick in, power on. Wait for the COPY LED to turn green and the NAS to
   reboot (a few minutes for the system; longer with much data).
4. Check: `ssh nsa310`, `lsblk` (new disk, `nas-root` mounted on `/`), `df -h /srv/data`; set the disk
   power profile again in Storage (settings are per disk). The stick can be removed.

Tested 2026-11-26 on the NAS against loop devices (`NR_TEST=1` / `NR_TEST_TARGET=`): stick
preparation, wipe of an old partition table, partitioning, restore of system and data, refusal of a
disk that already holds a NAS Revival system. The real boot from the stick is not tested yet.
