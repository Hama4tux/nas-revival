#!/usr/bin/env bash
# End-to-end check of the File Station (W4.1) on the deployed NAS: builds backend/cmd/e2e-files for ARMv5,
# runs it as nas-revival with -keep, checks owner, group and mode of the created entries as root, then
# removes the throw-away users and share. Usage: e2e-files.sh [host] [big-upload-MiB, default 0 = skip; a big upload loads the NAS CPU, ask first].
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
big="${2:-0}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-files" ./cmd/e2e-files)
scp -q "$tmp/e2e-files" "$host:/tmp/nr-e2e-files"
ssh "$host" "chmod 0755 /tmp/nr-e2e-files; rc=0
runuser -u nas-revival -- /tmp/nr-e2e-files -keep -big $big || rc=\$?
d=/srv/data/nrt-files
echo '--- on disk (root):'
stat -c '%U:%G %a %n' \"\$d/Složka\" \"\$d/Cíl\" \"\$d/.recycle\" \"\$d/.recycle/nrt-fu\" \"\$d/.recycle/nrt-fu/Cíl/renamed.bin\" || rc=1
ls -A \"\$d\" | grep -q '^.nas-upload-' && { echo 'FAIL temporary upload file left'; rc=1; }
runuser -u nas-revival -- /tmp/nr-e2e-files -cleanup || rc=1
rm -f /tmp/nr-e2e-files; exit \$rc"
