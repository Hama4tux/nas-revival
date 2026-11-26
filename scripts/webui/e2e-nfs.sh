#!/usr/bin/env bash
# End-to-end check of NFS (W2.6) on the deployed NAS with a real NFS client.
# Usage: scripts/webui/e2e-nfs.sh <client ssh target> [nas ssh alias, default nsa310]
# The client needs mount.nfs4 and root or passwordless sudo (the NAS kernel has an NFS server but no
# client, so it cannot mount itself). Through the root helper socket (as nas-revival, like the web UI)
# it creates the throw-away share nrt-nfs, exports it to the client's address and checks: only TCP 2049
# listens (no rpcbind), NFSv4.2 mount, root_squash writes as nobody:nas-nfs, NFSv3 and v4.0 refused,
# connected client list, read-only client, a client not in the list is refused, an exported share cannot
# be deleted. Finally restores the previous NFS settings and removes the share. Exit 0 = all checks passed.
set -euo pipefail
client="${1:?usage: e2e-nfs.sh <client ssh target> [nas]}"
host="${2:-nsa310}"
nasaddr="$(ssh -G "$host" | awk '$1 == "hostname" { print $2 }')"
sudo="sudo -n"
[ "$(ssh "$client" id -u)" = 0 ] && sudo=""
cli() { ssh "$client" "$sudo sh -c '$1'"; }
caddr="$(ssh "$client" "ip -4 route get $nasaddr" | awk '{ for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1) }')"
[ -n "$caddr" ] || { echo "cannot find the client address" >&2; exit 1; }
fails=0
ok() { echo "ok   $*"; }
bad() { echo "FAIL $*"; fails=$((fails + 1)); }

# call <op> <json args>: one helper request as nas-revival; prints the JSON reply.
call() {
	ssh "$host" "LC_ALL=C runuser -u nas-revival -- perl -MIO::Socket::UNIX -e '
		my \$s = IO::Socket::UNIX->new(Peer => q(/run/nas-revival-helper/helper.sock)) or die qq(socket: \$!\n);
		print \$s \$ARGV[0]; shutdown(\$s, 1); local \$/; print scalar <\$s>;' '{\"op\":\"$1\",\"user\":\"e2e-nfs\",\"args\":$2}'"
}
nas() { ssh "$host" "$@"; }
has() { grep -q -- "$2" <<<"$1"; }

prev="$(nas 'cat /etc/nas-revival/nfs.json 2>/dev/null || echo "{\"version\":1,\"enabled\":false,\"exports\":[]}"')"
prev="$(tr -d '\n' <<<"$prev")"
cleanup() {
	cli 'umount -f /mnt/nrt-nfs 2>/dev/null; rmdir /mnt/nrt-nfs 2>/dev/null' || true
	call nfs.set "{\"settings\":$prev}" >/dev/null || true
	call share.delete '{"name":"nrt-nfs","deleteData":true,"confirmName":"nrt-nfs"}' >/dev/null || true
}
trap cleanup EXIT

export_to() { # export_to <host> <readOnly true|false>
	call nfs.set "{\"settings\":{\"version\":1,\"enabled\":true,\"exports\":[{\"share\":\"nrt-nfs\",\"squash\":\"root\",\"clients\":[{\"host\":\"$1\",\"readOnly\":$2}]}]}}"
}
mnt() { cli "mkdir -p /mnt/nrt-nfs; timeout 20 mount -t nfs4 -o $1 $nasaddr:/srv/data/nrt-nfs /mnt/nrt-nfs 2>&1 && echo MOUNTED" || true; }
umnt() { cli 'umount /mnt/nrt-nfs 2>/dev/null || true'; }

r="$(call share.create '{"share":{"name":"nrt-nfs","volume":"/srv/data","permissions":[{"kind":"group","name":"nas-nfs","access":"rw"}]}}')"
has "$r" '"ok":true' && ok "share nrt-nfs created" || bad "share.create: $r"

r="$(export_to "$caddr" false)"
has "$r" '"running":true' && has "$r" '"managed":true' && ok "NFS enabled and exports applied by the helper" || bad "nfs.set: $r"
ports="$(nas "ss -Htlnu | awk '{print \$5}' | grep -E ':(111|2049|20048)\$' | sort -u | tr '\n' ' '")"
has "$ports" ':2049' && ! has "$ports" ':111' && ! has "$ports" ':20048' && ok "listening only on 2049 ($ports)" || bad "ports: $ports"
has "$(nas 'exportfs -v')" '/srv/data/nrt-nfs' && ok "exportfs lists the export" || bad "exportfs -v"

has "$(mnt vers=4.2)" MOUNTED && ok "NFSv4.2 mount" || bad "NFSv4.2 mount failed"
cli 'touch /mnt/nrt-nfs/f' || true
own="$(nas 'stat -c %U:%G /srv/data/nrt-nfs/f 2>&1' || true)"
[ "$own" = "nobody:nas-nfs" ] && ok "root_squash: client root writes as $own" || bad "owner of a root write: $own"
r="$(call nfs.status null)"
has "$r" "\"address\":\"$caddr\"" && ok "client listed in nfs.status" || bad "nfs.status: $r"
umnt
has "$(mnt vers=3)" MOUNTED && { bad "NFSv3 mount accepted"; umnt; } || ok "NFSv3 refused"
has "$(mnt vers=4.0)" MOUNTED && { bad "NFSv4.0 mount accepted"; umnt; } || ok "NFSv4.0 refused"

export_to "$caddr" true >/dev/null
if has "$(mnt vers=4.2)" MOUNTED; then
	cli 'touch /mnt/nrt-nfs/g 2>/dev/null' && bad "write on a read-only client" || ok "read-only client cannot write"
	umnt
else bad "read-only mount failed"; fi

export_to 192.0.2.1 false >/dev/null
has "$(mnt vers=4.2)" MOUNTED && { bad "client not in the list could mount"; umnt; } || ok "client not in the list refused"

r="$(call share.delete '{"name":"nrt-nfs"}')"
has "$r" 'NFS' && ok "exported share cannot be deleted" || bad "share.delete of an exported share: $r"

r="$(call nfs.set "{\"settings\":$prev}")"
has "$r" '"ok":true' && ok "previous NFS settings restored" || bad "restore: $r"
r="$(call share.delete '{"name":"nrt-nfs","deleteData":true,"confirmName":"nrt-nfs"}')"
has "$r" '"ok":true' && ok "share removed" || bad "share.delete: $r"
trap - EXIT
cli 'rmdir /mnt/nrt-nfs 2>/dev/null || true'
echo "== $fails failure(s)"
[ "$fails" -eq 0 ]
