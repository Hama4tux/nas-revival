# ADR-0001: Base distribution

Date: 2026-11-16. Status: **historical** – the owner chose Debian 13 on 2026-11-25 (see ADR-0002); the OpenWrt/Buildroot evaluation below was rejected. The related research was removed from the repo on 2026-11-26, see git history.

## Context

The target is a reported 256 MiB ARMv5 NAS with ageing NAND. We need maintained security components, low overhead, SATA/network/hwmon support and a recoverable boot path. Board identity, flash geometry and bootloader capabilities have not been measured.

## Options

| Option | Advantages | Costs and hardware limits | Updates and sustainability |
| --- | --- | --- | --- |
| OpenWrt Kirkwood | Embedded packaging, musl/BusyBox, existing service/configuration and RPC integration; published NSA310b profile | Exact NSA310 revision unresolved; router defaults need a NAS-specific configuration; NAND layout differs; Samba/TLS memory and ARMv5 performance need measurement | Use supported release builds with pinned feeds and complete signed system releases; no assumption of automatic rollback or indefinite Kirkwood support |
| Debian ARMEL + compatible kernel | Familiar administration and package ecosystem; useful diagnostic/reference environment | Requires third-party/custom Kirkwood kernel with Debian 13; greater configurable baseline footprint, unsuitable to assume a full system fits NAND; USB/SATA root would need separate planning | Debian 13 is the last ARMEL release; distribution-wide support dates do not guarantee future ARMEL LTS coverage; kernel maintenance becomes ours |
| Buildroot | Explicit ARMv5/EABI options; highly selective userspace and reproducible system integration | Board integration, service configuration, authentication, updater and recovery need more engineering; no NSA310 qualification performed | Whole-system rebuild/release model; project owns CVE triage, signed delivery and migrations; an LTS build system alone is not a maintained product |

Evidence: [OpenWrt release target](https://downloads.openwrt.org/releases/25.12.6/targets/kirkwood/generic/), [Kirkwood source](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/Makefile), [Debian ARMEL release note](https://www.debian.org/releases/trixie/release-notes/issues.html#last-release-for-armel), [Debian support scope](https://www.debian.org/releases/trixie/), [Buildroot manual](https://buildroot.org/downloads/manual/manual.html), [ARM configuration](https://github.com/buildroot/buildroot/blob/2025.02.19/arch/Config.in.arm).

## Decision

Prioritize **OpenWrt** for workstation investigation. This chooses a candidate, not a flash image or permission to boot it. Keep Buildroot as the fallback if board support or a sustainable NAS configuration cannot be achieved with small upstreamable changes. Debian ARMEL is a diagnostic alternative rather than the default long-term product base.

Accept this ADR only after all gates pass:

1. Identify board, network and fan controller; select correct DT and needed driver configuration.
2. Establish verified backups and a documented stock return path.
3. Demonstrate a separately authorized temporary boot without NAND, saved environment or user-disk writes.
4. Measure memory, service compatibility and cooling; document whether the proposed budgets are achievable.
5. Confirm current upstream security policy and ownership of downstream patches/update signing.

## Consequences

Reuse OpenWrt packages and small maintained overlays, without maintaining a complete fork. Keep hardware profiles and service adapters separate from the public API. No current evidence supports a universal NSA310 image, A/B NAND updates, vendor-compatible NAND offsets or line-rate encrypted sharing.

Revisit if Kirkwood support ends, essential packages stop building, cooling cannot be made reliable, or maintainers cannot sustain security updates. If none of the candidates is supportable, document that limitation rather than ship an obsolete kernel as a secure product.
