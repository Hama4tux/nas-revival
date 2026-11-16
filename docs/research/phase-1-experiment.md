# Experiment 01: workstation-only feasibility

Scope: research design and offline inspection. **No access to the NAS, live IP, serial adapter, disk or network scan. No host installations. No flashable image or executable installer.**

## Question and deliverables

Can a maintained ARMv5 software stack cover the original NSA310 with a small downstream integration and a credible recovery path?

1. Compare pinned kernel DT variants, OpenWrt profile/modules and NAND patch. Produce a delta table identifying fan, LEDs, storage and Ethernet assumptions.
2. Inspect existing release metadata/package indexes for SMB, NFS, SSH/SFTP, SMART, hwmon and TLS; record dependencies and unresolved compatibility. Package names alone are insufficient.
3. Draft synthetic status/capability examples: no sensor, fan fault, missing disk, stale sample and unknown revision. They are fictional fixtures, never observations from the NAS.
4. Specify a read-only API response and configuration validation boundaries without implementing services. Review the privilege boundary and first-administrator flow.
5. Prepare a local evidence form for a later owner-authorized inventory, with sensitive exports stored outside Git.

Downloading source/configuration metadata from public upstream is permitted. If further tools are missing, record them instead of installing packages. Toolchain/SDK builds and emulation are a later bounded task; ordinary QEMU virtual ARM boards cannot certify the NSA310's NAND, GPIO or fan behavior.

## Exit criteria

- Every hardware value is tagged owner-reported, source-declared, measured or unknown.
- Correct candidate DT remains unselected until board evidence exists.
- ADR, threat model, budgets, backup requirements and physical risks are reviewable.
- A later temporary-boot proposal states exact revision, image format, RAM map, no-write controls and return-to-stock criteria before approval.
- Bootstrap repository is committed and clean with no remote or deployable firmware.

## What phase 1 actually completed

Repository and design documents, upstream DT/profile/partition review, distribution/backend comparison, source provenance and recovery gates. No package dependency closure, cross compilation, UI prototype, emulation, live inventory, backup or boot test has been completed. Those are future experiments, not validation results.
