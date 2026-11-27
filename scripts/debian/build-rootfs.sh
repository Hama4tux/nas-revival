#!/bin/bash
# Build the Debian 13 (trixie) armel root file system for the NSA310 as a tarball.
# Needs: mmdebstrap, qemu-user-static + binfmt (armel runs emulated), the kernel modules tarball
# from build-kernel.sh and an SSH public key for root.
#
# Debian 13 is merged-usr (/lib -> usr/lib): modules go to usr/lib/modules, never over the symlink.
# /etc/locale.gen exists before the locales package is configured, so its postinst generates cs_CZ.UTF-8
# (ssh clients send LANG=cs_CZ.UTF-8; without it bash warns "cannot change locale" at every login).
# Every hook starts with set -e, otherwise a failing line in a multi-line hook passes silently.
# No chroot in hooks: under unshare + qemu-user it cannot exec the armel loader.
# Usage: scripts/debian/build-rootfs.sh <modules-V.tar.gz> <ssh-public-key-file> [out.tar]
# Env:   NAS_MAC=xx:xx:xx:xx:xx:xx  factory MAC (U-Boot `printenv ethaddr`); without it the
#                                   kernel picks a random MAC on every boot.
#        ROOT_PW_HASH='$6$…'        root password hash (`openssl passwd -6`) for the serial console;
#                                   without it the root password is locked (SSH key only).
#        NAS_HOSTNAME=nsa310
#        KEYRING=file              Debian archive keyring; needed on non-Debian hosts (Ubuntu): extract
#                                   debian-archive-keyring_*.deb from deb.debian.org and pass
#                                   usr/share/keyrings/debian-archive-keyring.gpg from it.
# The network uses DHCP; set a static address later in the web UI (Settings → Network).
set -euo pipefail
MODS="$(realpath "$1")"; KEY="$(realpath "$2")"; OUT="$(realpath -m "${3:-output/rootfs/rootfs-trixie-armel.tar}")"
HOST="${NAS_HOSTNAME:-nsa310}"
grep -qE '^(ssh-ed25519|ssh-rsa|ecdsa-sha2-)' "$KEY" || { echo "$KEY is not an SSH public key" >&2; exit 1; }
[ -z "${NAS_MAC:-}" ] || [[ "$NAS_MAC" =~ ^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$ ]] || { echo "bad NAS_MAC" >&2; exit 1; }
mkdir -p "$(dirname "$OUT")"
HERE="$(cd "$(dirname "$0")/../../deploy/debian" && pwd)"

PKGS=systemd-sysv,systemd-resolved,systemd-timesyncd,udev,dbus,kmod,locales,ca-certificates,curl,\
openssh-server,openssh-sftp-server,firmware-realtek,smartmontools,hdparm,gdisk,mtd-utils,u-boot-tools,\
rsync,cron,logrotate,bash-completion,whiptail,iproute2,ifupdown,less,nano

export HOST KEY MODS HERE NAS_MAC="${NAS_MAC:-}" ROOT_PW_HASH="${ROOT_PW_HASH:-}"
# mmdebstrap's unshare mode runs apt and the hooks as a mapped uid that cannot enter private (0700)
# directories such as $HOME: stage every input in a world-readable temp dir (nothing secret is in it).
STAGE="$(mktemp -d /tmp/nas-revival-rootfs.XXXXXX)"; trap 'rm -rf "$STAGE"' EXIT
install -m 0644 "$MODS" "$STAGE/modules.tar.gz"; install -m 0644 "$KEY" "$STAGE/authorized_keys"
install -d -m 0755 "$STAGE/deploy"; install -m 0644 "$HERE"/* "$STAGE/deploy/"; chmod 0755 "$STAGE"
MODS="$STAGE/modules.tar.gz"; KEY="$STAGE/authorized_keys"; HERE="$STAGE/deploy"
KR=()
if [ -n "${KEYRING:-}" ]; then
  install -m 0644 "$KEYRING" "$STAGE/debian-archive-keyring.gpg"
  KR=(--keyring="$STAGE/debian-archive-keyring.gpg")
fi
mmdebstrap "${KR[@]}" --arch=armel --variant=important --components=main,non-free-firmware \
  --include="$PKGS" \
  --essential-hook='set -e; printf "cs_CZ.UTF-8 UTF-8\n" > "$1/etc/locale.gen"' \
  --customize-hook='set -e; tar -C "$1" --keep-directory-symlink --no-same-owner -xzf "$MODS"; test -L "$1/lib"' \
  --customize-hook='set -e; echo "$HOST" > "$1/etc/hostname"; printf "127.0.1.1\t%s\n" "$HOST" >> "$1/etc/hosts"' \
  --customize-hook='set -e; cat > "$1/etc/fstab" <<EOF
LABEL=nasroot	/	ext4	noatime,errors=remount-ro	0	1
LABEL=boot	/boot	ext2	defaults,noatime,nofail,x-systemd.device-timeout=10s	0	2
LABEL=nasswap	none	swap	sw	0	0
LABEL=nasdata	/srv/data	ext4	noatime,nofail	0	2
tmpfs	/tmp	tmpfs	defaults,size=64M	0	0
EOF
mkdir -p "$1/srv/data" "$1/boot"' \
  --customize-hook='set -e; mkdir -p "$1/etc/systemd/network"; printf "[Match]\nName=e*\n\n[Network]\nDHCP=yes\n" > "$1/etc/systemd/network/20-wired.network"
if [ -n "$NAS_MAC" ]; then printf "# Factory MAC from the ZyXEL U-Boot environment; the kernel gets none from the bootloader.\n[Match]\nPath=platform-mbus@f1000000:pcie@82000000-pci-0000:01:00.0\n[Link]\nMACAddress=%s\nNamePolicy=kernel database onboard slot path\n" "$NAS_MAC" > "$1/etc/systemd/network/10-nsa310-mac.link"; fi' \
  --customize-hook='set -e; ln -sf ../run/systemd/resolve/stub-resolv.conf "$1/etc/resolv.conf"' \
  --customize-hook='set -e; install -d -m 0700 "$1/root/.ssh"; install -m 0600 "$KEY" "$1/root/.ssh/authorized_keys"
printf "PermitRootLogin prohibit-password\nPasswordAuthentication no\n" > "$1/etc/ssh/sshd_config.d/50-nas-revival.conf"
awk -F: -v OFS=: -v h="$ROOT_PW_HASH" '"'"'$1=="root"{$2=(h!=""?h:"!*")}1'"'"' "$1/etc/shadow" > "$1/etc/shadow.new"
cat "$1/etc/shadow.new" > "$1/etc/shadow"; rm "$1/etc/shadow.new"' \
  --customize-hook='set -e; install -m 0755 "$HERE/nsa310-fan" "$HERE/nsa310-leds" "$1/usr/local/sbin/"
install -m 0644 "$HERE/nsa310-fan.service" "$HERE/nsa310-leds.service" "$HERE/nsa310-led-fault@.service" "$1/etc/systemd/system/"
install -d "$1/etc/systemd/resolved.conf.d" "$1/etc/systemd/journald.conf.d"
install -m 0644 "$HERE/50-nas-revival-resolved.conf" "$1/etc/systemd/resolved.conf.d/50-nas-revival.conf"
install -m 0644 "$HERE/50-nas-revival-journald.conf" "$1/etc/systemd/journald.conf.d/50-nas-revival.conf"
systemctl --root="$1" enable systemd-networkd systemd-resolved systemd-timesyncd ssh nsa310-fan nsa310-leds
rm -f "$1"/etc/ssh/ssh_host_*_key*   # regenerated on first boot (sshd-keygen), never shared between images' \
  --customize-hook='set -e; printf "Types: deb\nURIs: http://deb.debian.org/debian\nSuites: trixie trixie-updates\nComponents: main non-free-firmware\n\nTypes: deb\nURIs: http://security.debian.org/debian-security\nSuites: trixie-security\nComponents: main non-free-firmware\n" > "$1/etc/apt/sources.list.d/debian.sources"; rm -f "$1/etc/apt/sources.list"' \
  trixie "$OUT" http://deb.debian.org/debian
sha256sum "$OUT" > "$OUT.sha256"
ls -lh "$OUT"
