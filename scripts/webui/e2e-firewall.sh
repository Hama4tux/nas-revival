#!/usr/bin/env bash
# End-to-end check of the firewall (W5.2, ADR 0014) on the deployed NAS (ssh host alias, default nsa310).
# Builds backend/cmd/e2e-firewall for ARMv5, runs it on the NAS as nas-revival (helper socket) and checks the
# effect from this workstation: web 8443 and SSH stay reachable, SMB 445 is blocked while the firewall is on.
#   1. a change that would lock this workstation out is refused;
#   2. enable without confirming → rules active, boot file untouched → rolled back after 180 s;
#   3. enable + confirm → boot file written; 4. rule table: a drop rule that would cut this workstation off is
#      refused, an accept rule opens SMB, a drop rule closes SMB for this workstation only;
#   5. disable + confirm → table and boot file gone.
# Runs only while the firewall is off, keeps NFS open to everyone (Proxmox backups) and restores the stored
# settings (lists, rules) at the end. Takes about 4 minutes. Exit 0 = all passed.
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

# The helper opens its socket only after applying the disk settings (up to ~35 s after a restart).
for _ in $(seq 30); do fw get >/dev/null 2>&1 && break; sleep 2; done
check "start: firewall off"         "$(fw get)" "supported=true active=false enabled=false pending=false"
[ "$(fw get)" = "supported=true active=false enabled=false pending=false" ] || { echo "firewall is on or pending – not testing over it" >&2; exit 1; }
# The owner's settings (lists, rules) are put back at the end, also when a check fails.
saved="$(fw save)"
restore() { fw restore "$client" "$saved" >/dev/null || echo "WARNING: could not restore the firewall settings: $saved" >&2; }
trap 'restore; rm -rf "$tmp"' EXIT
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

check "lockout by a drop rule refused" "$(fw lockoutrule "$client" "$lan" | cut -c1-8)" "refused:"
fw rule "$client" "$lan" >/dev/null; fw confirm >/dev/null
check "rule: SMB allowed by an accept rule" "$(port 445)" "open"
check "rule: web reachable"         "$(port 8443)" "open"
fw dropclient "$client" "$lan" >/dev/null; fw confirm >/dev/null
check "drop rule: SMB closed for this PC although the LAN may use it" "$(port 445)" "closed"
check "drop rule: web reachable"    "$(port 8443)" "open"
check "listening ports reported"    "$(fw get ports | grep -c '^blocked=')" "1"

fw disable "$client" >/dev/null
check "disable confirmed"           "$(fw confirm)" "supported=true active=false enabled=false pending=false"
check "boot file removed"           "$(bootfile)" "absent"
check "SMB reachable at the end"    "$(port 445)" "open"

restore; trap 'rm -rf "$tmp"; root rm -f /tmp/nr-e2e-firewall' EXIT
check "owner settings restored"     "$(fw save)" "$saved"
[ $fails -eq 0 ] && echo "all checks passed" || { echo "$fails check(s) failed"; exit 1; }
