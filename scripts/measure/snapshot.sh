#!/bin/sh
# Read-only resource snapshot of the NAS for PERFORMANCE_BUDGET.md.
# Usage: scripts/measure/snapshot.sh [ssh-host] [sample-seconds]
# Runs only reads of /proc, /sys and ps on the NAS; changes nothing.
set -eu
HOST=${1:-nsa310}
SAMPLE=${2:-30}
case $SAMPLE in ''|*[!0-9]*) echo "sample-seconds must be a number" >&2; exit 2;; esac

ssh -o BatchMode=yes "$HOST" SAMPLE="$SAMPLE" sh -s <<'REMOTE'
set -eu
kb() { awk -v k="$1" '$1==k":" {print $2}' /proc/meminfo; }
mib() { awk -v v="$1" 'BEGIN {printf "%.1f", v/1024}'; }
total=$(kb MemTotal); avail=$(kb MemAvailable); cached=$(kb Cached)
swt=$(kb SwapTotal); swf=$(kb SwapFree)
echo "date:            $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "kernel:          $(uname -r)  debian $(cat /etc/debian_version)"
echo "uptime_s:        $(cut -d' ' -f1 /proc/uptime)"
echo "loadavg:         $(cut -d' ' -f1-3 /proc/loadavg)"
echo "mem_total_mib:   $(mib "$total")"
echo "mem_used_mib:    $(mib $((total - avail)))   (total - available)"
echo "mem_avail_mib:   $(mib "$avail")"
echo "page_cache_mib:  $(mib "$cached")"
echo "swap_used_mib:   $(mib $((swt - swf)))"
echo "processes:       $(ps -e --no-headers | wc -l)"
for name in nas-revival; do
  for pid in $(pgrep -x "$name" || true); do
    rss=$(awk '/^VmRSS/ {print $2}' /proc/$pid/status)
    hwm=$(awk '/^VmHWM/ {print $2}' /proc/$pid/status)
    pss=$(awk '/^Pss:/ {print $2}' /proc/$pid/smaps_rollup 2>/dev/null || echo 0)
    echo "proc $pid $(tr '\0' ' ' </proc/$pid/cmdline | cut -c1-60): rss $(mib "$rss") MiB, pss $(mib "$pss") MiB, peak $(mib "$hwm") MiB"
  done
done
echo "top_rss_kib:"
ps -eo rss=,comm= --sort=-rss | head -8 | sed 's/^/  /'
disks=$(ls /sys/block | grep -E '^(sd|mmcblk|nvme)' || true)
snap() { for d in $disks; do awk -v d="$d" '$3==d {print d, $6, $10}' /proc/diskstats; done; }
cpu() { awk '/^cpu / {print $2+$3+$4+$5+$6+$7+$8, $5}' /proc/stat; }
b1=$(snap); c1=$(cpu)
sleep "$SAMPLE"
b2=$(snap); c2=$(cpu)
echo "sample_s:        $SAMPLE"
echo "$c1 $c2" | awk '{t=$3-$1; i=$4-$2; if (t>0) printf "cpu_idle_pct:    %.1f\n", 100*i/t}'
echo "$b1" | while read -r d r w; do
  [ -n "$d" ] || continue
  set -- $(echo "$b2" | awk -v d="$d" '$1==d {print $2, $3}')
  echo "disk $d: read $(( ($1 - r) / 2 )) KiB, written $(( ($2 - w) / 2 )) KiB in sample"
done
for d in $disks; do
  if command -v hdparm >/dev/null 2>&1; then
    echo "power $d: $(hdparm -C /dev/$d 2>/dev/null | awk -F: '/state/ {gsub(/ /,"",$2); print $2}')"
  fi
done
echo "listening:"
ss -ltnuH | awk '{print "  " $1, $5}' | sort -u
REMOTE
