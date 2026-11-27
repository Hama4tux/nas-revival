#!/usr/bin/env bash
# End-to-end check of the Debian Update Manager (W5.4, ADR 0016) on the deployed NAS (ssh host alias, default nsa310).
# Builds backend/cmd/e2e-updates for ARMv5 and runs it on the NAS as nas-revival (helper socket):
#   1. a check starts nas-revival-updates@check.service; a second start while it runs is refused;
#   2. the check finishes without error, state.json is 0644 root and the list has a time;
#   3. an upgrade with a stale or malformed list time is refused – nothing is ever installed;
#   4. the daily check timer is enabled.
# Changes only the package lists (apt-get update) and state.json. Exit 0 = all passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-updates" ./cmd/e2e-updates)
scp -q "$tmp/e2e-updates" "$host:/tmp/nr-e2e-updates"
ssh "$host" chmod 0755 /tmp/nr-e2e-updates

up() { ssh "$host" runuser -u nas-revival -- /tmp/nr-e2e-updates "$@"; }
fails=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fails=$((fails + 1)); fi; }
field() { sed -nE "s/.*\\b$1=([^ ]*).*/\\1/p" <<<"$2"; }

# The helper opens its socket ~35 s after a restart: wait for it.
for _ in $(seq 30); do up get >/dev/null 2>&1 && break; sleep 2; done

# 1. start a check and refuse a second one while it runs
st="$(up get)"
check "helper answers"  "${st%% *}" "running="
check "check started"   "$(up check)" "started"
for _ in $(seq 20); do [ "$(field running "$(up get)")" = check ] && break; sleep 0.5; done
check "check running"   "$(field running "$(up get)")" "check"
check "second refused"  "$(up check)" "updates_running"

# 2. wait for the end (apt-get update + simulation, about a minute on the NAS)
for _ in $(seq 150); do [ -z "$(field running "$(up get)")" ] && break; sleep 2; done
st="$(up get)"
echo "     $st"
check "check finished"  "$(field running "$st")" ""
check "checked"         "$(field checked "$st")" "true"
check "no check error"  "$(field error "$st")" "false"
check "state.json mode" "$(ssh "$host" "stat -c '%a %U' /var/lib/nas-revival-updates/state.json")" "644 root"
listed="$(field listed "$st")"
[ -n "$listed" ] && echo "ok   list time $listed" || { echo "FAIL list time missing"; fails=$((fails + 1)); }

# 3. never install: stale and malformed list times
check "stale list refused"     "$(up upgrade 2001-01-01T00:00:00Z)" "updates_stale"
check "malformed time refused" "$(up upgrade yesterday)" "invalid"
check "no upgrade ran"         "$(ssh "$host" systemctl is-active nas-revival-updates@upgrade.service || true)" "inactive"

# 4. daily check
check "timer enabled" "$(ssh "$host" systemctl is-enabled nas-revival-updates-check.timer)" "enabled"

ssh "$host" rm -f /tmp/nr-e2e-updates
[ "$fails" -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
