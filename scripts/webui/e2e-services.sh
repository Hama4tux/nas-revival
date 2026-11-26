#!/usr/bin/env bash
# End-to-end check of the Services app (W2.4) on the deployed NAS: builds backend/cmd/e2e-services for ARMv5,
# copies it to the NAS (ssh host alias, default nsa310), runs it as nas-revival and removes it.
# Restarts cron.service once (harmless); never stops anything. Exit code 0 = all checks passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-services" ./cmd/e2e-services)
scp -q "$tmp/e2e-services" "$host:/tmp/nr-e2e-services"
ssh "$host" 'chmod 0755 /tmp/nr-e2e-services; rc=0; runuser -u nas-revival -- /tmp/nr-e2e-services || rc=$?; rm -f /tmp/nr-e2e-services; exit $rc'
