# ADR 0017: Update signing key

Status: accepted (owner, 2026-11-27)

## Context

UPD-02 (NAS Revival self-update) and UPD-04 (kernel to NAND slots from the web) need a signed release
manifest (threat model: malicious or replayed update). The repository is public, the release is built
in GitHub Actions on a tag, and the owner works on Linux, macOS and Windows.

## Decision

- **Format:** minisign (Ed25519). The private key is protected by a passphrase (scrypt). The tool runs on
  all three platforms. The NAS can verify the format with Go stdlib `crypto/ed25519`.
- **Private key:** only in `~/.secrets/nas-revival/release.key` (mode 600) on the owner's machine, never in
  git, never in CI or GitHub Secrets. The owner generates it locally (`minisign -G`), so the passphrase
  never passes through an agent. An encrypted copy is kept offline, away from the computer.
- **Public keys:** committed in the repo and built into the NAS system. The NAS trusts a list of public
  keys: the current key and one backup key, which is generated at the same time and stored offline.
- **Signing flow:** CI builds the kernel, rootfs, manifest and `SHA256SUMS`, all unsigned. The owner
  rebuilds locally, compares the hashes, and signs the manifest locally. The signature is uploaded to
  the release. A compromised GitHub account therefore cannot produce an update the NAS accepts.
- **Manifest:** covers every artifact (hash, board, architecture, version). The NAS rejects a version
  lower than the installed one (downgrade), a different board, and an invalid signature, all before any
  write.
- **Rotation:** an update signed by the current key can replace the trusted-key list. If both keys are
  lost or leaked, the owner installs a new public key manually over SSH or UART (physical access is
  assumed by the threat model).

## Consequences

- Each release needs one manual local step (rebuild and sign). This is acceptable for one owner and few
  releases.
- Losing the private key does not brick anything: it only blocks new signed updates until rotation.
  Leaking it is the serious case, which is why the key has a passphrase and CI never holds it.
