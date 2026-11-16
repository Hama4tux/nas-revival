# Bootstrap validation — 2026-11-16

## NAS Revival

- All local Markdown document links resolve inside the repository.
- `hardware.yaml` is valid JSON, a YAML 1.2 subset; measured values remain null, no runtime profile is selected and all write permissions are false.
- Research evidence includes 12 inspected source files with SHA-256 digests of their retrieved UTF-8 contents. No release image signature was checked.
- The shared agent procedure is copied verbatim from the existing reference project; project-specific restrictions follow it.
- OPEN.md uses the required Czech sections/date format; files contain no private IP literals or device dumps.
- Git staged whitespace validation passes before the bootstrap commit.

These are documentation checks. No cross build, runtime security test, emulation, hardware inventory, boot, thermal measurement, disk check or backup has been performed. No remote repository was created.

## Portfolio integration handoff

The [patch](../../tools/hamaproject.patch) adds only product-development information and a HamaProject changelog entry. It applies cleanly to the local HamaProject source snapshot. The real HamaProject remains unchanged: it is outside this session's writable roots.

In an isolated temporary copy with existing dependencies copied locally and Node 22 selected:

- `npm run typecheck`: passed.
- Full `npm test`: **28 passed, 10 failed**, with 10 `listen EPERM` errors preventing local HTTP servers from starting. These integration tests remain unverified in this sandbox; none was changed or skipped to manufacture a passing suite.
- `node --import tsx scripts/collect.ts`: passed and generated a local portfolio candidate with NAS Revival, state from OPEN.md, two owner decisions, eight unfinished items and the completed milestone.

This local artifact is not the published portfolio.json. No standard deployment or published-artifact verification was possible. Apply the patch in an appropriately writable session, rerun the required checks and deploy using HamaProject's own instructions; then verify the actual served project entry.
