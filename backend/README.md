# Backend scope

Design only: [technology evaluation](../docs/architecture/technology-selection.md) proposes OpenWrt-native uhttpd/rpcd/ubus with small ucode modules, and narrow privileged helpers only where necessary.

First implementation milestone is an authenticated read-only API for status and capabilities. Define the API contract and ACL/process boundary before coding. No Node.js runtime, database daemon, shell-command endpoint or custom cryptography is planned on the NAS. Footprint and ARM compatibility require actual tests.
