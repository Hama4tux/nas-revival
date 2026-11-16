# Validation strategy

Bootstrap validation covers local document links, manifest syntax/safety defaults and Git whitespace. These checks do not demonstrate hardware compatibility, successful boot, security, build reproducibility or NAS performance. No runtime implementation exists, so no artificial code unit tests were added.

Future meaningful tests:

- HAL fixtures: missing sensors, renumbered hwmon, stale values, fan failure and unqualified profile writes.
- API: authentication, per-operation ACL, CSRF, path traversal, command injection, bounded input and job cancellation.
- Storage/shares on disposable media: user/group isolation, symlink escapes, mount failures, full disks and duplicate operations.
- Hardware qualification: SATA/Ethernet/USB, shutdown, fan safety and stock recovery, with private sanitized evidence.
- Release: two clean builds, signatures and wrong-board/downgrade rejection, interrupted updates and configuration compatibility.

Device tests require separately authorized phases; the workstation must never infer permission from this checklist.
