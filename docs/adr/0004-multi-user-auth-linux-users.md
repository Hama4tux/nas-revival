# ADR 0004: Web accounts are Linux users, roles via groups

- Status: accepted by the owner, 2026-11-25 (implementation in M2, web roadmap W2.1–W2.2)
- Context: the plan requires administrators, read-only administrators and standard users, tied to
  the same identities that Samba and SFTP use, without parallel virtual users. The web binary is
  static Go without cgo, so it cannot call PAM or verify the yescrypt hashes in `/etc/shadow`.

## Decision

- A NAS account is a Linux user (uid ≥ 1000, member of `nas-users`). System accounts (uid < 1000,
  `root`, `nas-revival`) are never editable from the UI.
- Roles are Linux groups: `nas-admin` = administrator, `nas-viewer` = read-only administrator,
  otherwise standard user. A user is in at most one of the two admin groups.
- One password is stored as three hashes, all written by the helper in one operation
  (`user.setPassword`, password only on stdin, never logged or in argv): Linux shadow
  (`chpasswd`), Samba (`smbpasswd -s`), web argon2id in `/etc/nas-revival/credentials.json`
  (0600, keyed by Linux username). If one step fails, the earlier ones are rolled back where
  possible and the error lists which service still has the old password.
- The existing single `admin.json` account is migrated to a Linux user in `nas-admin` with the same
  web hash; Linux/Samba hashes are set on the first password change.
- All sessions of a user are revoked on password change, role change, disable or delete.
- argon2id verification is serialized (one at a time) to cap RAM (16 MiB each).

## Consequences

- Disabling a user must disable all three (shadow lock, `smbpasswd -d`, web flag).
- Changing a password outside the UI (`passwd`) desynchronizes the web hash; the UI shows the
  account as "password changed outside, set again". Implemented (W2.1) with a SHA-256 fingerprint
  of the shadow hash stored next to the web hash (lock prefix ignored) instead of the change date,
  which has only day resolution.
- `credentials.json` is written only by the helper and owned by `nas-revival` (0600), like
  `admin.json`, so the web login (W2.2) reads it directly without an `auth.verify` op.
- NAS accounts get primary group `nas-users`, no home directory (`/nonexistent`) and shell
  `/usr/sbin/nologin`; editable groups are gid 1000–59999 without the `nas-` prefix and without
  private groups of non-NAS accounts. Memberships the UI does not manage are kept.
- The shadow tools write lock and temp files next to `/etc/passwd`, so the helper unit needs
  `ReadWritePaths=/etc` (previously only `/etc/systemd/network` and `/etc/nas-revival`).
