#!/bin/bash
# Tests for scripts/release/release.sh and sign-release.sh (version check, manifest, build comparison,
# signing) with throwaway keys. Run: bash tests/test_release.sh
set -uo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
RELEASE_LIB=1 . "$here/scripts/release/release.sh"
SIGN_RELEASE_LIB=1 . "$here/scripts/release/sign-release.sh"
set +e
fail=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }
rc() { "$@" >/dev/null 2>&1; echo $?; }

for v in 2026.11.27 2026.01.05.2 2030.12.31.999; do check "version $v valid" "$(rc valid_version "$v")" 0; done
for v in v2026.11.27 2026.11.7 2026.13.01 2026.11.32 2026.11.27.0 2026.11.27.1000 "2026.11.27 " 1.2.3 ""; do
  check "version '$v' rejected" "$(rc valid_version "$v")" 1
done

# Fake release directory with the four artifacts.
mk() {
  rm -rf "$1"; mkdir -p "$1"
  echo bin > "$1/nas-revival-armv5"; echo img > "$1/uImage"
  echo mods > "$1/modules-6.12.119-nr1.tar.gz"; echo cfg > "$1/kernel-6.12.119-nr1.config"
}
mk "$work/a"
write_manifest "$work/a" 2026.11.27 abc123 1764240000 6.12.119-nr1
m="$work/a/manifest.json"
check "manifest version line" "$(grep -c '^  "version": "2026.11.27",$' "$m")" 1
check "manifest board" "$(grep -c '"board": "zyxel,nsa310a"' "$m")" 1
check "manifest has four artifacts" "$(grep -c '"sha256": "[0-9a-f]\{64\}"' "$m")" 4
check "manifest uImage hash" "$(grep -c "\"name\": \"uImage\", \"type\": \"kernel\", \"size\": 4, \"sha256\": \"$(sha256sum "$work/a/uImage" | cut -d' ' -f1)\"" "$m")" 1
if command -v python3 >/dev/null; then
  check "manifest is valid JSON" "$(rc python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$m")" 0
fi
mk "$work/b"; write_manifest "$work/b" 2026.11.27 abc123 1764240000 6.12.119-nr1
check "manifest is reproducible" "$(rc cmp "$m" "$work/b/manifest.json")" 0

check "manifest matches version and commit" "$(rc manifest_matches "$m" 2026.11.27 abc123)" 0
check "manifest for other version refused" "$(rc manifest_matches "$m" 2026.11.28 abc123)" 1
check "manifest for other commit refused" "$(rc manifest_matches "$m" 2026.11.27 abc12)" 1
write_manifest "$work/b" 2026.11.27 abc123-dirty 1764240000 6.12.119-nr1
check "dirty manifest refused" "$(rc manifest_matches "$work/b/manifest.json" 2026.11.27 abc123)" 1

sums() { (cd "$1" && sha256sum manifest.json nas-revival-armv5 uImage modules-6.12.119-nr1.tar.gz kernel-6.12.119-nr1.config > SHA256SUMS); }
mk "$work/b"; write_manifest "$work/b" 2026.11.27 abc123 1764240000 6.12.119-nr1; sums "$work/a"; sums "$work/b"
check "identical builds accepted" "$(rc same_build "$work/a" "$work/b")" 0
echo other > "$work/b/uImage"; sums "$work/b"
check "different uImage refused" "$(rc same_build "$work/a" "$work/b")" 1
echo img > "$work/b/uImage"; sums "$work/b"; echo tampered > "$work/b/nas-revival-armv5"
check "file not matching its SHA256SUMS refused" "$(rc same_build "$work/a" "$work/b")" 1
rm "$work/b/SHA256SUMS"
check "missing SHA256SUMS refused" "$(rc same_build "$work/a" "$work/b")" 1

if command -v openssl >/dev/null; then
  openssl genpkey -algorithm ed25519 -out "$work/k" 2>/dev/null; openssl pkey -in "$work/k" -pubout -out "$work/k.pub"
  openssl genpkey -algorithm ed25519 -out "$work/o" 2>/dev/null; openssl pkey -in "$work/o" -pubout -out "$work/o.pub"
  check "sign and verify" "$(rc sign_manifest "$m" "$work/k" "$work/k.pub")" 0
  check "signature is 64 bytes" "$(stat -c %s "$m.sig")" 64
  check "signature from another key refused" "$(rc sign_manifest "$m" "$work/o" "$work/k.pub")" 1
  check "no signature file left after a failed check" "$([ -e "$m.sig" ] && echo yes || echo no)" no
  sign_manifest "$m" "$work/k" "$work/k.pub" >/dev/null 2>&1
  sed -i 's/2026.11.27/2026.11.28/' "$m"
  check "changed manifest fails verification" \
    "$(rc openssl pkeyutl -verify -rawin -pubin -inkey "$work/k.pub" -in "$m" -sigfile "$m.sig")" 1
  check "repo public keys are Ed25519" \
    "$(openssl pkey -pubin -in "$here/deploy/release-keys/release.pub" -noout -text 2>/dev/null | head -1 | cut -c1-15)" "ED25519 Public-"
fi

exit $fail
