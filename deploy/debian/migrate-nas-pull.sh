#!/bin/bash
# One-off migration of the interim nas-pull job (/etc/nas-pull/jobs.conf) to backup-pull tasks (W4.3).
# Run as root on the NAS after installing the new nas-revival binary and nas-revival-backup@.service.
# Idempotent; prints no keys or secrets. Usage: migrate-nas-pull.sh [HH:MM]   (daily start, default 06:45)
set -euo pipefail

JOBS=/etc/nas-pull/jobs.conf
OLD_HOME=/var/lib/nas-pull
OLD_KNOWN=/etc/nas-pull/known_hosts
NEW_HOME=/var/lib/nas-backup
NEW_KNOWN=/etc/nas-revival/backup_known_hosts
DATA=/srv/data
AT="${1:-06:45}"

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
[ -r "$JOBS" ] || { echo "missing $JOBS" >&2; exit 1; }
[[ "$AT" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || { echo "bad time $AT" >&2; exit 1; }

if systemctl is-active --quiet nas-pull.service; then
    echo "nas-pull.service is running; wait until it finishes" >&2; exit 1
fi

echo "== account nas-backup"
getent group nas-heavy >/dev/null || groupadd --system nas-heavy
getent group nas-backup >/dev/null || groupadd --system nas-backup
if ! getent passwd nas-backup >/dev/null; then
    useradd --system --gid nas-backup --home-dir "$NEW_HOME" --no-create-home --shell /usr/sbin/nologin nas-backup
fi
usermod -aG nas-heavy nas-backup
install -d -m 0700 -o nas-backup -g nas-backup "$NEW_HOME" "$NEW_HOME/.ssh"

echo "== ssh key"
if [ -f "$OLD_HOME/.ssh/id_ed25519" ] && [ ! -f "$NEW_HOME/.ssh/id_ed25519" ]; then
    install -m 0600 -o nas-backup -g nas-backup "$OLD_HOME/.ssh/id_ed25519" "$NEW_HOME/.ssh/id_ed25519"
    if [ -f "$OLD_HOME/.ssh/id_ed25519.pub" ]; then
        install -m 0644 -o nas-backup -g nas-backup "$OLD_HOME/.ssh/id_ed25519.pub" "$NEW_HOME/.ssh/id_ed25519.pub"
    fi
    echo "key copied"
else
    echo "key unchanged (already migrated or no old key; the web UI creates one)"
fi

echo "== known hosts"
install -d -m 0755 /etc/nas-revival
if [ -f "$OLD_KNOWN" ]; then
    tmp="$(mktemp /etc/nas-revival/.backup_known_hosts.XXXXXX)"
    # Union of the old file and what is already trusted; sort -u keeps it idempotent.
    if [ -f "$NEW_KNOWN" ]; then
        cat "$OLD_KNOWN" "$NEW_KNOWN" | grep -v '^[[:space:]]*$' | sort -u >"$tmp" || true
    else
        grep -v '^[[:space:]]*$' "$OLD_KNOWN" | sort -u >"$tmp" || true
    fi
    chmod 0644 "$tmp"
    mv -f "$tmp" "$NEW_KNOWN"
fi

echo "== data directories"
install -d -m 0700 -o nas-backup -g nas-backup "$DATA/apps" "$DATA/versions"
chown -R nas-backup:nas-backup "$DATA/apps" "$DATA/versions"

echo "== tasks"
/usr/local/bin/nas-revival task import-pull -time "$AT" "$JOBS"

echo "== old timer"
systemctl disable --now nas-pull.timer 2>/dev/null || true

echo "done; check Tasks in the web UI and run the new task once"
