#!/usr/bin/env bash
# Tests the night mode of deploy/debian/nsa310-leds and the profile checks of
# deploy/debian/nsa310-fan against fake sysfs trees. Run: bash tests/test_board_scripts.sh
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)/deploy/debian
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
fail=0
check() { if [[ $2 == "$3" ]]; then echo "ok   $1"; else echo "FAIL $1 (want '$3', got '$2')"; fail=1; fi; }

# Fake LEDs: writing a trigger replaces the file (the kernel shows the list).
leds() {
  rm -rf "$work/leds"
  for l in nsa310:green:sys nsa310:green:hdd nsa310:red:sys enp1s0-0::lan; do
    mkdir -p "$work/leds/$l"; echo 1 >"$work/leds/$l/brightness"; echo "[$1]" >"$work/leds/$l/trigger"
  done
}
run_leds() { # <now HHMM> <defaults content>
  printf '%b' "$2" >"$work/leds.defaults"
  NSA310_LEDS_ROOT=$work/leds NSA310_LEDS_DEFAULTS=$work/leds.defaults NSA310_LEDS_NOW=$1 sh "$here/nsa310-leds" 2>/dev/null
}

leds none; run_leds 2300 'NIGHT_START=2200\nNIGHT_END=0700\n'
check "night across midnight: green off"   "$(cat "$work/leds/nsa310:green:sys/trigger") $(cat "$work/leds/nsa310:green:sys/brightness")" "none 0"
check "night: LAN LED off"                 "$(cat "$work/leds/enp1s0-0::lan/trigger")" "none"
check "night: red sys keeps panic"         "$(cat "$work/leds/nsa310:red:sys/trigger")" "panic"
leds none; run_leds 0659 'NIGHT_START=2200\nNIGHT_END=0700\n'
check "06:59 is still night"               "$(cat "$work/leds/nsa310:green:sys/trigger")" "none"
leds none; run_leds 0700 'NIGHT_START=2200\nNIGHT_END=0700\n'
check "07:00 is day"                       "$(cat "$work/leds/nsa310:green:sys/trigger")" "default-on"
leds none; run_leds 1300 'NIGHT_START=1200\nNIGHT_END=1400\n'
check "night within one day"               "$(cat "$work/leds/nsa310:green:sys/trigger")" "none"
leds none; run_leds 2300 'NIGHT_START=\nNIGHT_END=\n'
check "night mode off: day setup"          "$(cat "$work/leds/nsa310:green:sys/trigger")" "default-on"
leds timer; run_leds 1000 ''
check "fault blink survives a day switch"  "$(cat "$work/leds/nsa310:red:sys/trigger")" "[timer]"

leds panic; NSA310_LEDS_ROOT=$work/leds NSA310_LEDS_DEFAULTS=/dev/null sh "$here/nsa310-leds" fault on
check "fault on: red sys blinks"           "$(cat "$work/leds/nsa310:red:sys/trigger") $(cat "$work/leds/nsa310:green:sys/trigger")" "timer [panic]"
NSA310_LEDS_ROOT=$work/leds sh "$here/nsa310-leds" fault off
check "fault off: panic trigger again"     "$(cat "$work/leds/nsa310:red:sys/trigger")" "panic"

# Fan: a fake LM85 that stores what is written.
run_fan() { # <defaults content>
  rm -rf "$work/sys"; mkdir -p "$work/sys/hwmon0"; echo lm85 >"$work/sys/hwmon0/name"
  printf '%b' "$1" >"$work/fan.defaults"
  NSA310_FAN_HWMON=$work/sys NSA310_FAN_DEFAULTS=$work/fan.defaults sh "$here/nsa310-fan" >/dev/null 2>&1
}
run_fan 'ZONE_MIN=45000\nZONE_MAX=65000\nPWM_MIN=52\n'
check "quiet profile applied"  "$(cat "$work/sys/hwmon0/temp1_auto_temp_min") $(cat "$work/sys/hwmon0/temp3_auto_temp_off") $(cat "$work/sys/hwmon0/pwm1_auto_pwm_min") $(cat "$work/sys/hwmon0/pwm1_enable")" "45000 41000 52 2"
run_fan 'ZONE_MIN=45000\nZONE_MAX=90000\nPWM_MIN=10\n'
check "unsafe values: balanced" "$(cat "$work/sys/hwmon0/temp1_auto_temp_min") $(cat "$work/sys/hwmon0/pwm1_auto_pwm_min")" "40000 64"
run_fan 'ZONE_MIN=x\n'
check "garbage: balanced"       "$(cat "$work/sys/hwmon0/temp1_auto_temp_min")" "40000"

exit $fail
