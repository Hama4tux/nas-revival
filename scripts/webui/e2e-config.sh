#!/usr/bin/env bash
# End-to-end check of config export/import (W4.4) on the deployed NAS: builds backend/cmd/e2e-config for ARMv5,
# copies it to the NAS (ssh host alias, default nsa310), runs it as nas-revival and removes it.
# Imports an extra NTP server and then the original export again (two import versions stay in the history). Exit code 0 = all checks passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-config" ./cmd/e2e-config)
scp -q "$tmp/e2e-config" "$host:/tmp/nr-e2e-config"
ssh "$host" 'chmod 0755 /tmp/nr-e2e-config; rc=0; runuser -u nas-revival -- /tmp/nr-e2e-config || rc=$?; rm -f /tmp/nr-e2e-config; exit $rc'
