# ADR 0016: Debian updates through a root job unit

- Status: accepted (2026-11-27, W5.4 phase 1)
- Scope: UPD-01 (check, list with security updates marked, install, restart recommendation), the advisor
  checks `updates.pending` / `updates.stale` (SEC-13). Not in scope: NAS Revival self-update (UPD-02) and kernel
  update from the web (UPD-04) – they need the signed release (open decision in OPEN.md).
- Context: the NAS runs Debian 13 armel from `deb.debian.org` and `security.debian.org` (`trixie`,
  `trixie-updates`, `trixie-security`; kernel and boot loader are not Debian packages, they live in NAND). Nothing
  installs security updates today; `apt-daily*.timer` exist but do nothing without `APT::Periodic` settings and
  `unattended-upgrades` is not installed. 240 MiB RAM + 1 GiB swap; `apt-get -s upgrade` takes ~8 s and ~36 s CPU
  together with `apt-get update`. An apt run that is killed half-way leaves dpkg half-configured.

## Decision

- **apt runs in its own unit, not in the helper.** `nas-revival-updates@.service` (instances `check`, `upgrade`;
  root; `ExecStart=nas-revival updates run %i`) is started by the helper with `systemctl start --no-block`. It is
  outside the helper's cgroup, so a restart or redeploy of the helper or the web service never kills dpkg. On stop
  (shutdown) only the runner gets SIGTERM (`KillMode=mixed`), dpkg has 30 min to finish. Both instances take one
  `flock` in the state directory and never overlap. Restart and shutdown are blocked (`package_manager`) while the
  upgrade unit runs, not only while an apt/dpkg process exists.
- **Fixed commands, no options from the request.** Environment `LANG=C`, `DEBIAN_FRONTEND=noninteractive`,
  `APT_LISTCHANGES_FRONTEND=none`; every apt-get call has `-q -o DPkg::Lock::Timeout=300` (waits for a manual
  apt run instead of failing).
  - `check`: `apt-get -o Acquire::Languages=none update` (no translations: −35 MB of lists), then
    `apt-get -s upgrade --with-new-pkgs` (parsed `Inst` lines and the kept-back list) and
    `apt-get --print-uris -qq upgrade --with-new-pkgs` (download size per package). An index that could not be
    fetched (`Err:`, `W: Failed to fetch`) is a check error; the list from the old indexes is still shown.
  - `upgrade`: `dpkg --configure -a` (finishes an interrupted run; no-op otherwise), then
    `apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade --with-new-pkgs`,
    then `apt-get clean` and a new simulation. Progress = `Setting up` lines against the listed count.
  - Only `upgrade --with-new-pkgs`: never removes a package and never changes the Debian release (no
    `dist-upgrade`, no `full-upgrade`); packages that need a removal stay "kept back" and are shown for manual
    handling over SSH. `--force-confold` keeps the administrator's (and the helper's) configuration files; this is
    the UPD-03 protection for now – no separate snapshot before an upgrade.
- **State file, not a socket.** The job writes `/var/lib/nas-revival-updates/state.json` (root, 0644, package names,
  versions, sizes, last run, no secrets) atomically; the helper only reads it. `security` = the candidate's origin
  label contains `-security`. `listedAt` identifies the list: `updates.upgrade` must name it, so the user installs
  what they saw (`updates_stale` otherwise). An upgrade marked running whose unit is no longer active is reported
  as interrupted.
- **Preconditions** in the helper before the upgrade starts: no check/upgrade running, list present and current,
  at least 300 MiB + 3 × download size free on `/`, at least 128 MiB `MemAvailable + SwapFree`.
- **Restart recommendation**: Debian without `needrestart` has no reliable signal, so the job records upgraded
  packages from a fixed list of libraries every long-running process uses (libc6, systemd, libssl3t64, PAM, zlib,
  …) together with the boot time; the recommendation disappears after the next boot. `/run/reboot-required`, when
  a package creates it, also counts. The web UI only recommends; the restart is the existing power dialog.
- **Daily check**: `nas-revival-updates-check.timer` (04:40 + up to 45 min, `Persistent=true`) runs only the
  `check` instance – nothing is installed automatically. The advisor warns about pending security updates
  (`updates.pending`) and about a list older than 7 days or never checked (`updates.stale`).
- **API**: `GET /api/v1/updates` (administrator and read-only administrator), `POST /updates/check` and
  `POST /updates/upgrade {"listedAt"}` (administrator, `202`, audit `updates.check` / `updates.upgrade`). The
  response includes the NAS Revival and kernel version (read-only).

## Consequences

- The daily check wakes the system disk once a night (the only disk of the NAS also holds the data).
- Package installation is root code from Debian's archive; the helper never passes request data to apt.
- Self-update and kernel update need the release pipeline first (UPD-02/04); until then the kernel is updated with
  `scripts/debian/update-kernel.sh` over SSH.
