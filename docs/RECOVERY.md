# Recovery

Status 2026-11-25, Debian phase. Supersedes the OpenWrt-era [recovery/recovery-plan.md](recovery/recovery-plan.md)
(kept for history). Boot details: [debian/boot.md](debian/boot.md). Every step that writes NAND,
saves the U-Boot environment, reboots or formats needs the owner's explicit consent.

## What protects the device today

| Layer | Protection | Verified |
|---|---|---|
| Kernel | slot A (`kernel_2`) → slot B (start of `rootfs2`) → stock ZyXEL firmware (`kernel_1`/`rootfs1`) | yes, invalid image falls through |
| U-Boot | untouched; only `bootcmd` chain saved in environment | yes |
| Environment | copies before change outside git (`nas-revival-backup/`) | stored |
| NAND | raw dumps of all 9 partitions with OOB + SHA256SUMS outside git | hashes yes, **restore not qualified** |
| Root FS | on internal disk; old frozen root copy on the USB stick (`rootfs`) | boots via console |
| Data | `/srv/data` on the same disk – **no backup yet** (M4) | – |
| Console | UART J2, CP2102, 115200 8N1, on the separate Live Ubuntu laptop | yes |

Gaps: a kernel that **boots but hangs** is not caught (no boot-once / watchdog yet); a missing disk
makes the kernel wait forever (`rootwait`); the NAS does not power on after a power cut.

## Scenarios

| # | Situation | Symptom | Recovery |
|---|---|---|---|
| R1 | Web UI broken / crash loop | no :8443, SSH works | `ssh nsa310`, `journalctl -u nas-revival`, reinstall previous binary from `output/webui` or `/usr/local/lib/nas-revival/b` (M5), `systemctl restart nas-revival` |
| R2 | Admin password lost | login fails | over SSH: `nas-revival set-password -config /etc/nas-revival` |
| R3 | Bad network config | NAS unreachable after change | wait 180 s: helper rolls back (also after reboot). Otherwise UART: edit `/etc/systemd/network/20-wired.network`, `networkctl reload` |
| R4 | Bad firewall (M5) | ports blocked | same confirm/rollback; UART: `nft flush table inet nas_revival` |
| R5 | Service config rejected/broken | service failed | Config Manager rollback in UI or `nas-revival config rollback <n>` over SSH |
| R6 | New kernel in slot A does not load | U-Boot "Bad Magic"/CRC | automatic: slot B boots. Then rewrite slot A from Linux |
| R7 | New kernel loads but hangs/panics | no network, LEDs stuck | UART: stop autoboot, `run slotB`; from Linux copy B → A. Planned: boot-once flag (OPEN.md) |
| R8 | Both slots bad | stock firmware boots | stock firmware is a usable shell (Telnet/console); from UART load kernel from USB (`scripts/debian/uboot-boot.py`) and rewrite slots |
| R9 | Root FS damaged | kernel panic / emergency mode | UART: boot with `root=` of the USB stick `rootfs`, `e2fsck` the disk root, restore config backup |
| R10 | Internal disk missing/dead | kernel waits for root | UART: boot USB root; replace disk; recreate partitions per boot.md; restore config + data backup |
| R11 | Debian update broke boot | emergency mode | USB root, `chroot`, `apt` fix, or restore previous packages list |
| R12 | Power cut | NAS stays off | press power button; plan UPS (HW-09) |

## Access recovery without the web UI

1. SSH with key (`ssh nsa310`, root today; named admin user after M2).
2. Serial console (always available, independent of network and web).
3. Stock firmware fallback (last resort, only if both slots fail).

The web UI must never be the only way to undo a change it made: every helper-managed file is plain
text with a snapshot in `/var/lib/nas-revival-helper/config/`, and `nas-revival config` (M1-03)
offers the same rollback from the shell.

## Disaster recovery package (M4, DR-01)

Exported by Backup & Restore to a USB disk or another host: NAS Revival config archive, package list
(`dpkg --get-selections`), `/etc` subset from the managed file registry, device profile id, disk
partition table (`sfdisk -d`), kernel images of both slots with sha256. Not included: NAND dumps,
U-Boot environment, TLS key (unless encrypted export).

Restore order: install disk per boot.md → Debian rootfs → `nas-revival` binary → config import →
data restore → verify with TEST_PLAN smoke tests.

## Rules for UI-driven recovery-sensitive operations

- No NAND/bootloader flash from the regular UI without a separate design (only the kernel slot update
  UPD-04 with A→B copy, write one slot, readback verify, boot-once).
- No automatic `saveenv` / `fw_setenv` until writing mtd1 from Linux is qualified.
- No automatic filesystem repair or formatting on discovery.
