#!/bin/sh
# Installs the NFS server for the NFS service (W2.6) without starting it. Run on the NAS as root.
# Only NFSv4.1/4.2 over TCP 2049 is served: rpcbind, rpc-statd and the NFSv3 ports stay off.
# Needs a kernel with CONFIG_NFSD (scripts/debian/build-kernel.sh since W2.6); the package
# installs without it, but nfs-server will not start.
set -eu

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
conf=/etc/nfs.conf.d/nas-revival.conf

if ! dpkg-query -W -f='${Status}' nfs-kernel-server 2>/dev/null | grep -q 'install ok installed'; then
	# policy-rc.d keeps maintainer scripts from starting nfs-server and rpcbind during the install.
	policy=/usr/sbin/policy-rc.d
	if [ -e "$policy" ]; then
		echo "$policy already exists; not touching it" >&2
		exit 1
	fi
	printf '#!/bin/sh\nexit 101\n' >"$policy"
	chmod 0755 "$policy"
	trap 'rm -f "$policy"' EXIT
	DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nfs-kernel-server
fi

mkdir -p /etc/nfs.conf.d /etc/exports.d
cat >"$conf" <<'CONF'
# Installed by NAS Revival (deploy/debian/install-nfs.sh). NFSv4.1/4.2 only: TCP 2049, no rpcbind.
# Four server threads: the NAS has 256 MiB RAM and one CPU core.
[nfsd]
threads=4
udp=n
vers3=n
vers4.0=n
vers4.1=y
vers4.2=y
CONF
chmod 0644 "$conf"

systemctl disable --now nfs-server.service
# NFSv4 needs neither rpcbind (port 111) nor statd; masking keeps nfs-server's Wants= from starting them.
systemctl mask --now rpcbind.service rpcbind.socket rpc-statd.service rpc-statd-notify.service
echo "nfs-kernel-server installed; nfs-server stays disabled until NFS is enabled in the web UI"
