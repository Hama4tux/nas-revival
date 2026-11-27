#!/bin/bash
# Owner's workstation: let the NAS pull ~/.secrets read-only (backup-pull task "ubuntusecrets").
# Run on the workstation (not the NAS). Idempotent.
#   NAS=<ssh alias of the NAS> NAS_IP=<NAS IPv4> scripts/debian/setup-secrets-backup.sh
# Layout: the real data live in ~/.nas-export/secrets, ~/.secrets is a symlink to it.
# rrsync resolves symlinks and refuses targets outside its root, so the export root must hold the real dir.
# Then the task on the NAS (as root; helper-call is backend/cmd/helper-call):
#   runuser -u nas-revival -- helper-call backup.test '{"host":"<ws-ip>","port":22,"user":"<user>","path":"secrets"}'
#   helper-call task.get, append {"id":"ubuntusecrets","name":"Záloha secrets (Ubuntu)","type":"backup-pull",
#     "enabled":true,"schedule":{"kind":"daily","time":"05:30"},"backup":{"dest":{"share":"data",
#     "folder":"daily-backup"},"host":"<ws-ip>","port":22,"user":"<user>","paths":["secrets"],"keepDays":35,
#     "bwlimitKiB":0,"excludes":[]}}
#   runuser -u nas-revival -- helper-call -user <admin> task.set '{"tasks":[...]}'
set -euo pipefail
NAS="${NAS:-nsa310}"
NAS_IP="${NAS_IP:?set NAS_IP to the NAS address (authorized_keys from=)}"
EXP="$HOME/.nas-export"
command -v rrsync >/dev/null || { echo "rrsync missing (package rsync)" >&2; exit 1; }
install -d -m 700 "$EXP"
if [ -d "$HOME/.secrets" ] && [ ! -L "$HOME/.secrets" ]; then
  [ -e "$EXP/secrets" ] && { echo "$EXP/secrets exists, refusing to move ~/.secrets" >&2; exit 1; }
  mv "$HOME/.secrets" "$EXP/secrets"
  ln -s "$EXP/secrets" "$HOME/.secrets"
fi
key="$(ssh "$NAS" cat /var/lib/nas-backup/.ssh/id_ed25519.pub)"
line="from=\"$NAS_IP\",restrict,command=\"rrsync -ro $EXP\" $key"
touch "$HOME/.ssh/authorized_keys"
chmod 600 "$HOME/.ssh/authorized_keys"
grep -qF "$key" "$HOME/.ssh/authorized_keys" || echo "$line" >> "$HOME/.ssh/authorized_keys"
echo "ok: export $EXP (read-only for the NAS backup key from $NAS_IP)"
