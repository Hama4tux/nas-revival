#!/usr/bin/env bash
# End-to-end check of SSH/SFTP (W2.7) and user keys (W2.7b) on the deployed NAS: builds backend/cmd/e2e-ssh for ARMv5, copies it
# to the NAS (ssh host alias, default nsa310) and runs it as nas-revival. It creates throw-away accounts
# nrt-sa/su/sn and group nrt-sftp, changes the SSH settings over HTTPS (SFTP group, password login, port
# 2222 and back), logs in with a real SSH client on 127.0.0.1 and finally restores the previous SSH
# settings and removes the accounts. This ssh session stays open during the port change (sshd keeps
# existing connections on reload). Exit code 0 = all checks passed.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
(cd "$repo/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -trimpath -o "$tmp/e2e-ssh" ./cmd/e2e-ssh)
scp -q "$tmp/e2e-ssh" "$host:/tmp/nr-e2e-ssh"
ssh "$host" 'chmod 0755 /tmp/nr-e2e-ssh; rc=0; runuser -u nas-revival -- /tmp/nr-e2e-ssh || rc=$?; rm -f /tmp/nr-e2e-ssh; exit $rc'
