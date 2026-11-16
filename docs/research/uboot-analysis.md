# Offline U-Boot analysis — ZyXEL NSA310

Date: 2026-11-16. Local read-only inspection of private `../nas-revival-backup/mtd0.raw` and relevant saved metadata only. No NAS connection, reboot, environment change, software installation, image preparation or external upload. `secret.env` was not read.

## Dump structure and method

Available tools used: Python 3, `strings`, `file`, `xxd`. `binwalk` was not installed and was not added. `file` reports both raw and derived files simply as `data`.

- Source length **1081344 bytes**; SHA-256 matches the private manifest before/after analysis. Original file opened read-only.
- **512 records × (2048 data + 64 OOB) bytes**. OOB bytes 0–39 are all `0xFF`; bytes 40–63 contain structured non-erased values on programmed pages, supporting per-page interleaving rather than a trailing OOB block.
- Separate data copy **1048576 bytes**, OOB copy **32768 bytes** and full string output remain in ignored `.private-analysis/`, directory mode 0700, files mode 0600.
- Reconstructed header begins with `0x8B`; its 32-byte main-header additive checksum matches. Fields describe source offset `0x200`, payload length `0x73E6C`, destination `0x600000`, entry `0x670000`. The payload's stored 32-bit additive word checksum also matches. Coherent command records provide further independent structural evidence for correct OOB removal.
- No ECC correction was performed. Agreement of these checksums is consistency evidence, not authentication or proof of recoverability. The derived file must never replace the original OOB-bearing backup in a restore plan.

## Static identification and commands

Banner at data-copy offset **`0x52B44`**:

> U-Boot 1.1.4 (Feb 22 2011 - 10:31:35) Marvell version: 3.4.19

This identifies the backed-up bootloader image; no live boot banner was observed. The header-derived address mapping resolves 59 consecutive, internally consistent 28-byte command records beginning at `0x739F4`, including valid name/help pointers and handler addresses. This is stronger than isolated strings, but does not prove working hardware.

| Item | Static evidence | Limit |
| --- | --- | --- |
| `usb` | Record `0x73F88`, handler `0x63B680`, subsystem help | Compiled command, not a tested controller |
| `usb start` | Help describes initialization and bus scan | Not executed |
| `usb storage` | Help and storage-transport/enumeration messages | USB media compatibility untested |
| `usb reset` | Exact phrase not found; generic reset strings also belong to other subsystems | Subcommand behavior unresolved; missing phrase does not prove absence |
| USB host | Host-mode, device/hub descriptor and mass-storage messages | Strong host-stack evidence; no literal EHCI/OHCI identification; exact HCD/port unverified |
| `usbboot` | Record `0x73F6C`; help expects address and device/partition | Block-device boot command, not proof of automatic FAT-file boot |
| `fatload`, `fatls` | Records `0x73B28`, `0x73B0C`; FAT12/16/32 labels | FAT loader present; owner's 2 GB FAT32 partition not tested |
| `ext2load`, `ext2ls` | Records `0x73AB8`, `0x73AD4` | No inference of modern ext4 feature compatibility |
| `ext4load` | No exact name or matching command record found | No evidence of a dedicated ext4 loader |
| `bootm` | Record `0x73A64`, handler `0x631AAC`, image-check messages and uImage magic | Legacy-image boot path present; alternative kernel untested |
| `bootz` | No exact name or matching command record found | No evidence of direct zImage command support |
| `tftpboot` | Record `0x73EA8`, handler `0x638E20`, network-loading help | Needs pre-OS control and tested network; neither established |
| `iminfo`, `bootd`, `boot`, `run` | Consistent command records/help | Inspection/default-command execution facilities; not authorization to run them |

Offsets are data-copy evidence positions, not NAND/raw offsets or approved new-kernel RAM addresses. No boot command sequence is supplied.

## Formats and vendor recovery clues

Legacy uImage magic `0x27051956`, header/data checksum errors and multi-file image descriptions are present. `bootm` help describes Linux and an optional initrd address. gzip-related code/error messages exist; a bzip2 label alone does not establish a working decompressor. No positive FIT/FDT, direct zImage or LZMA boot evidence was found in this bounded pass. Modern kernel/DTB compatibility is unverified.

Vendor strings describe recovery from USB FAT/ext2, network fallback, `enaAutoRecovery`, a TFTP recovery process and **Update bootcmd**. Trigger and write behavior are unresolved. This is not a qualified no-write route: do not invoke recovery, guess recovery filenames or press buttons as a workaround.

The allowlisted saved environment describes NAND read then `bootm`, delay 2, console rate 115200, `nandEcc=1bit` and NSA-310/A203 identity. These are not all environment contents. The stock RAM destination is not a qualified load address for a new image. The [kernel-address discrepancy](../hardware/zyxel-nsa310.md#address-discrepancy) must be resolved. The ECC environment string does not establish the dump tool's ECC/bad-block policy.

## Boot alternatives without UART

Available according to the owner: root **Telnet**, SMB, web administration, off-device backups and a 2 GB FAT32 USB partition. Telnet is a Linux shell, not a bootloader console. File-upload access does not establish execution or boot capabilities.

| Variant | Feasibility / prerequisites | Failure, return and physical intervention | Safe assessment now |
| --- | --- | --- | --- |
| U-Boot USB load/boot | Compiled facilities exist, but need pre-OS command access, qualified image/RAM map and tested USB. Fixed NAND bootcmd has no demonstrated USB branch | A hang may require physical reset/power cycle; unchanged settings could preserve stock boot afterward, but automatic fallback is not established | Static analysis only; not executable under current constraints |
| `kexec` | **Unverified**: need exact running kernel CONFIG_KEXEC, usable architecture/syscall/tooling and hardware/DT/ATAG handoff support. Version 2.6.31.8/root access alone prove none of this | Bypasses bootloader initialization; may hang and need physical reset. No guaranteed automatic return | Need configuration/build evidence first; no kexec command, load or execution attempted |
| U-Boot TFTP/other loaders | Compiled `tftpboot` still needs a pre-OS control channel and valid network/load setup | Same recovery/console problem as USB; automatic return unknown | Not an alternative control channel; no network activity performed |
| Vendor automatic recovery | Strings only; trigger and potential NAND/environment writes unknown | Could change persistent state; return unknown; button/power intervention may be needed | Reject as an unqualified workaround |

**No safe, demonstrated one-shot alternative boot path is established without UART under the current no-write/no-restart constraints.** Preserve NAND boot. The safest next course is to obtain an appropriate serial adapter with verified board pinout/voltage for a separately approved observation phase. Owner-provided kernel configuration could clarify kexec prerequisites in another read-only task, but would not prove a safe handoff.

## Remaining evidence

Actual bootloader/USB behavior and kernel/DT handoff; explanation of the saved address discrepancy; exact kernel configuration/kexec implementation if pursued; qualified UART wiring and watchdog/return behavior; backup creation tool/options, ECC/OOB/bad-block policy and an independently qualified restore method. Nine matching hashes do not prove successful restoration.
