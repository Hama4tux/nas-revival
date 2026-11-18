# Validation strategy

Bootstrap validation covers document links, manifest syntax/safety defaults and Git whitespace. Stage 1 now has meaningful workstation tests for ELF/ABI rejection and controller safety, without real mounts or NAS access:

```sh
python3 -m unittest discover -s tests -p test_stage1.py -v
```

Clang, when already installed, generates ARM test **objects**, not a deployable rootfs. These tests do not demonstrate vendor-kernel compatibility, a working SSH login, build reproducibility or NAS performance. The full rootfs build/audit remains unverified until sources can be downloaded.

OpenWrt: `test_openwrt_probe.py` checks the probe is read-only, `test_openwrt_uimage.py` checks the uImage/appended-DTB swap on synthetic images. The QEMU scenarios (`scripts/openwrt/sim-*.sh`) need downloads and run separately; see [OpenWrt simulation](../docs/research/openwrt-simulation.md).

Future meaningful tests:

- HAL fixtures: missing sensors, renumbered hwmon, stale values, fan failure and unqualified profile writes.
- API: authentication, per-operation ACL, CSRF, path traversal, command injection, bounded input and job cancellation.
- Storage/shares on disposable media: user/group isolation, symlink escapes, mount failures, full disks and duplicate operations.
- Hardware qualification: SATA/Ethernet/USB, shutdown, fan safety and stock recovery, with private sanitized evidence.
- Release: two clean builds, signatures and wrong-board/downgrade rejection, interrupted updates and configuration compatibility.

Device tests require separately authorized phases; the workstation must never infer permission from this checklist.
