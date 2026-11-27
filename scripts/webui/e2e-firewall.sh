#!/usr/bin/env bash
# End-to-end check of the firewall (W5.2, ADR 0014) on the deployed NAS (ssh host alias, default nsa310).
# Builds backend/cmd/e2e-firewall for ARMv5, runs it on the NAS as nas-revival (helper socket) and checks the
# effect from this workstation: web 8443 and SSH stay reachable, SMB 445 is blocked while the firewall is on.
#   1. a change that would lock this workstation out is refused;
#   2. enable without confirming → rules active, boot file untouched → rolled back after 180 s;
#   3. enable + confirm → boot file written; 4. disable + confirm → table and boot file gone.
# Keeps NFS open to everyone during the test (Proxmox backups). Takes about 4 minutes. Exit 0 = all passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-firewall" ./cmd/e2e-firewall)
scp -q "$tmp/e2e-firewall" "$host:/tmp/nr-e2e-firewall"
ssh "$host" chmod 0755 /tmp/nr-e2e-firewall

nas="$(ssh -G "$host" | awk '$1 == "hostname" {print $2}')"
client="$(ssh "$host" 'echo "${SSH_CLIENT%% *}"')"
lan="$(ssh "$host" "ip -4 -o route show dev enp1s0 scope link | awk '{print \$1; exit}'")"
fw() { ssh "$host" runuser -u nas-revival -- /tmp/nr-e2e-firewall "$@"; }
root() { ssh "$host" "$@"; }
fails=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fails=$((fails + 1)); fi; }
port() { if timeout 3 bash -c "exec 3<>/dev/tcp/$nas/$1" 2>/dev/null; then echo open; else echo closed; fi; }
bootfile() { root 'test -e /etc/nftables.d/nas-revival.nft && echo present || echo absent'; }

check "start: firewall off"         "$(fw get)" "supported=true active=false enabled=false pending=false"
[ "$(root 'smbstatus -b 2>/dev/null | tail -n +5 | grep -c .' || true)" = 0 ] || { echo "SMB clients connected – not blocking SMB now" >&2; exit 1; }
check "start: SMB reachable"        "$(port 445)" "open"
check "lockout refused"             "$(fw lockout "$client" "$lan" | cut -c1-8)" "refused:"

check "enable (unconfirmed)"        "$(fw enable "$client" "$lan")" "supported=true active=true enabled=false pending=true"
check "web reachable"               "$(port 8443)" "open"
check "new SSH connection works"    "$(ssh -o ConnectTimeout=5 "$host" echo yes)" "yes"
check "SMB blocked"                 "$(port 445)" "closed"
check "boot file not written yet"   "$(bootfile)" "absent"
echo "     waiting 185 s for the rollback…"; sleep 185
check "rolled back"                 "$(fw get)" "supported=true active=false enabled=false pending=false"
check "SMB reachable again"         "$(port 445)" "open"

fw enable "$client" "$lan" >/dev/null
check "confirm"                     "$(fw confirm)" "supported=true active=true enabled=true pending=false"
check "boot file written"           "$(bootfile)" "present"
check "boot file loads"             "$(root 'nft -c -f /etc/nftables.d/nas-revival.nft && echo ok')" "ok"
check "SMB blocked after confirm"   "$(port 445)" "closed"

fw disable "$client" >/dev/null
check "disable confirmed"           "$(fw confirm)" "supported=true active=false enabled=false pending=false"
check "boot file removed"           "$(bootfile)" "absent"
check "SMB reachable at the end"    "$(port 445)" "open"

root rm -f /tmp/nr-e2e-firewall
[ $fails -eq 0 ] && echo "all checks passed" || { echo "$fails check(s) failed"; exit 1; }
