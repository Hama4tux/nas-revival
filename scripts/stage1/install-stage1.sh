#!/bin/sh
# Run manually on the NAS only after reviewing the workstation audit and hash.
set -eu
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
die() { echo "stage1-install: $*" >&2; exit 1; }
[ "$#" -eq 4 ] || die 'usage: install-stage1.sh ARCHIVE EXPECTED_SHA256 DESTINATION PUBLIC_KEY_FILE'
[ "$(id -u)" = 0 ] || die 'root is required for ownership-preserving extraction'
archive=$1 expected=$2 destination=$3 key=$4
case "$archive" in /*) ;; *) die 'use an absolute archive path';; esac
case "$key" in /*) ;; *) die 'use an absolute public key path';; esac
case "$destination" in /*/nas-revival-stage1) ;; *) die 'destination must be an absolute .../nas-revival-stage1 path';; esac
case "$destination" in *[!a-zA-Z0-9_./-]*|*/../*|*/./*) die 'noncanonical destination';; esac
case "$expected" in *[!a-f0-9]*|'') die 'expected SHA256 must be lowercase hexadecimal';; esac
[ "${#expected}" -eq 64 ] || die 'SHA256 length must be 64'
[ -f "$archive" ] && [ -f "$key" ] || die 'archive and administrator public key are required'
[ ! -e "$destination" ] && [ ! -L "$destination" ] || die 'destination already exists; never extract over another tree'
parent=${destination%/*}
[ "$(readlink -f "$parent")" = "$parent" ] || die 'parent must already exist without symlink components'
fstype=$(awk -v p="$parent" '$2=="/" || p==$2 || index(p,$2"/")==1 {if(length($2)>n){n=length($2);t=$3}} END{print t}' /proc/mounts)
case "$fstype" in ext2|ext3|ext4) ;; *) die 'destination must be on an already mounted ext2/3/4 filesystem; no FAT or system NAND';; esac
# Fail closed when the backing ext filesystem is also the host root filesystem.
backing=$(awk -v p="$parent" '$2=="/" || p==$2 || index(p,$2"/")==1 {if(length($2)>n){n=length($2);m=$2}} END{print m}' /proc/mounts)
[ "$backing" != / ] || die 'refusing installation onto the host root filesystem'
actual=$(sha256sum "$archive" | awk '{print $1}')
[ "$actual" = "$expected" ] || die 'archive checksum mismatch'
# Only our reviewed, audited archive is accepted; the trusted hash is supplied separately.
# This listing guard is additional protection, not an arbitrary untrusted-tar extractor.
tar -tzf "$archive" | awk '
  /^\// || /(^|\/)\.\.(\/|$)/ || /[[:space:]]/ {bad=1}
  END{exit bad}' || die 'unsafe archive member names'
public_key=$(awk 'NF && $1!~/^#/ {if($1!="ssh-ed25519" || $2!~/^[A-Za-z0-9+\/=]+$/)bad=1; n++; value=$1" "$2} END{if(bad || n!=1)exit 1; print value}' "$key") || die 'provide one plain ssh-ed25519 public key, without options'
mkdir -m 700 "$destination"
# On failure retain the incomplete directory for inspection; never delete an arbitrary tree.
cp "$archive" "$destination/.stage1-archive"
[ "$(sha256sum "$destination/.stage1-archive" | awk '{print $1}')" = "$expected" ] || die 'archive changed while copying; do not start'
tar -xzf "$destination/.stage1-archive" -C "$destination"
[ "$(cat "$destination/.nas-revival-stage1")" = nas-revival-stage1-v1 ] || die 'wrong rootfs marker'
(
  cd "$destination"
  sha256sum -c etc/stage1-files.sha256 >/dev/null
) || die 'installed file integrity failed; do not start'
printf '%s\n' "$public_key" > "$destination/root/.ssh/authorized_keys"
chmod 700 "$destination/root" "$destination/root/.ssh" "$destination/etc/dropbear"
chmod 600 "$destination/root/.ssh/authorized_keys"
rm "$destination/.stage1-archive"
echo 'Installed separate Stage 1 directory; no service started, no mounts or host configuration changed.'
