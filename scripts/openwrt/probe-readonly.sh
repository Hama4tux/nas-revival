#!/bin/sh
# Read-only hardware probe for the stock NSA310 firmware (BusyBox 1.17.2, Linux 2.6.31.8).
# Run by the owner over the existing root Telnet shell, output captured on the workstation:
#   sh probe-readonly.sh > probe.txt
# It only reads /proc, /sys and dmesg. No mounts, writes, module loads, i2c/SMART
# transactions, fw_setenv or network changes. MAC addresses and serials are redacted.

redact() {
    sed -e 's/\([0-9a-fA-F]\{2\}:\)\{5\}[0-9a-fA-F]\{2\}/XX:XX:XX:XX:XX:XX/g' \
        -e 's/\(serial[^=:]*[=:][[:space:]]*\)[^[:space:]]*/\1REDACTED/Ig'
}

section() { echo; echo "===== $1"; }

show() {  # show FILE...: print readable files, mark missing ones
    for f in "$@"; do
        if [ -r "$f" ]; then echo "--- $f"; cat "$f" 2>/dev/null; else echo "--- $f (missing)"; fi
    done
}

{
section "kernel"
uname -a
show /proc/version /proc/cmdline

section "cpu and memory"
show /proc/cpuinfo /proc/meminfo

section "boot log: SoC, clocks, ethernet, sata, usb, i2c, nand"
dmesg 2>/dev/null | grep -iE 'kirkwood|88f6|tclk|feroceon|r8169|rtl81|mv643|eth[0-9]|phy|sata_mv|ata[0-9]|ehci|usb [0-9]|i2c|adt7|lm85|hwmon|nand|mtd|ubi|memory'

section "kexec support"
if [ -r /proc/config.gz ]; then
    zcat /proc/config.gz | grep -E 'KEXEC|ATAGS|ARCH_KIRKWOOD|MACH_|USE_OF|MTD_NAND|R8169|SENSORS_(ADT7475|LM85)'
else
    echo "/proc/config.gz missing"
fi
grep -i kexec /proc/kallsyms 2>/dev/null | head -5
ls -l /sbin/kexec /usr/sbin/kexec 2>/dev/null

section "mtd"
show /proc/mtd
for d in /sys/class/mtd/mtd[0-9]*; do
    [ -d "$d" ] || continue
    echo "$d: name=$(cat $d/name 2>/dev/null) size=$(cat $d/size 2>/dev/null) erasesize=$(cat $d/erasesize 2>/dev/null) writesize=$(cat $d/writesize 2>/dev/null) oobsize=$(cat $d/oobsize 2>/dev/null)"
done

section "u-boot environment (read only, via fw_printenv if present)"
if command -v fw_printenv >/dev/null 2>&1; then
    fw_printenv 2>&1 | grep -E '^(bootcmd|bootargs|kernel_addr|mtdparts|arcNumber|mainlineLinux|bootdelay|stdin|stdout|usb|ipaddr|serverip)='
else
    echo "fw_printenv not present"
fi

section "pci devices (ethernet controller)"
for d in /sys/bus/pci/devices/*; do
    [ -d "$d" ] || continue
    echo "$d vendor=$(cat $d/vendor) device=$(cat $d/device) class=$(cat $d/class) driver=$(basename "$(readlink $d/driver 2>/dev/null)" 2>/dev/null)"
done
show /proc/bus/pci/devices

section "network drivers and link"
for n in /sys/class/net/*; do
    echo "$n driver=$(basename "$(readlink $n/device/driver 2>/dev/null)" 2>/dev/null) speed=$(cat $n/speed 2>/dev/null) operstate=$(cat $n/operstate 2>/dev/null)"
done

section "i2c, hwmon, fan and thermal"
for d in /sys/bus/i2c/devices/*; do
    [ -d "$d" ] || continue
    echo "$d name=$(cat $d/name 2>/dev/null) driver=$(basename "$(readlink $d/driver 2>/dev/null)" 2>/dev/null)"
done
for h in /sys/class/hwmon/hwmon*; do
    [ -d "$h" ] || continue
    echo "$h name=$(cat $h/name $h/device/name 2>/dev/null | head -1)"
    ls "$h" "$h/device" 2>/dev/null | grep -E '^(temp|fan|pwm)[0-9]_(input|label)$|^pwm[0-9]$' | sort -u
done

section "gpio, leds, input"
show /sys/kernel/debug/gpio
ls /sys/class/leds /sys/class/gpio /sys/class/input 2>/dev/null
show /proc/bus/input/devices

section "storage"
show /proc/partitions /proc/mdstat /proc/scsi/scsi
show /sys/block/sd?/device/model /sys/block/sd?/device/vendor

section "usb"
for d in /sys/bus/usb/devices/*; do
    [ -r "$d/idVendor" ] && echo "$d $(cat $d/idVendor):$(cat $d/idProduct) $(cat $d/product 2>/dev/null)"
done

section "modules"
show /proc/modules
} 2>&1 | redact
