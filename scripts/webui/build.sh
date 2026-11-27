#!/usr/bin/env bash
# Build the web UI and the nas-revival binary for the NSA310 (ARMv5, soft-float).
# Output: output/webui/nas-revival-armv5 (+ SHA256SUMS). Needs Go (GO env or ~/sdk) and Node via nvm.
# Env: NR_VERSION (default git describe), OUT (default output/webui). The armv5 binary is reproducible:
# -trimpath, no build ID, the Go version from go.mod and the web UI from package-lock.json.
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
GO="${GO:-$(command -v go || ls -d "$HOME"/sdk/go*/bin/go 2>/dev/null | sort -V | tail -1)}"
[ -x "$GO" ] || { echo "Go not found; set GO=/path/to/go" >&2; exit 1; }

if ! command -v npm >/dev/null; then
  # shellcheck disable=SC1091
  source "$HOME/.nvm/nvm.sh"
fi

cd "$repo/frontend"
npm ci --no-audit --no-fund
npm run typecheck
npm test
npm run build

dist="$repo/backend/internal/web/dist"
rm -rf "$dist"
cp -r "$repo/frontend/dist" "$dist"

cd "$repo/backend"
"$GO" vet ./...
"$GO" test ./...
out="$(realpath -m "${OUT:-$repo/output/webui}")"
version="${NR_VERSION:-$(git -C "$repo" describe --always --dirty 2>/dev/null || echo dev)}"
mkdir -p "$out"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 "$GO" build -tags embedui -trimpath \
  -ldflags "-s -w -buildid= -X main.version=$version" -o "$out/nas-revival-armv5" ./cmd/nas-revival
CGO_ENABLED=0 "$GO" build -tags embedui -trimpath -o "$out/nas-revival-host" ./cmd/nas-revival
(cd "$out" && sha256sum nas-revival-armv5 > SHA256SUMS)
ls -l "$out"
