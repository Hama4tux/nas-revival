#!/bin/sh
# Manual target-side controller. No network connections, no host configuration writes.
set -eu
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
LC_ALL=C
export LC_ALL
umask 077
PROC=/proc
die() { echo "stage1: $*" >&2; exit 1; }
for tool in id readlink stat cat awk uname grep od sha256sum mount umount chroot mkdir mknod chmod ln rm rmdir sleep; do
    command -v "$tool" >/dev/null 2>&1 || die "required host applet missing: $tool"
done
[ "$#" -ge 2 ] || die 'usage: control-stage1.sh preflight|probe|start|stop ROOT [BIND_IPV4] [--accept-unverified-kernel]'
ACTION=$1 ROOT=$2
shift 2
case "$ACTION" in preflight|probe|start|stop) ;; *) die 'unknown action';; esac
case "$ROOT" in /*/nas-revival-stage1) ;; *) die 'root must be an absolute .../nas-revival-stage1 directory';; esac
case "$ROOT" in *[!a-zA-Z0-9_./-]*|*/../*|*/./*) die 'noncanonical root path';; esac
[ "$(id -u)" = 0 ] || die 'root required'
[ "$(readlink -f "$ROOT")" = "$ROOT" ] || die 'root must exist without symlink components'
[ "$(stat -c '%u:%a' "$ROOT")" = 0:700 ] || die 'root directory must be owned by root, mode 0700'
[ -f "$ROOT/.nas-revival-stage1" ] && [ ! -L "$ROOT/.nas-revival-stage1" ] || die 'missing real rootfs marker'
[ "$(cat "$ROOT/.nas-revival-stage1")" = nas-revival-stage1-v1 ] || die 'wrong rootfs marker'
fstype=$(awk -v p="$ROOT" '$2=="/" || p==$2 || index(p,$2"/")==1 {if(length($2)>n){n=length($2);t=$3;m=$2}} END{if(m!="/")print t}' "$PROC/mounts")
case "$fstype" in ext2|ext3|ext4) ;; *) die 'rootfs must be a separate directory on an already mounted ext2/3/4 disk, not host root/NAND';; esac
case "$(uname -m)" in armv5tel|armv5tejl) ;; *) die 'target must be little-endian ARMv5; will not run ARM programs on this machine';; esac
case "$(uname -r)" in 2.6.31.8|2.6.31.8-*) ;; *) die 'this experiment is scoped to kernel 2.6.31.8';; esac
grep -Eq 'CPU architecture[[:space:]]*:[[:space:]]*5TE|Feroceon' "$PROC/cpuinfo" || die 'ARMv5TE/Feroceon CPU evidence missing'
[ -r "$PROC/self/mountinfo" ] || die 'mount ID tracking unavailable'

mount_id() { awk -v p="$1" '$5==p {id=$1;n++} END{if(n==1)print id}' "$PROC/self/mountinfo"; }
root_pids() {
    for process in "$PROC"/[0-9]*; do
        [ -d "$process" ] || continue
        [ "$(readlink "$process/root" 2>/dev/null || :)" = "$ROOT" ] || continue
        echo "${process##*/}"
    done
}
port_in_use() {
    set -- "$PROC/net/tcp"
    if [ -e "$PROC/net/tcp6" ]; then
        [ -r "$PROC/net/tcp6" ] || die 'cannot inspect IPv6 port occupancy'
        set -- "$@" "$PROC/net/tcp6"
    fi
    if awk '$2 ~ /:08AE$/ && $4=="0A" {found=1} END{exit !found}' "$@"; then
        return 0
    else
        result=$?
        [ "$result" = 1 ] || die 'port occupancy inspection failed'
        return 1
    fi
}
check_elf() {
    local words flags executable
    executable=$1
    words=$(od -An -tu1 -N20 "$executable")
    set -- $words
    [ "$#" = 20 ] || die 'short ELF header'
    [ "$1 $2 $3 $4 $5 $6" = '127 69 76 70 1 1' ] || die 'expected little-endian ELF32'
    shift 18
    [ "$1 $2" = '40 0' ] || die 'ELF machine is not ARM'
    words=$(od -An -tu1 -j36 -N4 "$executable")
    set -- $words
    [ "$#" = 4 ] && [ "$4" = 5 ] || die 'expected EABI5 flags'
    flags=$(( $1 + 256 * $2 ))
    [ $((flags & 1024)) = 0 ] || die 'hard-float ABI rejected'
}
STATE=$ROOT/.stage1-state
LOCK=$ROOT/.stage1-lock

if [ "$ACTION" != stop ]; then
    for name in bin/sh bin/busybox usr/sbin/dropbear usr/bin/dropbearkey etc/passwd etc/group etc/shells etc/stage1-files.sha256; do
        [ -f "$ROOT/$name" ] || die "missing $name"
        case "$(readlink -f "$ROOT/$name")" in "$ROOT"/*) ;; *) die "escaping file link $name";; esac
    done
    for name in proc dev run root root/.ssh etc/dropbear; do
        [ -d "$ROOT/$name" ] && [ ! -L "$ROOT/$name" ] || die "expected real directory $name"
        [ "$(readlink -f "$ROOT/$name")" = "$ROOT/$name" ] || die "symlink component in $name"
    done
    check_elf "$ROOT/bin/busybox"
    check_elf "$ROOT/usr/sbin/dropbear"
    (cd "$ROOT"; sha256sum -c etc/stage1-files.sha256 >/dev/null) || die 'rootfs integrity check failed'
    awk -v p="$ROOT" '$5==p || index($5,p"/")==1 {bad=1} END{exit bad}' "$PROC/self/mountinfo" || die 'pre-existing rootfs mounts: use stop or inspect manually'
    [ -z "$(root_pids)" ] || die 'processes already use this rootfs'
    if [ "$ACTION" = preflight ]; then
        [ "$#" = 0 ] || die 'preflight takes no extra arguments'
        echo 'Preflight passed; kernel runtime compatibility remains unverified. No mutations performed.'
        exit 0
    fi
fi

BIND=
PTY=yes
case "$ACTION" in
    probe)
        [ "$#" -ge 1 ] && [ "$#" -le 2 ] && [ "$1" = --accept-unverified-kernel ] || die 'probe requires --accept-unverified-kernel [--no-pty]'
        if [ "$#" = 2 ]; then [ "$2" = --no-pty ] || die 'unknown probe option'; PTY=no; fi
        ;;
    start)
        [ "$#" -ge 2 ] && [ "$#" -le 3 ] && [ "$2" = --accept-unverified-kernel ] || die 'start requires BIND_IPV4 --accept-unverified-kernel [--no-pty]'
        if [ "$#" = 3 ]; then [ "$3" = --no-pty ] || die 'unknown start option'; PTY=no; fi
        BIND=$1
        echo "$BIND" | awk -F. 'NF!=4{exit 1} {for(i=1;i<=4;i++)if($i!~/^[0-9]+$/ || $i>255)exit 1; if($1==0 || $1>=224)exit 1}' || die 'explicit unicast IPv4 required'
        [ -s "$ROOT/root/.ssh/authorized_keys" ] && [ ! -L "$ROOT/root/.ssh/authorized_keys" ] || die 'administrator public key missing'
        [ "$(stat -c '%u:%a' "$ROOT/root/.ssh/authorized_keys")" = 0:600 ] || die 'authorized_keys must be root-owned mode 0600'
        [ -r "$PROC/net/tcp" ] || die 'cannot inspect IPv4 port occupancy'
        if port_in_use; then die 'port 2222 already has a listener; nothing stopped'; fi
        ;;
    stop) [ "$#" = 0 ] || die 'stop takes no extra arguments';;
esac

[ ! -L "$STATE" ] || die 'state path is a symlink'
if [ -e "$STATE" ]; then
    [ "$(stat -c '%u:%a' "$STATE")" = 0:700 ] || die 'state directory ownership/mode invalid'
else
    mkdir -m 700 "$STATE"
fi
mkdir -m 700 "$LOCK" 2>/dev/null || die 'controller lock exists; inspect any previous interrupted operation'
ROLLBACK=no
cleanup_environment() {
    local pid current id kind point
    # No killall/pkill: only processes whose /proc/PID/root matches this exact root.
    for pid in $(root_pids); do
        [ "$(readlink "$PROC/$pid/root" 2>/dev/null || :)" != "$ROOT" ] || kill -TERM "$pid" 2>/dev/null || :
    done
    sleep 1
    for pid in $(root_pids); do
        [ "$(readlink "$PROC/$pid/root" 2>/dev/null || :)" != "$ROOT" ] || kill -KILL "$pid" 2>/dev/null || :
    done
    sleep 1
    [ -z "$(root_pids)" ] || { echo 'stage1: rootfs still in use; mounts retained' >&2; return 1; }
    if [ -f "$STATE/mounts" ]; then
        awk '{a[NR]=$0} END{for(i=NR;i;i--)print a[i]}' "$STATE/mounts" > "$STATE/unmount-list" || return 1
        while IFS='|' read -r id kind point; do
            [ -n "$point" ] || continue
            case "$point" in "$ROOT/proc"|"$ROOT/dev"|"$ROOT/dev/pts"|"$ROOT/run") ;; *) return 1;; esac
            current=$(mount_id "$point")
            if [ -n "$current" ]; then
                [ "$current" = "$id" ] || { echo "stage1: foreign/replaced mount at $point; retained" >&2; return 1; }
                umount "$point" || return 1
            elif awk -v p="$point" '$5==p{found=1} END{exit !found}' "$PROC/self/mountinfo"; then
                echo "stage1: ambiguous stacked mounts at $point; retained" >&2
                return 1
            fi
        done < "$STATE/unmount-list"
        : > "$STATE/mounts"
        rm -f "$STATE/unmount-list"
    fi
    rm -f "$STATE/daemon.pid"
}
finish() {
    status=$?
    trap - EXIT HUP INT TERM
    if [ "$ROLLBACK" = yes ]; then
        cleanup_environment || status=1
    fi
    rmdir "$LOCK" 2>/dev/null || status=1
    exit "$status"
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
if [ "$ACTION" = stop ]; then
    cleanup_environment
    echo 'Stage 1 processes stopped and owned mounts removed; host services unchanged.'
    exit 0
fi
[ ! -s "$STATE/mounts" ] || die 'unfinished mount ledger; run stop or inspect before starting'
: > "$STATE/mounts"
ROLLBACK=yes
mount_owned() {
    local kind point options id
    kind=$1 point=$2 options=$3
    mount -t "$kind" -o "$options" "$kind" "$point"
    id=$(mount_id "$point")
    if [ -z "$id" ]; then
        umount "$point" || :
        die 'cannot track newly created mount'
    fi
    printf '%s|%s|%s\n' "$id" "$kind" "$point" >> "$STATE/mounts" || { umount "$point"; return 1; }
}
mount_owned proc "$ROOT/proc" nosuid,nodev,noexec
mount_owned tmpfs "$ROOT/run" mode=0755,nosuid,nodev,noexec
mount_owned tmpfs "$ROOT/dev" mode=0755,nosuid,noexec
if [ "$PTY" = yes ]; then
    mkdir "$ROOT/dev/pts"
    # newinstance requires CONFIG_DEVPTS_MULTIPLE_INSTANCES on this old kernel.
    # Fail and roll back if unsupported; never bind host /dev or /dev/pts.
    mount_owned devpts "$ROOT/dev/pts" newinstance,nosuid,noexec,ptmxmode=0666,mode=0620,gid=5
    [ -c "$ROOT/dev/pts/ptmx" ] || die 'private devpts ptmx node unavailable; consider an explicitly selected --no-pty test'
fi
for specification in 'null 1 3' 'zero 1 5' 'random 1 8' 'urandom 1 9' 'tty 5 0'; do
    set -- $specification
    mknod "$ROOT/dev/$1" c "$2" "$3"
    chmod 666 "$ROOT/dev/$1"
done
if [ "$PTY" = yes ]; then ln -s pts/ptmx "$ROOT/dev/ptmx"; fi
ln -s /proc/self/fd "$ROOT/dev/fd"
chroot "$ROOT" /bin/busybox true
if [ "$ACTION" = probe ]; then
    chroot "$ROOT" /bin/busybox env -i HOME=/root PATH=/usr/sbin:/usr/bin:/sbin:/bin \
        /bin/sh -c 'echo "NAS Revival shell probe"; id; uname -a; ip -4 addr show; ip -4 route show'
    exit 0
fi
chroot "$ROOT" /bin/busybox ip -4 addr show | grep -F "inet $BIND/" >/dev/null || die 'bind address is not currently assigned; no network changes made'
hostkey=$ROOT/etc/dropbear/dropbear_ed25519_host_key
[ ! -L "$hostkey" ] || die 'host key path is a symlink'
if [ ! -e "$hostkey" ]; then
    entropy=$(cat "$PROC/sys/kernel/random/entropy_avail")
    case "$entropy" in ''|*[!0-9]*) die 'cannot establish entropy availability';; esac
    [ "$entropy" -ge 256 ] || die 'insufficient reported entropy; host key not generated'
    chroot "$ROOT" /usr/bin/dropbearkey -t ed25519 -f /etc/dropbear/dropbear_ed25519_host_key > "$STATE/hostkey-public.txt"
    chmod 600 "$hostkey"
fi
[ "$(stat -c '%u:%a' "$hostkey")" = 0:600 ] || die 'host key ownership/mode invalid'
if port_in_use; then die 'port 2222 became occupied; nothing stopped'; fi
chroot "$ROOT" /bin/busybox env -i HOME=/root PATH=/usr/sbin:/usr/bin:/sbin:/bin \
    /usr/sbin/dropbear -F -E -s -j -k -p "$BIND:2222" \
    -r /etc/dropbear/dropbear_ed25519_host_key -P /run/dropbear.pid > "$STATE/dropbear.log" 2>&1 &
pid=$!
echo "$pid" > "$STATE/daemon.pid"
sleep 1
kill -0 "$pid" 2>/dev/null && [ "$(readlink "$PROC/$pid/root" 2>/dev/null || :)" = "$ROOT" ] || die 'Dropbear startup failed; see private .stage1-state/dropbear.log'
port_in_use || die 'Dropbear has not opened port 2222'
ROLLBACK=no
echo "Stage 1 SSH listening on $BIND:2222; verify the NEW host key through the existing Telnet session."
if [ "$PTY" = no ]; then echo 'No PTY devices exposed: connect with ssh -T; terminal allocation will fail.'; fi
echo 'Existing Telnet, SMB, web, addresses, routing, DNS and firewall were not configured by this controller.'
