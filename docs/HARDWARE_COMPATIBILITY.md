# Hardware compatibility

Evidence levels: **verified** (measured on the device by us) · **observed** (seen once, not
qualified) · **declared** (device tree / datasheet / upstream only) · **unknown**. The UI may offer a
write operation only for *verified* capabilities. Profiles: [ARCHITECTURE §5](ARCHITECTURE.md#5-hardware-abstraction-layer).
Raw evidence and history: [hardware/zyxel-nsa310.md](hardware/zyxel-nsa310.md).

## ZyXEL NSA310 (profile `zyxel-nsa310`)

Running Debian 13.8 armel, kernel 6.12.119, DT `kirkwood-nsa310a` (checked 2026-11-25).

| Capability | Interface | Evidence | Notes |
|---|---|---|---|
| CPU | Marvell 88F6281 A1, Feroceon 88FR131, ARMv5TE, 1 core, ~1.2 GHz | verified | no NEON/VFP; Go `GOARM=5` soft-float |
| RAM | 256 MiB, 246 136 kB visible to Linux | verified | budget base = 240 MiB |
| Swap | 1 GiB partition on internal disk | verified | — |
| NAND | 128 MiB Samsung, 9 MTD partitions, 1 factory bad block | verified | kernel slots A/B; [debian/boot.md](debian/boot.md) |
| Internal SATA | 1 bay, `sda` | verified | currently ST1000LM014 SSHD 1 TB |
| eSATA | second SATA port | declared | not tested; LEDs exist |
| USB | 2 ports via internal Genesys hub | verified | boot stick `sdb` |
| Ethernet | RTL8111D (r8169), 1 GbE | verified | 3 netdev LED triggers `enp1s0-*` |
| Temperature | LM85 on i2c 0x2e, 3 channels | verified | none of them is the disk; disk temp via SMART |
| Fan | LM85 `fan1` tach + `pwm1` | verified | automatic zone mode (`pwm1_auto_channels`=123) set by `nsa310-fan`; fan2–4 unused |
| LEDs | 9 board LEDs: sys/hdd/esata/copy (green+red), usb (green) + 3 LAN | verified | per-SATA-port triggers missing in 6.12 |
| Buttons | COPY (evdev), RESET, POWER | COPY verified; RESET/POWER observed | power button = gpio-poweroff path not tested from UI |
| RTC | PCF8563 i2c 0x51 | observed | alarm-based power-on: **unknown** (wiring to PSU not known) |
| Wake-on-LAN | RTL8111D | unknown | needs test with PSU standby rail |
| Power after outage | — | observed: stays off | not configurable known; recovery plan covers it |
| Disk standby | ATA standby via hdparm | unknown on this disk | `hdparm` not installed yet; system shares disk (see below) |
| SMART | ATA SMART via smartctl | verified | self-test long running/scheduled 2026-11-25 |
| exFAT / NTFS | kernel modules | unsupported (current kernel) | needs new kernel build (FEATURE_MATRIX STOR-16) |
| Watchdog | Orion watchdog | declared | not enabled; candidate for M5 |
| Crypto offload | Marvell CESA | declared | not used; TLS in Go software |

### Constraints that shape features

- **Single disk for system, swap, logs and data:** idle writes ~0.2 MiB per 30 s (measured) keep
  the disk active. Disk spin-down needs ADR 0007 (journal/commit changes or root on USB).
- **SSHD head parking:** SMART attribute 193 (load cycles) 35 003 at 6 866 h – APM setting matters.
- **No automatic power-on after power loss**: scheduled power-on and UPS restart are not offered.
- **Debian 13 is the last armel release** and has no Kirkwood kernel: the kernel is ours to maintain
  (6.12 LTS). Plan for the end of trixie security support.

## Generic ARM NAS (`generic-arm-nas`)

Selected when the device tree has no profile. Read-only: discovered hwmon sensors (by driver name),
LEDs listed but not writable, no fan control, no buttons. Storage, services and users work because
they use standard Linux interfaces.

## Generic Linux NAS (`generic-linux-nas`)

x86/other architectures with DMI model name. Same rules as generic ARM. Build needs only another
`GOARCH`; no code path may assume NAND, U-Boot or LM85 outside the NSA310 profile.

## Adding a device

1. Collect evidence read-only (DT `compatible`, hwmon names/labels, LED names, input devices, SATA/USB topology).
2. Write `devices/<id>/profile.json` with evidence level per capability.
3. Verify each actuator on the device (fan floor, LED, button) before marking it verified.
4. Add a row set here and a hardware test pass in [TEST_PLAN](TEST_PLAN.md).
