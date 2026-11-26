#!/bin/sh
# Installs Samba for the SMB service (W2.5) without starting it. Run on the NAS as root.
# Debian enables smbd/nmbd on install; they stay disabled until SMB is switched on in the
# web UI, which writes /etc/samba/smb.conf (helper op smb.set) and starts smbd.
set -eu

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
if dpkg-query -W -f='${Status}' samba 2>/dev/null | grep -q 'install ok installed'; then
	echo "samba is already installed"
	exit 0
fi

# policy-rc.d keeps maintainer scripts from starting smbd/nmbd with Debian's
# default smb.conf (shares [homes] and printers) during the install.
policy=/usr/sbin/policy-rc.d
if [ -e "$policy" ]; then
	echo "$policy already exists; not touching it" >&2
	exit 1
fi
printf '#!/bin/sh\nexit 101\n' >"$policy"
chmod 0755 "$policy"
trap 'rm -f "$policy"' EXIT

DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends samba

systemctl disable --now smbd.service nmbd.service
# Keep Debian's template for reference; the helper owns smb.conf from now on.
if [ -f /etc/samba/smb.conf ] && [ ! -e /etc/samba/smb.conf.debian ]; then
	cp -p /etc/samba/smb.conf /etc/samba/smb.conf.debian
fi
echo "samba installed; smbd and nmbd are disabled until SMB is enabled in the web UI"
