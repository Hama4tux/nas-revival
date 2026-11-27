#!/bin/bash
# Generate the Ed25519 release signing keys (ADR 0017): main + backup.
# Private keys go to ~/.secrets/nas-revival/ (mode 600), never into the repo.
# Public keys are written to deploy/release-keys/ and are meant to be committed.
# Usage: scripts/release/gen-signing-key.sh   (refuses to overwrite existing keys)
set -euo pipefail
SECRETS="${NR_SECRETS_DIR:-$HOME/.secrets/nas-revival}"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
PUBDIR="$REPO/deploy/release-keys"
umask 077
install -d -m 700 "$SECRETS"
mkdir -p "$PUBDIR"
for name in release release-backup; do
  key="$SECRETS/$name.key"
  if [ -e "$key" ]; then echo "exists, skipped: $key" >&2; continue; fi
  openssl genpkey -algorithm ed25519 -out "$key"
  chmod 600 "$key"
  openssl pkey -in "$key" -pubout -out "$PUBDIR/$name.pub"
  chmod 644 "$PUBDIR/$name.pub"
  echo "generated $name: private $key, public ${PUBDIR#$REPO/}/$name.pub"
done
# self-test: sign and verify with the main key
t="$(mktemp -d)"; trap 'rm -f -- "$t/msg" "$t/sig"; rmdir -- "$t"' EXIT
echo test > "$t/msg"
openssl pkeyutl -sign -rawin -inkey "$SECRETS/release.key" -in "$t/msg" -out "$t/sig"
openssl pkeyutl -verify -rawin -pubin -inkey "$PUBDIR/release.pub" -in "$t/msg" -sigfile "$t/sig"
