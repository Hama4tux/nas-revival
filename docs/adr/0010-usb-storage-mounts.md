# ADR 0010: USB storage mounts through systemd

- Status: accepted (2026-11-26, W4.2 phase 1)
- Scope: STOR-USB-01 (detect), STOR-USB-02 (mount, safe eject, share); STOR-USB-03 (backup target) and
  automount later
- Context: the helper runs in its own mount namespace (ProtectSystem=strict, PrivateTmp); a `mount` it runs
  would never reach the host, Samba or the web. `/sys` is read-only for it (ProtectKernelTunables). The NAS
  kernel has only ext4 (also mounts ext2/3) and vfat; no exFAT or NTFS.

## Decision

- `usb.list` builds on the storage report (sysfs + udev, no disk access): only disks on the USB bus, per
  partition the filesystem, label, mount and why it may not be mounted (`blocked`): rescue stick (UUID in
  `/etc/nsa310-copy.exclude`), disk swap stick (label `nrinstall`), swap, no or unsupported filesystem
  (exFAT/NTFS named explicitly), mounted elsewhere (e.g. the COPY job).
- `usb.mount {diskId, partition, readOnly}` re-reads the report, accepts only a partition of that USB disk
  and runs `systemd-mount --collect --fsck=no -t <fs> -o nosuid,nodev,noexec,noatime[,ro] /dev/<part>
  /srv/usb/<label>`. systemd creates the mount in the host namespace and stops it when the device goes away.
  FAT gets `uid=0,gid=nas-users,fmask=0117,dmask=0007` (the Samba fallback modes); ext keeps its own owners.
  The name is the sanitized label (≤ 32 chars, else `usb-<part>`), `-2`… when a mount or non-empty folder
  has it; an empty folder of an earlier mount is reused. No fsck: it could take minutes and changes foreign
  media.
- `usb.unmount` only unmounts what is below `/srv/usb` (`systemd-umount`); busy → 409 with a hint.
- `usb.eject {diskId}` refuses while another job has a partition mounted, unmounts ours, then starts
  `nas-revival-usb-eject@<sdX>.service` (outside the sandbox): checks USB and no mounts, `sync`,
  `blockdev --flushbufs`, `echo 1 > /sys/block/<sdX>/device/delete`.
- `/srv/usb/<name>` is a data volume for shared folders (vfat added to the data filesystems); on FAT the
  helper skips chown/chmod of the share folder, the mount options already give root:nas-users 0770.

## Consequences

- A share on a stick that is not mounted is unavailable (Samba logs it, File Station says the folder is
  missing) until the stick is mounted again under the same name.
- exFAT/NTFS need a kernel with `CONFIG_EXFAT_FS`/`CONFIG_NTFS3_FS` (NAND write, owner decision).
- Mounts do not survive a reboot (no automount yet).

## Addendum 2026-11-27: destination of the COPY button

- The COPY job no longer writes to the fixed `/srv/data/usb-zalohy`. The destination is a share with the
  `backupTarget` option plus an optional subfolder (UI default `usb-backups`), set in Storage -> USB.
  `usb.copy.get` / `usb.copy.set {share, folder}` (admin; the viewer may read through the API) validate
  it (share exists, `backupTarget`, on `/srv/data`, folder = share name rules), create the folder inside
  the share (0770, inherits the default ACL) and write `/etc/nas-revival/usbcopy.json` plus
  `/etc/nas-revival/usbcopy.dest` (one line: the absolute folder). The shell script cannot parse JSON or
  `shares.json`, so it reads only the `.dest` file.
- `nsa310-copy` accepts the folder only when it starts with `/srv/data/`, has no `..` and is a real
  directory reached without symlinks (`readlink -f` equals the path); otherwise it logs "no copy
  destination set", lights the red LED and exits 1. The unset default behaves the same.
- Layout: `<dest>/<vendor>_<model>_<serial>/` (from `/sys/block/<sd>/device/vendor|model` and the USB
  serial, characters outside `A-Za-z0-9._-` become `_`, repeats collapsed; no serial -> first 8 characters
  of the first partition UUID). One usable filesystem: versions `<YYYY-MM-DD_HHMMSS>` directly in it;
  several: `<label or partN>_<uuid>/<stamp>`. `--link-dest` points to the newest version of the same
  folder; rsync uses `--chmod=D750,F640` so the share's users read the copies through the ACL.
- The share manager hook `InUseAsBackupTarget` also names the COPY button, so the share cannot be
  deleted or lose `backupTarget` while it is the destination. The setting is not part of the config
  export yet.
