# Release process

A release is the input of UPD-02 (NAS Revival self-update) and UPD-04 (kernel to the NAND slots). It is built
reproducibly in GitHub Actions and again on the owner's workstation; only when both give the same bytes does
the owner sign it ([ADR 0017](adr/0017-update-signing-key.md)). CI never holds the signing key.

## Contents

`output/release/<version>/` (and the draft GitHub release `v<version>`):

| File | What |
|---|---|
| `nas-revival-armv5` | web UI + API binary (Go, `GOARM=5`, UI embedded) |
| `uImage` | kernel with the NSA310 device tree appended, for slot A |
| `modules-<release>.tar.gz` | modules, rooted at `usr/lib/modules/<release>/` (what `update-kernel.sh` accepts) |
| `kernel-<release>.config` | kernel configuration, for reference |
| `manifest.json` | schema 1: version, board `zyxel,nsa310a`, arch, commit, `source_date_epoch`, kernel release, name/type/size/sha256 of every artifact |
| `manifest.json.sig` | raw 64-byte Ed25519 signature of `manifest.json`, added by the owner |
| `BUILDINFO` | toolchain versions (cross gcc, binutils, mkimage, Go, Node), not signed |
| `SHA256SUMS` | hashes of the files above except `BUILDINFO` and the signature |

The root file system is not part of a release: `build-rootfs.sh` bakes in the owner's SSH key, the factory MAC
and the root password hash. A generic installer image belongs to the console-free installation (OPEN.md).

## Version

`YYYY.MM.DD` or `YYYY.MM.DD.N` (N = 1…999 for more releases on one day), tag `v<version>`. The NAS compares
versions number by number and refuses a lower one than installed (downgrade). Pinned inputs (kernel version and
suffix) live only in [`scripts/release/versions.conf`](../scripts/release/versions.conf).

## Steps

1. Commit everything, update `CHANGELOG.md`, push `main` and wait for CI to be green.
2. `git tag v<version> && git push origin v<version>` – the Release workflow
   ([`.github/workflows/release.yml`](../.github/workflows/release.yml)) builds in `ubuntu:26.04` (pinned
   digest, the workstation's distribution) and attaches everything unsigned to a **draft** release.
3. On the workstation, with the tag checked out and a clean tree: `scripts/release/release.sh <version>`
   (≈10 min, kernel and web UI; Go is pinned to `go.mod` through `GOTOOLCHAIN`).
4. `scripts/release/sign-release.sh <version>` downloads the draft's files, requires identical `SHA256SUMS`
   and files matching them, checks that the manifest names the version and the tag's commit, signs
   `manifest.json` with `~/.secrets/nas-revival/release.key` and verifies the signature with
   `deploy/release-keys/release.pub`. On a mismatch it shows the differing hashes and the `BUILDINFO` diff and
   signs nothing.
5. Upload and publish with the two `gh` commands it prints (`gh release upload`, `gh release edit --draft=false`).

## What makes it reproducible

- `SOURCE_DATE_EPOCH` is the commit time of the built commit; the kernel gets fixed `KBUILD_BUILD_TIMESTAMP`,
  `_USER`, `_HOST` and `_VERSION`, `mkimage` takes the uImage header time from `SOURCE_DATE_EPOCH`.
- The kernel source tarball is checked against kernel.org's `sha256sums.asc`; the build is in-tree, so no
  absolute path ends up in the image; modules are stripped. Debug info is off (`CONFIG_DEBUG_INFO_NONE`): it
  holds the absolute build directory, which changed the build IDs of the image and of every module. `make
  RUSTC=false` keeps a host rustc out of `.config`; both sides install `libc6-dev-armel-cross` (`CC_CAN_LINK`).
- The modules tarball is sorted by name with fixed owner, mode bits and mtime, and gzip stores no name or time.
- Go: exact toolchain from `go.mod`, `-trimpath`, `-buildid=`, `-buildvcs=false`, `CGO_ENABLED=0`; the UI comes from
  `package-lock.json` (`npm ci`).
- The manifest contains no build time, so a rebuild gives the same manifest and the signature covers it.

The toolchain is not pinned to the byte: CI installs the current Ubuntu 26.04 packages, the workstation has its
own. Keep the workstation updated (`apt upgrade`) before step 3; a mismatch shows up as a `BUILDINFO` diff.

## On the NAS (UPD-02/04, not built yet)

Before any write the NAS will check the signature against the trusted keys built into the system
(`deploy/release-keys/`), then board, architecture, version (no downgrade) and every artifact hash from the
manifest. Kernel slots keep using the A→B copy and readback of `scripts/debian/update-kernel.sh`.
