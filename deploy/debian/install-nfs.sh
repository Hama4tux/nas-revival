#!/bin/sh
# Installs the NFS server for the NFS service (W2.6) without starting it. Run on the NAS as root.
# Only NFSv4.1/4.2 over TCP 2049 is served; rpc-statd and the NFSv3 ports stay off. rpcbind listens on
# TCP 111 only, because Proxmox checks an NFSv4 storage with `rpcinfo -T tcp <nas> nfs 4`, which asks
# rpcbind; without it Proxmox reports the storage as not online.
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
	# A fresh install stays off; a re-run keeps the state the web UI set.
	systemctl disable --now nfs-server.service
fi

mkdir -p /etc/nfs.conf.d /etc/exports.d
cat >"$conf" <<'CONF'
# Installed by NAS Revival (deploy/debian/install-nfs.sh). NFSv4.1/4.2 only: TCP 2049 (+ rpcbind TCP 111).
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

# Squashed NFS clients write with this member-less group (anongid); shares grant it separately from SMB users.
getent group nas-nfs >/dev/null || groupadd --system nas-nfs
# NFSv4 needs no statd; masking keeps nfs-server's Wants= from starting it.
systemctl mask --now rpc-statd.service rpc-statd-notify.service
# rpcbind only for Proxmox's online check (see top): TCP IPv4 and the local socket, no UDP (reflection).
# An empty ListenStream= resets every listener of the packaged unit. Older installs masked rpcbind.
mkdir -p /etc/systemd/system/rpcbind.socket.d
cat >/etc/systemd/system/rpcbind.socket.d/nas-revival.conf <<'UNIT'
# Installed by NAS Revival (deploy/debian/install-nfs.sh): rpcbind on TCP only.
[Socket]
ListenStream=
ListenStream=/run/rpcbind.sock
ListenStream=0.0.0.0:111
UNIT
# rpcbind opens its own UDP socket besides the activated ones; -h binds it to loopback only.
printf '# Installed by NAS Revival (deploy/debian/install-nfs.sh)\nOPTIONS="-w -h 127.0.0.1"\n' >/etc/default/rpcbind
systemctl unmask rpcbind.service rpcbind.socket
systemctl daemon-reload
systemctl enable rpcbind.socket
systemctl stop rpcbind.service
systemctl restart rpcbind.socket
# nfsd registers with rpcbind only when it starts.
systemctl try-restart nfs-server.service
# The helper's sandbox (ProtectSystem=strict) only sees /var/lib/nfs as writable if it existed when the
# helper started; without a restart exportfs cannot write etab and every NFS change is refused.
systemctl try-restart nas-revival-helper.service
echo "nfs-kernel-server installed; nfs-server stays disabled until NFS is enabled in the web UI"
