# Hardware abstraction

The HAL adapts **existing Linux interfaces**, not raw board registers. Device Tree/kernel configuration belongs to device integration; policy and web presentation remain separate. Unsupported and unverified are distinct from healthy.

## Proposed contract

`capabilities()` returns profile ID, evidence status, supported sensors/LED roles/buttons and whether operations are read-only or qualified for writes. `read_health()` returns temperature in millidegrees Celsius, fan RPM, timestamp and `ok | stale | unknown | unsupported | fault`; missing values are null.

| Operation | Linux interface | Policy |
| --- | --- | --- |
| Read temperature/fan | hwmon `name`, `temp*_input`, `fan*_input` | Resolve by driver and physical device path/label, never a hardcoded hwmon index |
| Request fan policy | Verified hwmon PWM/automatic mode | Only qualified profile ranges and automatic control; no arbitrary PWM from browser |
| Set LED role | LED class brightness/triggers | Stable semantic roles: status, fault, disk, identify; preserve kernel activity triggers |
| Observe buttons | input events or distribution hotplug adapter | Debounce; short/long press policy; reset does not format storage |
| Power off/reboot | Distribution service manager and kernel power support | Authenticated, explicit action after service stop, flush and unmount handling |

Use a read-only fake adapter for workstation experiments. A generic Linux adapter handles standard interfaces; profiles provide validated selectors, units, limits and board capabilities. Profiles are trusted release artifacts, not user-uploaded scripts. The API never accepts a sysfs path.

## NSA310 uncertainty

Linux `kirkwood-nsa310.dts` describes an ADT7476 controller; `kirkwood-nsa310a.dts` describes an LM85 and a different USB LED arrangement. OpenWrt's `kirkwood-nsa310b.dts` describes LM85 with red/green USB LEDs. **Do not infer the installed controller from the enclosure name.** See [Linux NSA310](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310.dts), [Linux NSA310a](https://github.com/torvalds/linux/blob/v6.12/arch/arm/boot/dts/marvell/kirkwood-nsa310a.dts), [OpenWrt NSA310b](https://github.com/openwrt/openwrt/blob/v25.12.6/target/linux/kirkwood/files/arch/arm/boot/dts/marvell/kirkwood-nsa310b.dts).

Both controller families have existing Linux hwmon support: [adt7475 driver covering ADT7476](https://docs.kernel.org/hwmon/adt7475.html), [lm85 driver](https://docs.kernel.org/hwmon/lm85.html). The correct module, channel mapping and actual fan wiring still require qualification. Do not label every temperature channel CPU temperature.

## Failure behavior

The web process must not be responsible for keeping the fan running. Prefer verified controller automatic mode; otherwise a supervised thermal policy with a known safe fallback is required. On sensor failure retain verified safe cooling and report degraded state. Unknown profile: no actuator writes and no promise that a generic emergency PWM value is safe. If reliable cooling cannot be established, stop hardware experiments.

Later tests must cover missing/renumbered hwmon devices, stale samples, fan stall, profile mismatch, service crash and duplicate button events. A mock can validate policy; only a board test can qualify thermal behavior. `hardware.yaml` currently has no active selectors or write permissions.
