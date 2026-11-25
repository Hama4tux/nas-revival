# Debian deployment of the web UI

Deployed to the NAS on 2026-11-25 (about 24 MiB RSS after start). Build on the workstation with
`scripts/webui/build.sh` (output in `output/webui/`), then on the NAS as root:

```sh
apt install smartmontools            # optional, for the SMART card
useradd --system --no-create-home --shell /usr/sbin/nologin nas-revival
install -m 0755 nas-revival-armv5 /usr/local/bin/nas-revival
install -m 0644 nas-revival.service /etc/systemd/system/
install -d -m 0700 -o nas-revival -g nas-revival /etc/nas-revival
nas-revival set-password -config /etc/nas-revival     # at least 12 characters
chown nas-revival:nas-revival /etc/nas-revival/admin.json
systemctl daemon-reload && systemctl enable --now nas-revival
```

Fan control (independent of the web UI; the LM85 regulates the fan itself once configured):

```sh
install -m 0755 nsa310-fan /usr/local/sbin/nsa310-fan
install -m 0644 nsa310-fan.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now nsa310-fan
```

Installed on the NAS 2026-11-25. Thresholds are in the script header.

COPY button backup (`nsa310-button` is built from `backend/cmd/nsa310-button` with
`GOARCH=arm GOARM=5`; needs `apt install rsync`):

```sh
install -m 0755 nsa310-button-armv5 /usr/local/bin/nsa310-button
install -m 0755 nsa310-copy /usr/local/sbin/nsa310-copy
install -m 0644 nsa310-copy.service nsa310-button.service /etc/systemd/system/
blkid -o value -s UUID /dev/sdX1 >> /etc/nsa310-copy.exclude   # every rescue stick partition
systemctl daemon-reload && systemctl enable --now nsa310-button
```

Installed on the NAS 2026-11-25. `/etc/nsa310-copy.exclude` stays on the device only.

The UI listens on HTTPS port 8443 with a self-signed certificate generated on first start
in `/var/lib/nas-revival/tls` (replace `cert.pem`/`key.pem` to avoid the browser warning).
There is one administrator account, independent of Linux and Samba users.

To verify on the device: memory use, `systemctl show` inside the sandbox, and whether
`smartctl` works with only `CAP_SYS_RAWIO` and group `disk`.
