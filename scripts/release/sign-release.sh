#!/bin/bash
# Sign a release after checking that the local rebuild gives the same bytes as CI (ADR 0017, docs/RELEASE.md).
#   1. the local build output/release/<version>/ (scripts/release/release.sh on the tag) must exist;
#   2. the CI artifacts are downloaded from the draft release v<version> (or taken from <ci-dir>);
#   3. both SHA256SUMS must be identical and every file must match them, the manifest must name the version
#      and the commit of tag v<version>;
#   4. manifest.json is signed with the private key (Ed25519, OpenSSL) and the signature checked against
#      deploy/release-keys/release.pub. Upload and publish stay a manual step (commands are printed).
# Usage: scripts/release/sign-release.sh <version> [ci-dir]
# Env:   NR_RELEASE_KEY (default ~/.secrets/nas-revival/release.key)
# Tests: tests/test_release.sh sources it with SIGN_RELEASE_LIB=1 (functions only).
set -euo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
KEY="${NR_RELEASE_KEY:-$HOME/.secrets/nas-revival/release.key}"
PUB="$REPO/deploy/release-keys/release.pub"

die() { echo "$*" >&2; exit 1; }

# same_build LOCAL CI: both directories hold the same SHA256SUMS and every listed file matches it.
same_build() {
  [ -f "$1/SHA256SUMS" ] && [ -f "$2/SHA256SUMS" ] || { echo "SHA256SUMS missing" >&2; return 1; }
  if ! cmp -s "$1/SHA256SUMS" "$2/SHA256SUMS"; then
    echo "local and CI builds differ:" >&2
    diff "$1/SHA256SUMS" "$2/SHA256SUMS" >&2 || true
    if [ -f "$1/BUILDINFO" ] && [ -f "$2/BUILDINFO" ]; then
      echo "toolchain (local < > CI):" >&2; diff "$1/BUILDINFO" "$2/BUILDINFO" >&2 || true
    fi
    return 1
  fi
  (cd "$1" && sha256sum -c --quiet SHA256SUMS) || return 1
  (cd "$2" && sha256sum -c --quiet SHA256SUMS) || return 1
}

# manifest_matches MANIFEST VERSION COMMIT: the manifest is for this version and this clean commit.
manifest_matches() {
  grep -Fqx "  \"version\": \"$2\"," "$1" || { echo "manifest is not for version $2" >&2; return 1; }
  grep -Fqx "  \"commit\": \"$3\"," "$1" || { echo "manifest commit is not $3 (dirty or other commit)" >&2; return 1; }
}

# sign_manifest MANIFEST KEY PUB: write MANIFEST.sig (64-byte raw Ed25519) and verify it with PUB.
sign_manifest() {
  # Explicit returns: callers may run without set -e, and a failed check must never leave a usable .sig.
  openssl pkeyutl -sign -rawin -inkey "$2" -in "$1" -out "$1.sig" || { rm -f "$1.sig"; return 1; }
  if ! openssl pkeyutl -verify -rawin -pubin -inkey "$3" -in "$1" -sigfile "$1.sig" >/dev/null \
    || [ "$(stat -c %s "$1.sig")" -ne 64 ]; then
    echo "signature does not verify with $3" >&2; rm -f "$1.sig"; return 1
  fi
}

[ "${SIGN_RELEASE_LIB:-}" = 1 ] && return 0

VER="${1:-}"; [ -n "$VER" ] || die "usage: $0 <version> [ci-dir]"
LOCAL="$REPO/output/release/$VER"
[ -d "$LOCAL" ] || die "no local build in $LOCAL; run scripts/release/release.sh $VER on tag v$VER first"
[ -f "$KEY" ] || die "private key $KEY not found"
COMMIT="$(git -C "$REPO" rev-parse --verify "v$VER^{commit}")" || die "tag v$VER not found"

CI="${2:-}"
if [ -z "$CI" ]; then
  CI="$REPO/output/release/$VER-ci"
  rm -rf "$CI"; mkdir -p "$CI"
  gh release download "v$VER" --repo "$(git -C "$REPO" remote get-url origin)" --dir "$CI" \
    --pattern SHA256SUMS --pattern BUILDINFO --pattern manifest.json --pattern 'nas-revival-armv5' \
    --pattern uImage --pattern 'modules-*.tar.gz' --pattern 'kernel-*.config'
fi

same_build "$LOCAL" "$CI" || die "not signed: the local rebuild does not match CI"
manifest_matches "$LOCAL/manifest.json" "$VER" "$COMMIT" || die "not signed"
sign_manifest "$LOCAL/manifest.json" "$KEY" "$PUB"
echo "== signed: $LOCAL/manifest.json.sig (verified with ${PUB#"$REPO"/})"
echo "Upload and publish:"
echo "  gh release upload v$VER '$LOCAL/manifest.json.sig'"
echo "  gh release edit v$VER --draft=false"
