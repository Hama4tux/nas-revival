# OpenWrt qualification: read-only probe

Status 2026-11-16: prepared, **not yet run on the NAS**. Running it requires the owner's approval for each run.

[`scripts/openwrt/probe-readonly.sh`](../../scripts/openwrt/probe-readonly.sh) runs on the stock firmware through the existing root Telnet shell. It only reads `/proc`, `/sys`, `dmesg` and (if present) `fw_printenv`. It does no mounts, writes, module loads, i2c/SMART transactions or network changes, and it redacts MAC addresses and serial numbers. `tests/test_openwrt_probe.py` enforces this statically. Keep the output private (`output/` is Git-ignored) and transfer only sanitized conclusions into the [hardware evidence](../hardware/zyxel-nsa310.md).

| Question | Probe evidence | Why it matters for OpenWrt |
| --- | --- | --- |
| Exact SoC and clock | `dmesg` Kirkwood/TCLK line, `/proc/cpuinfo` | Kirkwood 88F6281 assumed by every NSA310 DT |
| Fan/thermal chip | i2c device names, hwmon names | NSA310 DT: ADT7476; NSA310a and OpenWrt NSA310b: LM85. Wrong chip means no fan control |
| Ethernet controller | PCI vendor/device, bound driver | OpenWrt NSA310b expects r8169 (Realtek, PCIe) |
| NAND geometry and map | `/proc/mtd`, sysfs writesize/oobsize | OpenWrt replaces vendor partitions with UBI; any offset must be re-derived |
| U-Boot environment | `bootcmd`, `bootargs`, `kernel_addr`, `arcNumber`, `mainlineLinux` | Resolves the `kernel_addr` discrepancy and whether the bootloader can boot a DT kernel |
| kexec | `/proc/config.gz`, kallsyms, kexec binary | Only no-UART path to boot a new kernel; unverified and still prohibited |
| LEDs, buttons, GPIO | debugfs gpio, leds, input devices | Board profile mapping |
| Storage and USB | partitions, mdstat, SCSI, USB IDs | SATA controller, USB boot media |

Outcome: if SoC, fan chip and Ethernet match the OpenWrt NSA310b profile, the next gate is UART access and an OpenWrt initramfs booted from USB or RAM (M3) without NAND writes. Any mismatch blocks M3 until a matching device tree is identified.
