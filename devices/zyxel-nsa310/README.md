# ZyXEL NSA310 integration

Support status: **research only, exact revision unverified**. This directory is not an installable board-support package.

`hardware.yaml` is evidence metadata, written in JSON syntax (valid YAML 1.2) so it can be inspected with Python's standard library without installing a YAML parser. Null means unmeasured; expected values are separately named reference data. It must never be passed to an installer as a qualified profile.

Before activating a device profile, complete [hardware qualification](../../docs/hardware/zyxel-nsa310.md), [recovery gates](../../docs/recovery/recovery-plan.md) and ADR acceptance. Model strings alone cannot select GPIO or fan writes.
