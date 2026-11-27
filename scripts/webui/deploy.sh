#!/usr/bin/env bash
# Install a web UI build (scripts/webui/build.sh) on the NAS and restart the web service.
# Usage: scripts/webui/deploy.sh [ssh-host]   (default nsa310; root over SSH)
# Keeps the running binary as /usr/local/bin/nas-revival.prev; when the service does not come back
# within 30 s the previous binary is put back. The helper is not restarted (its binary is the same
# file, the running helper keeps the old one until its own restart).
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
host="${1:-nsa310}"
out="$repo/output/webui"
bin="$out/nas-revival-armv5"
[ -f "$bin" ] || { echo "missing $bin – run scripts/webui/build.sh first" >&2; exit 1; }
(cd "$out" && sha256sum -c --quiet SHA256SUMS)
sum="$(cut -d' ' -f1 "$out/SHA256SUMS")"

scp -q "$bin" "$host:/usr/local/bin/nas-revival.new"
ssh "$host" bash -s -- "$sum" <<'EOF'
set -euo pipefail
sum="$1"; dir=/usr/local/bin
echo "$sum  $dir/nas-revival.new" | sha256sum -c --quiet
chmod 0755 "$dir/nas-revival.new"
cp -p "$dir/nas-revival" "$dir/nas-revival.prev"
mv -f "$dir/nas-revival.new" "$dir/nas-revival"
systemctl restart nas-revival
for _ in $(seq 30); do
  if systemctl is-active --quiet nas-revival && curl -ksf -o /dev/null https://127.0.0.1:8443/; then
    echo "nas-revival is up (sha256 ${sum:0:12})"; exit 0
  fi
  sleep 1
done
echo "nas-revival did not come up, restoring the previous binary" >&2
journalctl -u nas-revival -n 20 --no-pager >&2 || true
cp -p "$dir/nas-revival.prev" "$dir/nas-revival"
systemctl restart nas-revival
exit 1
EOF
