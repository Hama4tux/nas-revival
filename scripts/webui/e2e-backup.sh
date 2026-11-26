#!/usr/bin/env bash
# End-to-end check of the backup helper ops (W4.3) on the deployed NAS: builds backend/cmd/e2e-backup for ARMv5,
# copies it to the NAS (ssh host alias, default nsa310), runs it as nas-revival and removes it.
# Key, share state, host key scan, connection test, versions, download of every backup-pull task; restores one small
# file into a throw-away share of a throw-away account (nrt-br / nrt-restore) and removes both again.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-backup" ./cmd/e2e-backup)
scp -q "$tmp/e2e-backup" "$host:/tmp/nr-e2e-backup"
ssh "$host" 'chmod 0755 /tmp/nr-e2e-backup; rc=0; runuser -u nas-revival -- /tmp/nr-e2e-backup || rc=$?; rm -f /tmp/nr-e2e-backup; exit $rc'
