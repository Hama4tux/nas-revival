# Scripts policy

Stage 1 adds a [workstation builder and manual target installer/controller](stage1/) for a separate chroot directory. See the [guide](../docs/research/stage1-userspace.md). No host setup, network discovery, disk preparation or flashing is automated. The scripts have not been executed on the NAS.

[`openwrt/`](openwrt/) holds the read-only probe and the workstation-only OpenWrt build and simulation scripts ([guide](../docs/research/openwrt-simulation.md)); they download into `~/.cache/nas-openwrt-sim` and never contact the NAS.

[`measure/snapshot.sh`](measure/snapshot.sh) takes a read-only resource snapshot of the NAS over SSH (RAM, RSS/PSS, CPU idle, disk writes, listening ports) for [PERFORMANCE_BUDGET](../docs/PERFORMANCE_BUDGET.md).

Future scripts must be idempotent where appropriate, have bounded output and clear prerequisites, pin external inputs, distinguish host from target and default to no device writes. Do not install packages, scan networks, modify U-Boot or initialize storage as a build side effect.
