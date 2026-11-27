# ADR 0011: Config export and import

- Status: accepted (2026-11-27, W4.4 phase 1)
- Scope: CFG-05 (export/import with schema migration), the config part of BKP-01
- Context: the config history (W1.8, ARCHITECTURE §4) keeps 20 versions of the managed files on the NAS
  itself. They are lost with the system disk, and there is no way to move a configuration to a
  reinstalled or second NAS. ARCHITECTURE §4 planned "a versioned archive, secrets only encrypted".

## Decision

- **Contents = the managed file registry**, nothing else: network (`20-wired.network`), NTP drop-in,
  `disks.json`, `shares.json`, `smb.json`, `nfs.json`, `ssh.json`. None of them holds a secret, so phase 1
  has no encrypted export. Password hashes, SSH keys of users, the TLS key, `admin.json`,
  `credentials.json` and the backup key are not managed files and never leave the NAS this way. The
  file still names accounts, groups and NFS client addresses: the export is admin-only (also as GET).
- **Format:** one JSON document `{"format": "nas-revival-config", "schema": 1, "created", "hostname",
  "files": [{"id", "present", "content"}]}` – no tar or zip parser on the import path. `present: false`
  means the file did not exist (built-in defaults). Limits: 64 KiB per file, 32 KiB per part.
- **Schema migration:** `migrateArchive` turns schema N−1 into N step by step; a newer schema than the
  build knows is refused ("update this NAS first"). Unknown fields and unknown part IDs are refused.
- **Import = rollback path.** The helper validates every part with the parser of its owner and renders it
  again (network: `RenderNetwork`, NTP: `RenderTimesyncd`, JSON: the package `Encode` that `Save` uses),
  so an imported file can only contain what the settings pages could write (no extra systemd-networkd
  directives, no unknown JSON fields). Parts missing from the file stay untouched. Then the same drift
  rule (takeover), the same owners' `Restore` (Samba, exports, sshd, hdparm re-applied) and the network
  part last through the 180 s confirm-or-rollback window. The result is one history version of kind
  `import`; the version before it is the undo.
- **Two steps, stateless:** `config.importPreview` (diff per part, drift, other host name) writes nothing;
  `config.import` sends the same file again. The helper request limit grew to 80 KiB for this.

## Consequences

- An import from another NAS is allowed but flagged (`otherHost`); a static address would collide – the
  confirm window reverts it if the new address is unreachable.
- Shares point to folders on data volumes; a share whose folder does not exist is configured but its
  permissions are not applied (as with a rollback). Accounts and groups are not created by an import.
- Not yet in the export: scheduled tasks and backup jobs (`tasks.json`, `backup.json`), hardware settings
  (`hardware.json`). They need a `Restore` with apply (timers, fan, LEDs) before they join the registry;
  then they appear in the history and the export without a format change.
- A future encrypted export (if secrets are added) gets a new schema; schema 1 stays readable.
