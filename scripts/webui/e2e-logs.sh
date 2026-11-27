#!/usr/bin/env bash
# End-to-end check of the Log Center (W5.3, ADR 0015) on the deployed NAS (ssh host alias, default nsa310).
# Builds backend/cmd/e2e-logs for ARMv5 and runs it on the NAS as nas-revival (helper socket):
#   1. every category answers, kernel/nas/services/auth are not empty, a text search without a match is empty;
#   2. invalid queries and retention values are refused;
#   3. an audit entry written by another process (not the nas-revival units) carries the audit fields in the
#      journal but does not appear in the audit category (trusted _SYSTEMD_UNIT match);
#   4. retention: a change is written to the journald drop-in and journald restarts; the old values are put
#      back at the end.
# Read-only apart from step 3 (one journal entry) and step 4 (journald restarted twice). Exit 0 = all passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-logs" ./cmd/e2e-logs)
scp -q "$tmp/e2e-logs" "$host:/tmp/nr-e2e-logs"
ssh "$host" chmod 0755 /tmp/nr-e2e-logs

lg() { ssh "$host" runuser -u nas-revival -- /tmp/nr-e2e-logs "$@"; }
fails=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fails=$((fails + 1)); fi; }
nonempty() { case "$2" in n=0\ *) echo "FAIL $1: empty ($2)"; fails=$((fails + 1)) ;; n=*) echo "ok   $1 ($2)" ;; *) echo "FAIL $1: $2"; fails=$((fails + 1)) ;; esac; }

# 1. categories
for c in all kernel auth nas services; do nonempty "category $c" "$(lg query "$c" 2>&1 || true)"; done
check "audit category answers" "$(lg query audit >/dev/null 2>&1 && echo yes || echo no)" "yes"
check "text without match"     "$(lg query all "zz-no-such-text-$RANDOM" 2>&1 || true)" "n=0 more=false audit=0"

# 2. validation
check "invalid input refused" "$(lg bad | sort -u)" "refused"

# 3. forged audit entry
marker="w53-forge-$RANDOM$RANDOM"
lg forge "$marker"
sleep 1
check "forged entry in journal with audit fields" "$(lg query all "$marker")" "n=1 more=false audit=1"
check "forged entry not in audit category"        "$(lg query audit "$marker")" "n=0 more=false audit=0"

# 4. retention
saved="$(lg retention)"
read -r use age <<<"$(sed -E 's/maxUse=([^ ]+) maxRetention=([^ ]+).*/\1 \2/' <<<"$saved")"
restore() { lg retention "$use" "$age" >/dev/null || echo "WARNING: could not restore retention $use $age" >&2; }
trap 'restore; rm -rf "$tmp"' EXIT
check "retention usage reported" "${saved##* }" "usage=true"
check "retention set"            "$(lg retention 128M 1month)" "maxUse=128M maxRetention=1month usage=true"
check "drop-in written"          "$(ssh "$host" "grep -cE '^(SystemMaxUse=128M|MaxRetentionSec=1month|SyncIntervalSec=15min)$' /etc/systemd/journald.conf.d/50-nas-revival.conf")" "3"
check "journald running"         "$(ssh "$host" systemctl is-active systemd-journald)" "active"
restore
trap 'rm -rf "$tmp"' EXIT
check "retention restored"       "$(lg retention)" "$saved"

ssh "$host" rm -f /tmp/nr-e2e-logs
[ "$fails" -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
