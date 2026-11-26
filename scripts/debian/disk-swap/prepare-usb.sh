#!/bin/sh
# Prepare a USB stick that moves the running NAS system to a new internal disk (docs/debian/disk-swap.md).
# Run on the NAS as root: prepare-usb.sh /dev/sdX --wipe-stick
#
# The stick is wiped and gets one ext4 partition (GPT name nas-revival-install, label nrinstall) with
#   - a bootable copy of the running system with the installer service (nas-revival-install) enabled and
#     everything that touches disks or data disabled,
#   - /nas-revival-install/{rootfs.tar,data.tar,install.conf,SHA256SUMS}: snapshot of / and /srv/data.
# Last, the partition gets the PARTUUID of the current system partition. The kernel in NAND boots
# root=PARTUUID=<that>, so after the disk swap (old disk out, new one in) it boots the stick, which
# installs onto the new disk. Nothing is written to NAND or the U-Boot environment.
#
# Power off right after this script and swap the disk before powering on again: with the old disk
# and the stick both present, the kernel may boot either (the installer then refuses to wipe a disk
# with a NAS Revival system and does nothing).
#
# Test mode: NR_TEST=1 skips the USB and service checks (stick may be a loop device).
set -eu
stick="${1:-}"
[ "${2:-}" = --wipe-stick ] || { echo "usage: $0 /dev/sdX --wipe-stick" >&2; exit 2; }
here="$(cd "$(dirname "$0")" && pwd)"
TEST="${NR_TEST:-}"
MNT=/mnt/nr-stick
die() { echo "error: $*" >&2; exit 1; }
part() { case "$1" in *[0-9]) echo "${1}p$2" ;; *) echo "$1$2" ;; esac; }
# snap: GNU tar exits 1 when a file changed while it was read (logs, mountpoints of a live
# system) - a warning, the archive is complete; 2 is a real error.
snap() { rc=0; tar --warning=no-file-changed "$@" || rc=$?; [ "$rc" -le 1 ] || die "tar failed ($rc)"; }

[ "$(id -u)" -eq 0 ] || die "run as root"
[ -b "$stick" ] || die "$stick is not a block device"
rootsrc="$(findmnt -no SOURCE /)"
rootdisk="/dev/$(lsblk -no PKNAME "$rootsrc")"
[ "$stick" != "$rootdisk" ] || die "$stick holds the running system"
[ -n "$TEST" ] || [ "$(lsblk -dno TRAN "$stick")" = usb ] || die "$stick is not a USB device"
ROOT_PARTUUID="$(blkid -s PARTUUID -o value "$rootsrc")"
[ -n "$ROOT_PARTUUID" ] || die "no PARTUUID on $rootsrc"
ROOT_MIB=$(( $(blockdev --getsize64 "$rootsrc") / 1048576 ))
swapdev="$(blkid -L nasswap || true)"
SWAP_MIB=1024
[ -z "$swapdev" ] || SWAP_MIB=$(( $(blockdev --getsize64 "$swapdev") / 1048576 ))

# Two copies of / plus the data plus 10 %.
used_root=$(df -B1 --output=used / | tail -1)
used_data=0
mountpoint -q /srv/data && used_data=$(df -B1 --output=used /srv/data | tail -1)
need=$(( (2 * used_root + used_data) * 11 / 10 ))
have=$(blockdev --getsize64 "$stick")
[ "$have" -gt "$need" ] || die "stick too small: need $((need / 1048576)) MiB, has $((have / 1048576)) MiB"
echo "stick $stick: $(lsblk -dno SIZE,MODEL "$stick" | tr -s ' '); system $((used_root / 1048576)) MiB, data $((used_data / 1048576)) MiB"

for p in $(lsblk -nrpo NAME,TYPE "$stick" | awk '$2 == "part" { print $1 }'); do
	umount "$p" 2>/dev/null || true
	wipefs -a "$p" >/dev/null
done
wipefs -a "$stick" >/dev/null
sgdisk --zap-all "$stick" >/dev/null
sgdisk -n 1:2048:0 -t 1:8300 -c 1:nas-revival-install "$stick" >/dev/null
blockdev --rereadpt "$stick" || partprobe "$stick" || true
udevadm settle || true
sp="$(part "$stick" 1)"
[ -b "$sp" ] || die "partition $sp missing"
mkfs.ext4 -F -q -L nrinstall "$sp"
mkdir -p "$MNT"
mount "$sp" "$MNT"
trap 'umount "$MNT" 2>/dev/null || true' EXIT
D="$MNT/nas-revival-install"
mkdir -p "$D"

# Freeze the configuration while the snapshot is taken (the web UI and helper write /etc).
restart=""
if [ -z "$TEST" ]; then
	for u in nas-revival.service nas-revival-helper.service; do
		systemctl is-active --quiet "$u" && restart="$restart $u"
	done
	# shellcheck disable=SC2086
	[ -z "$restart" ] || systemctl stop $restart
fi
echo "snapshot of / (other file systems such as /proc, /run, /tmp, /srv/data are not included)"
snap -C / --one-file-system --numeric-owner --acls --xattrs --xattrs-include='*' \
	--exclude=./var/cache/apt/archives/*.deb -cf "$D/rootfs.tar" .
echo "snapshot of /srv/data"
if mountpoint -q /srv/data; then
	# A test keeps its loop images in /srv/data/nrt-test; they are not data.
	snap -C /srv/data --one-file-system --numeric-owner --acls --xattrs --xattrs-include='*' \
		${TEST:+--exclude=./nrt-test} -cf "$D/data.tar" .
else
	tar -cf "$D/data.tar" --files-from /dev/null
fi
# shellcheck disable=SC2086
[ -z "$restart" ] || systemctl start $restart
# A test must not create a second partition with the PARTUUID of the running system.
[ -z "$TEST" ] || ROOT_PARTUUID="$(cat /proc/sys/kernel/random/uuid)"
cat >"$D/install.conf" <<EOF
# Written by prepare-usb.sh $(date -Is)
ROOT_PARTUUID=$ROOT_PARTUUID
ROOT_MIB=$ROOT_MIB
SWAP_MIB=$SWAP_MIB
EOF
(cd "$D" && sha256sum rootfs.tar data.tar >SHA256SUMS)

echo "installer system on the stick"
tar -xpf "$D/rootfs.tar" -C "$MNT" --numeric-owner --acls --xattrs --xattrs-include='*'
cat >"$MNT/etc/fstab" <<'EOF'
# NAS Revival installer stick (prepare-usb.sh): only the stick itself; the internal disk is left alone.
LABEL=nrinstall	/	ext4	noatime,errors=remount-ro	0	1
tmpfs	/tmp	tmpfs	defaults,size=64M	0	0
EOF
# Nothing that writes to disks, data or packages runs on the installer system; SSH, network, fan and LEDs stay.
for u in nas-revival.service nas-revival-helper.service nsa310-button.service smartmontools.service cron.service \
	nfs-server.service smbd.service nmbd.service apt-daily.timer apt-daily-upgrade.timer dpkg-db-backup.timer \
	e2scrub_all.timer e2scrub_reap.service fstrim.timer logrotate.timer hs-cert.timer hs-collector.timer; do
	# Units installed in /etc (ours) cannot be masked in place: move them aside first.
	if [ -f "$MNT/etc/systemd/system/$u" ] && [ ! -L "$MNT/etc/systemd/system/$u" ]; then
		systemctl --root="$MNT" disable "$u" >/dev/null 2>&1 || true
		mkdir -p "$MNT/etc/nas-revival-installer.disabled"
		mv "$MNT/etc/systemd/system/$u" "$MNT/etc/nas-revival-installer.disabled/"
	fi
	systemctl --root="$MNT" mask "$u" >/dev/null 2>&1 || true
done
for u in nas-revival.service nas-revival-helper.service nsa310-button.service smartmontools.service; do
	[ "$(readlink "$MNT/etc/systemd/system/$u")" = /dev/null ] || die "could not disable $u on the stick"
done
install -m 0755 "$here/nas-revival-install" "$MNT/usr/local/sbin/nas-revival-install"
install -m 0644 "$here/nas-revival-install.service" "$MNT/etc/systemd/system/nas-revival-install.service"
systemctl --root="$MNT" enable nas-revival-install.service >/dev/null
echo "nas-revival-installer" >"$MNT/etc/nas-revival-installer"
sync
umount "$MNT"
trap - EXIT

# Last step: the stick answers to the system PARTUUID (kernel root=PARTUUID=...).
if [ -n "$TEST" ]; then
	echo "== test: stick prepared, PARTUUID left unchanged"
	exit 0
fi
sgdisk -u "1:$ROOT_PARTUUID" "$stick" >/dev/null
blockdev --rereadpt "$stick" 2>/dev/null || true
sync
echo "== stick ready. Power off now, replace the disk, power on with the stick inserted."
