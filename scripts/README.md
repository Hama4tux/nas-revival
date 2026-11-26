# Scripts policy

[`debian/`](debian/) builds the Debian 13 rootfs and kernel and holds the disk-swap installer and U-Boot helpers; [`webui/`](webui/) builds the web UI and runs end-to-end checks; [`git-hooks/`](git-hooks/) holds the pre-commit hook.

[`measure/snapshot.sh`](measure/snapshot.sh) takes a read-only resource snapshot of the NAS over SSH (RAM, RSS/PSS, CPU idle, disk writes, listening ports) for [PERFORMANCE_BUDGET](../docs/PERFORMANCE_BUDGET.md).

Future scripts must be idempotent where appropriate, have bounded output and clear prerequisites, pin external inputs, distinguish host from target and default to no device writes. Do not install packages, scan networks, modify U-Boot or initialize storage as a build side effect.
