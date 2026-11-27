# ADR 0009: File Station through the helper

- Status: accepted (2026-11-26, W4.1 phase 1)
- Scope: FS-01..03 (browse, folder operations, streamed upload/download); FS-04 copy jobs and cancel, FS-05 later
- Context: the web process runs as `nas-revival` and has no access to the shares (folders are
  `root:nas-users 0770`; on the NAS data volume without ACLs Samba enforces the per-user permissions).
  Every NAS account must be able to work with the files it may use over SMB, never with more.

## Decision

- All file operations are helper ops `fs.*` with the session user; the web process never opens a path.
  Arguments are a share name plus a path relative to it, validated like the backup browse paths
  (clean, relative, UTF-8, no `.`/`..`, no control characters) and resolved through `os.Root` on the
  share folder, so neither `..` nor a symlink can leave the share. `.recycle` and unfinished uploads
  (`.nas-upload-<token>`) are hidden and refused.
- Access is the SMB matrix (`shares.Effective`: user entry wins, else the best group, read-only share
  caps to ro). ~~Admins get no implicit access. The recovery administrator (`admin.json`, no Linux
  account) has no files.~~ Amended 2026-11-27 (owner): administrators – members of `nas-admin` and the
  recovery administrator – see every share in the File Station with read/write (a read-only share stays
  read-only), without an entry in the matrix; they could grant it to themselves anyway. Entries the
  recovery administrator creates belong to root with group `nas-users`. SMB and SFTP keep the matrix. Unknown and forbidden shares give the same 404.
- The helper works as root with explicit checks instead of `setfsuid` (Go threads make per-thread fs
  ids fragile). New entries are `chown`ed to the user and group `nas-users`; without ACLs they get the
  Samba masks 0660/0770, with ACLs the default ACL applies. In SMB mode the share-level check is the
  only enforcement anyway (every file is `nas-users` 0660).
- New names follow Windows-safe rules (no `/ \ : * ? " < > |`, no leading/trailing space, no trailing
  dot, ≤ 255 bytes). Renames and moves use `renameat2(RENAME_NOREPLACE)`; existing names are never
  overwritten except by an upload with explicit overwrite of a regular file.
- Delete moves to `.recycle/<user>/<path>` (Samba layout and `Copy #n of` naming, mtime set to the
  deletion time so the recycle task counts from it) when the share has a bin, else removes for good
  (the UI says so). Batches take at most 100 paths and report per-item failures.
- Download: the helper opens the file (`O_NOFOLLOW`) and passes the descriptor (SCM_RIGHTS), as for
  backups. Upload: `fs.uploadOpen` checks free space (size + 64 MiB), creates a hidden temporary file
  owned by the user and passes its descriptor; the web streams the body into it with one 32 KiB buffer
  (`io.CopyN`, Content-Length required, 60 s idle deadline, fsync), then `fs.uploadCommit` renames it
  (access re-checked) or `fs.uploadAbort` removes it. At most 2 uploads at once (429 otherwise).

## Consequences

- API: `/api/v1/files…` for role user and above (see API_DESIGN). No jobs yet: copy, cross-share move
  and cancel of long operations are FS-04.
- A crash during an upload can leave a hidden `.nas-upload-*` file; nothing cleans it yet.
- Uploads are bound by TLS on the single ARM core (whole CPU while streaming); the 2 GiB RSS gate of
  TEST_PLAN S3 is run only with the owner's consent.
