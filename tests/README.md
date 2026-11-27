# Validation strategy

Validation covers document links, manifest syntax/safety defaults and Git whitespace (`tests/test_pre_commit.sh`, run by the pre-commit hook). `tests/test_board_scripts.sh` checks the night mode and fault blink of `nsa310-leds` and the profile checks of `nsa310-fan` against fake sysfs trees. `tests/test_release.sh` covers the release version format, manifest, build comparison and signing with throwaway keys. CI runs every `tests/test_*.sh`. Backend and frontend tests live next to their code.

Future meaningful tests:

- HAL fixtures: missing sensors, renumbered hwmon, stale values, fan failure and unqualified profile writes.
- API: authentication, per-operation ACL, CSRF, path traversal, command injection, bounded input and job cancellation.
- Storage/shares on disposable media: user/group isolation, symlink escapes, mount failures, full disks and duplicate operations.
- Hardware qualification: SATA/Ethernet/USB, shutdown, fan safety and stock recovery, with private sanitized evidence.
- Release: two clean builds, signatures and wrong-board/downgrade rejection, interrupted updates and configuration compatibility.

Device tests require separately authorized phases; the workstation must never infer permission from this checklist.
