# ADR 0014: Firewall with confirm-or-rollback

- Status: accepted (2026-11-27, W5.2)
- Scope: SEC-12 (nftables rules: allowed networks per service, anti-lockout, confirm-or-rollback), advisor check
  "firewall off" (SEC-13)
- Context: every service listens on all addresses and nothing filters incoming traffic: services the UI turns
  on are reachable from any network the NAS is in, and ports the NAS does not need (e.g. rpc.mountd on random
  ports) are open. The kernel had no netfilter at all; the kernel `6.12.119-nr1` adds nftables (built in,
  IPv4 only – the kernel has no IPv6, so the NAS has no IPv6 address to protect).

## Decision

- **Model: allowed sources per service**, not free-form rules. Fixed catalogue (package `internal/firewall`):
  `web` TCP 8443, `ssh` TCP 22, `smb` TCP 445, `nfs` TCP 2049 + 111. Each service has a list of up to 16 IPv4
  addresses or networks (normalised to the network address; `0.0.0.0/0` = anyone, the UI warns); an empty list
  blocks the service. Settings `{enabled, allow: {web: [...], ssh: [...], smb: [...], nfs: [...]}}` in
  `/etc/nas-revival/firewall.json`; unknown service keys are rejected. The UI proposes the LAN network of the
  interface for every service.
- **Rule set** (table `ip nas_revival`, input hook only, policy drop): `lo`, `ct state established,related`,
  drop `ct state invalid`, ICMP echo-request (limited 10/s), DHCP replies (UDP 67 → 68), then one rule per
  service and source set. Output and forward stay untouched. Anything else incoming is dropped – also ports
  of services not in the catalogue (rpc.mountd, LLMNR).
- **Atomic apply:** the helper renders a complete nft script that starts with `table ip nas_revival {}` +
  `delete table ip nas_revival`, so `nft -f` replaces the table in one transaction; disabled = only those two
  lines. `nft -c -f` checks the script first.
- **Confirm-or-rollback (as network changes, 180 s):** apply loads the new rules into the kernel only; the boot
  file `/etc/nftables.d/nas-revival.nft` (root, 0644) and `firewall.json` are written only on confirm. Rollback
  (timeout, failed apply) re-loads the boot file, or removes the table when there is none. A reboot during the
  window boots with the old rules. A marker in the helper's state directory makes a helper restart during the
  window roll back immediately instead of re-arming.
- **Anti-lockout:** the API passes the client's TCP address (`RemoteAddr`, no proxy, `X-Forwarded-For` never
  trusted – ADR 0013) to the helper; an enabled firewall is refused when `web` or `ssh` (the recovery path) do
  not allow that address. The confirmation must arrive over the new rules, which proves the browser still
  reaches the NAS.
- **Boot:** `nas-revival-firewall.service` (oneshot, `Before=network-pre.target`) runs `nft -f` on the boot
  file, so the rules are active before the interface comes up. Debian's `nftables.service` stays disabled.
- **Netfilter built into the kernel,** not as modules: the helper runs with `ProtectKernelModules=yes` and nft
  could not load them. Without kernel support (`nft list tables` fails) the API reports `supported: false` and
  the UI only explains it.
- **API:** `GET /security/firewall` (settings, pending change with deadline, supported, active, client address,
  suggested LAN network; read-only administrator too), `PUT /security/firewall` (admin) → `{pending}`,
  `POST /security/firewall/confirm` (admin). Each apply and confirm is logged with the user.
- **Advisor:** `firewall.off` (warn) when supported but disabled, fix link to the firewall.

- **Rule table and listening ports (2026-11-27 amendment):** the fixed catalogue alone blocked services it did
  not know with no way to allow them. `firewall.json` gains `rules` (up to 64, "expert mode"): an ordered table
  of source, ports (empty = all), tcp/udp/both and accept/drop, rendered before the catalogue services so the
  first match wins; there is no raw nft, every field is validated and the note never reaches the script. The
  anti-lockout check walks the rules for the client and tcp 8443/22 and refuses a matching drop, naming the
  rule. The state also lists the non-loopback listening sockets (`ss`, the parser of the Services app) with
  `allowed`, computed by a pure function from the pending (else saved) settings, so the UI can show what a
  firewall that is still off would block. A failing `ss` gives an empty list, never a failed get.

## Consequences

- Recovery when locked out anyway: wait 180 s (rollback), reboot (the boot file was not changed yet), or on the
  console `nft delete table ip nas_revival` and remove the boot file (docs/RECOVERY.md R4).
- Not in phase 1: config history / export of `firewall.json` (restore would bypass the confirmation), packet
  counters, rate limiting of new connections, IPv6.
