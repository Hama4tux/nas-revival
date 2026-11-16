# Recovery plan — preparation only

**No steps in this document authorize device access now.** Phase 1 does not connect to the NAS, reboot it, attach UART/USB or build an image. The owner-reported stock administration still works; preserve it.

## Risk classes

| Class | Examples | Authorization |
| --- | --- | --- |
| A: workstation-only read | Public code/manual inspection, local document validation | Current phase |
| B: read intent on live unit | Stock status viewing, serial log capture, reading MTD metadata | Future explicit inventory approval; connecting wires/rebooting has physical and operational risks |
| C: transient execution | Interrupt boot, load and execute an external kernel | Separate approved experiment; even without NAND writes, code can change hardware/disk state |
| D: persistent/destructive | NAND/MTD writes/erase, saved U-Boot environment, format/repair, factory reset, firmware upload | Prohibited in this phase; future specific approval, verified backup and recovery qualification required |

Even “read-only” status or SMART queries may wake a disk; a running stock OS can continue background writes. No routine here is a forensic guarantee. If data recovery is needed after JBOD initialization, stop OS/firmware experiments and make a separate recovery plan with the owner before power or disk actions.

## Gate 1: future inventory

After owner approval, identify the PCB revision, chip markings, serial header orientation and disk/data priority. Prefer passive observations before commands. Establish whether a safe normal shutdown/reboot is acceptable. Do not press reset/copy buttons experimentally.

Candidate **read-only command examples for a future authorized Linux console**, not commands run during bootstrap:

```sh
uname -a
cat /proc/cpuinfo
cat /proc/meminfo
cat /proc/mtd
cat /proc/cmdline
dmesg
```

Review their output privately; boot arguments/logs can expose identifiers or secrets. Available commands and shell access are unknown. Do not obtain access through an exploit, vendor firmware upload or installation script in this phase.

At an identified U-Boot prompt, future `version`, `help` and `printenv` inspection can establish build and capabilities without intentionally changing environment. Confirm their semantics on that build; current [printenv documentation](https://docs.u-boot.org/en/latest/usage/cmd/printenv.html) is a reference, not proof of vendor command support. Environment exports can contain secrets and must remain private. No `setenv`, `saveenv`, `fw_setenv`, `env default`, or boot script execution is part of inventory.

## Gate 2: UART preparation

Physical intervention requires approval, power isolation before wiring, ESD precautions and exact board-specific pin identification. A reference DTS console rate of 115200 does not establish electrical pinout. Verify signal voltage; never connect RS-232 voltage levels or assume a “3.3 V” adapter has safe I/O merely from its power label.

Use only verified GND/TX/RX connections, cross TX/RX and leave VCC disconnected. Begin with receive-only capture if appropriate after grounding/voltage checks. Never connect an adapter's power output to the NAS. Do not copy another revision's pin numbering. UART output may identify U-Boot and actual DRAM/NAND even when SSH/Telnet is unavailable. Wiring and power cycling are not risk-free read operations.

## Gate 3: future NAND backup

First record the **actual** MTD partition offsets/sizes and full chip geometry, tool versions, ECC scheme/statistics and bad-block map. Neither the reference Linux DT nor the [OpenWrt repartition patch](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/patches-6.12/107-01-zyxel-nsa3x0-common-nand-partitions.patch) is the installed map.

Plan two documented forms as supported by the verified controller/tooling: logical ECC-corrected partition data and a raw/OOB-aware capture preserving recovery-relevant metadata. Record whether each tool skips, pads or retains bad blocks, and distinguish data/ECC bytes from OOB layout. Consult [Linux NAND documentation](https://docs.kernel.org/driver-api/mtdnand.html) and the exact available nanddump version before choosing arguments.

Do not provide a universal `dd`, nanddump offset or restore recipe before this inventory. A data-only copy is insufficient evidence of recoverability. The backup procedure must open source MTD read-only, store outside the NAS/NAND and existing user disk, check free destination space and bound streaming buffers; holding several dumps in 256 MiB RAM is unsuitable.

Archive bootloader, environment, vendor metadata/keys and system partitions with a private manifest of tool/options, start/length, ECC/OOB mode, bad-block policy, timestamp, size and SHA-256. Keep two independent copies off the device. Repeated stable-partition reads should agree or have explained ECC/bad-block differences; changing system partitions need a quiescent strategy. Verify offline parsing/geometry and a non-destructive recovery rehearsal before considering backups qualified. Never test recovery by overwriting the sole working unit.

## Gate 4: temporary USB boot proposal

Proceed only after exact board/DT, bootloader commands, cooling and backups are qualified and the owner approves a separate proposal. Do not replace U-Boot just to make an initial test convenient. BootROM UART/kwboot support is not established; it is a possible recovery investigation, not a guaranteed rescue route.

The proposal must document image type supported by this stock loader (legacy uImage/FIT/DT handling), kernel/initramfs/DTB sizes, RAM load addresses, decompression ranges and non-overlap. Modern [bootm documentation](https://docs.u-boot.org/en/latest/usage/cmd/bootm.html) does not prove modern FIT support in stock U-Boot. No addresses or executable boot chain are prescribed yet.

To keep the strict no-environment-change rule, require a supported explicit one-shot load/boot sequence that does not invoke environment-changing scripts or commands. If the loader requires environment changes, stop and seek a separately scoped decision; do not silently treat unsaved changes as permitted.

External boot must start with no automount, no filesystem recovery, no RAID assembly, no UBI auto-attach and no startup environment writes. Mark MTD read-only or omit write-capable MTD support in a later experimental configuration. Keep user SATA electrically disconnected if the owner approves and it is safe; otherwise a verified kernel-level no-write strategy is required. A mere read-only ext4 mount can replay a journal unless explicitly prevented; first test should mount no user filesystem at all.

## Return and stop criteria

Expected return from a qualified external test is stock boot after the controlled removal of experiment media, using the original persisted boot path. This is currently an **unverified objective**. Watchdog resets, stock USB autodetection and boot scripts must be understood first.

Stop on unknown revision, unverified voltage, unexpected boot command semantics, missing backups, unexplained ECC errors, ambiguous RAM ranges, unsafe fan state or any path that writes NAND/environment/user disk. Persistent flashing and recovery restore commands require a later device-specific plan and are intentionally absent.
