# Scripts policy

Stage 1 adds a [workstation builder and manual target installer/controller](stage1/) for a separate chroot directory. See the [guide](../docs/research/stage1-userspace.md). No host setup, network discovery, disk preparation or flashing is automated. The scripts have not been executed on the NAS.

Future scripts must be idempotent where appropriate, have bounded output and clear prerequisites, pin external inputs, distinguish host from target and default to no device writes. Do not install packages, scan networks, modify U-Boot or initialize storage as a build side effect.
