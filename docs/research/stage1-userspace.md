# Stage 1: minimal userspace beside the ZyXEL firmware

Status: **experimental build recipe and controller prepared; no rootfs has been built or deployed in this session**. No NAS connection, credentials, restart, kexec, NAND/environment write, host package installation or changes to existing services. Stage 2 is not authorized.

## Compatibility decision

This experiment shares the existing **Linux 2.6.31.8** kernel. It is not a bootable distribution, new kernel or independent network stack. It does not change ADR-0001's long-term distribution candidate.

Default modern libc choices are unsuitable to claim compatibility: [Buildroot's glibc selection requires headers >=3.2](https://github.com/buildroot/buildroot/blob/2025.02.19/toolchain/toolchain-buildroot/Config.in); [musl documents Linux >=2.6.39 for POSIX-conforming behavior](https://wiki.musl-libc.org/supported-platforms), with limitations on older kernels. Static linking alone does not remove kernel requirements.

The proposed compatibility experiment keeps **Buildroot 2025.02.19 LTS**, GCC 13.4.0, BusyBox 1.37.0 and Dropbear 2026.93, but selects maintained **uClibc-ng 1.0.59**, ARM926/ARMv5TE-compatible little-endian EABI soft-float, no threads, and the time32 ABI. It uses **only userspace headers** from Linux 2.6.31.8, not a kernel build. Modern tools use their existing old-API fallback paths rather than deliberately assuming new syscalls.

Evidence: [uClibc package integration](https://github.com/buildroot/buildroot/blob/2025.02.19/package/uclibc/uclibc.mk), [custom 2.6.x header selection](https://github.com/buildroot/buildroot/blob/2025.02.19/package/linux-headers/Config.in.host), [uClibc header-based feature assumptions](https://github.com/wbx-github/uclibc-ng/blob/v1.0.59/libc/sysdeps/linux/common/bits/kernel-features.h), [Dropbear package](https://github.com/buildroot/buildroot/blob/2025.02.19/package/dropbear/dropbear.mk).

This is a **candidate, not confirmed support** for the vendor kernel. The old-header build itself may expose source incompatibilities with current libc/packages/host tools; no successful full compile is claimed. Do not silently update headers to make the build pass: that can introduce newer-kernel assumptions. If this candidate fails, capture the exact error and qualify a narrow maintained-source compatibility fix or a separately audited ARMv5 external toolchain built for <=2.6.31.8. Do not switch to an abandoned distribution or reuse undocumented vendor libraries automatically. time32 also retains a year-2038 limitation.

## Workstation build

All [source pins](../../userspace/stage1/sources.json) and [configuration](../../userspace/stage1/configs/nsa310_stage1_defconfig) are committed. Buildroot's pinned package hashes/patches select package contents. Git commits identify the two source snapshots; tag signatures were not independently authenticated here. This environment can read source through the GitHub connector, but direct downloads fail DNS resolution; no Buildroot source checkout, cross libc or QEMU ARM runtime is available. Therefore actual ELF files, dependency closure and rootfs size remain unmeasured.

On an Ubuntu workstation with existing Buildroot prerequisites, as an ordinary user (no sudo):

```sh
# Download public build sources only; no NAS connection.
git clone --depth 1 --branch 2025.02.19 https://github.com/buildroot/buildroot.git /tmp/nas-revival-buildroot
git -C /tmp/nas-revival-buildroot rev-parse HEAD
# Must be 4f924fd52841608d68ac3dcb83fccee8b8c90316.

cd ~/VScode/NAS310/nas-revival
python3 scripts/stage1/build.py /tmp/nas-revival-buildroot "$PWD/output/stage1"
```

The output directory must be fresh. For configuration inspection only, use a different fresh output path and `--configure-only`. The script never installs workstation packages: missing prerequisites stop the build. Buildroot may download and compile build-local host tools/dependencies. No `make linux`, bootloader, disk image or firmware target is requested.

After a successful build and audit, outputs would be:

- `output/stage1/images/rootfs-stage1.tar.gz`: separate directory tree, not a flash image.
- `output/stage1/images/audit-stage1.json`: architecture, dependency, size and hash results; runtime compatibility explicitly UNVERIFIED.
- `output/stage1/.config` and package configurations: exact resolved build evidence.

The packer fixes owner, order and timestamps. These are reproducibility controls; byte-for-byte reproducibility is not claimed until two complete clean builds are compared. Before redistributing binaries, retain Buildroot legal-info and corresponding source/licenses; the project's MIT license does not relicense BusyBox/libc.

## What the offline audit checks

The builder calls [audit_rootfs.py](../../scripts/stage1/audit_rootfs.py) without executing ARM programs or using host `ldd`:

- All ELF files must be little-endian ARM ELF32/EABI5, ARMv4T–ARMv5TEJ attributes, without hard-float, VFP or SIMD requirements. ARM926's instruction subset is suitable for the reported Feroceon ARMv5TE; vendor kernel EABI support still needs runtime confirmation.
- Required shell, BusyBox, Dropbear, key generator and account/config files must exist. Libraries/interpreter must resolve within the tree; DT_NEEDED and strong dynamic-symbol dependencies are checked. GNU kernel ABI notes requesting newer kernels are rejected.
- Resolved Buildroot/libc settings and generated 2.6.31 API headers are checked; no kernel build, modern libc substitution, threads or time64 silently accepted.
- No keys or special device files are shipped. Rootfs bytes, compressed archive bytes and hashes are recorded only after an actual successful build.

ELF notes are not a complete syscall manifest. Their absence is **not** a proof that Linux 2.6.31.8 will work. Header selection reduces newer-syscall assumptions but cannot prove the vendor kernel configuration, EABI helper behavior, ioctl/netlink behavior, entropy, private PTY support or every fallback path. Those remain runtime tests. Any contradictory minimum-kernel evidence blocks the build; lack of proof is reported as unverified.

## Manual installation — not executed here

Keep the original Telnet session open as the emergency/control channel. Manually transfer the successfully audited archive, the four target shell scripts in `scripts/stage1/`, and one administrator **public** Ed25519 key using the owner's chosen existing mechanism. Do not transfer private client keys or read `secret.env` automatically. The target scripts never make remote connections.

Choose a **new directory** named `nas-revival-stage1` on an already mounted SATA/USB **ext2/3/4** filesystem. The existing FAT32 partition cannot hold this Unix rootfs correctly. No disk is mounted, formatted, repartitioned or repaired by the installer. Do not choose the ZyXEL root filesystem, an existing system directory, a symlink path or the root of a mount.

Through the owner's existing root Telnet session, substituting real already-mounted paths:

```sh
ROOTFS_DIR=/absolute/already-mounted-disk/nas-revival-stage1
# EXPECTED_SHA256 must come from the workstation build report through a trusted channel.
sh /path/to/helpers/install-stage1.sh /path/to/rootfs-stage1.tar.gz "$EXPECTED_SHA256" "$ROOTFS_DIR" /path/to/admin-key.pub
sh /path/to/helpers/control-stage1.sh preflight "$ROOTFS_DIR"
```

The installer accepts only the separately hash-verified, audited build artifact, not arbitrary untrusted tarballs. It refuses an existing destination; a failed extraction is retained for inspection rather than deleting an arbitrary tree. It copies the public key into the separate rootfs only. Host account files, DNS and routing are untouched.

## First controlled test and SSH

First run a short probe; its proc/dev/run mounts are removed on exit:

```sh
sh /path/to/helpers/control-stage1.sh probe "$ROOTFS_DIR" --accept-unverified-kernel --no-pty
```

This executes the new shell, `id`, `uname`, and read-only address/route queries on the existing kernel. `--no-pty` needs no private PTY support. The acknowledgement flag states that static checks cannot certify runtime compatibility. A successful probe is not yet an SSH login test.

If that succeeds, supply one already assigned unicast IPv4 address; do not use a wildcard bind:

```sh
sh /path/to/helpers/start-stage1.sh "$ROOTFS_DIR" "$NAS_LAN_IP" --accept-unverified-kernel
cat "$ROOTFS_DIR/.stage1-state/hostkey-public.txt"
```

Dropbear binds only that address on **2222**, checks for existing IPv4/IPv6 listeners, disables password/PAM authentication at compile time and also passes `-s`. Forwarding and SFTP are disabled. A new persistent Ed25519 host key is generated **on the NAS**, only after entropy checks, and never copied from ZyXEL. Compare its fingerprint obtained over the existing trusted control session when connecting from Ubuntu:

```sh
ssh -p 2222 -i ~/.ssh/nas_revival -o IdentitiesOnly=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o UserKnownHostsFile=~/.ssh/known_hosts_nas_revival -o StrictHostKeyChecking=ask "root@$NAS_LAN_IP"
```

Do not accept a mismatching key or disable verification. A root shell in this chroot is fully privileged on the **same host kernel/network**, not a sandbox. Only a trusted administrator key is appropriate. No NAT, DHCP, Ethernet, firewall, routes, host DNS or host services are configured. Rootfs DNS is intentionally empty; IP diagnostics and SSH do not need DNS. Existing firewall reachability must be observed, not automatically changed.

From the original Telnet session, stop the experiment:

```sh
sh /path/to/helpers/stop-stage1.sh "$ROOTFS_DIR"
```

Only processes with this exact `/proc/PID/root` are signalled; no killall/pkill. Existing sessions in the chroot are also stopped. Mounts are tracked by kernel mount ID and removed in reverse order; pre-existing/replaced mounts are never adopted or forcibly/lazily unmounted. If processes or mounts remain busy, stop fails visibly and retains state. No directory is deleted. A stale controller lock after SIGKILL/power loss requires manual inspection, not a blind retry/removal.

Private `/dev` is a new tmpfs containing null/zero/random/urandom/tty plus, by default, a **new devpts instance**, not a bind of host `/dev`. Linux 2.6.31.8 needs CONFIG_DEVPTS_MULTIPLE_INSTANCES for that PTY path; if unavailable, the test aborts/rolls back rather than exposing host terminals. An explicit `--no-pty` on start skips devpts and exposes no PTY nodes: connect with `ssh -T` to run a shell/commands without terminal allocation. This supports a bounded non-PTY SSH experiment without requiring a different kernel; it is not an automatic fallback. New `/proc` still exposes host process information; no PID/network isolation is claimed.

## Validation performed and remaining blockers

Fourteen local tests passed. They cover real Clang ARM object attributes (v5 soft-float accepted; v7/hard-float rejected), wrong-host ELF rejection, escaping symlinks, rootfs finalization without keys, occupied-port refusal, refusal of pre-existing/replaced mounts and rollback after a simulated private-PTY failure. Test mount/chroot operations use temporary fake commands/views: no real mounts, ARM execution or NAS connections were performed.

Before the first SSH login we still need the pinned source downloads and complete successful build/audit (including actual size/dependency results), vendor-kernel probe, EABI/private-PTY/entropy support, administrator public key, available port and existing-network reachability, followed by an actual authenticated SSH shell and stop test. This task stops at the prepared recipe/scripts; it does not authorize deploying or running them on the NAS now.
