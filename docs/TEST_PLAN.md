# Test plan

Three levels; a feature becomes *Tested* in the [FEATURE_MATRIX](FEATURE_MATRIX.md) only after
levels 1 and 2 pass and the relevant level-3 scenarios are recorded.

| Level | Where | What | Command |
|---|---|---|---|
| 1 Unit/integration | workstation | Go packages with fake `/proc`/`/sys` trees and fake command runners; frontend logic (`lib/*.test.ts`) | `cd backend && go vet ./... && go test ./...` · `cd frontend && npm test && npm run build` |
| 2 Device smoke | NAS | deploy build, API smoke, measurement snapshot | `scripts/webui/build.sh`, deploy, `scripts/measure/snapshot.sh` |
| 3 Scenario | NAS (+ clients) | scenarios below, results dated in PERFORMANCE_BUDGET | manual, with owner consent where marked ⚠ |

## Rules for tests

- Every change has a test that fails before and passes after.
- Helper ops: table tests for each validator (valid, boundary, injection strings `; $( ) \n ../`,
  unknown fields, oversize), plus a test that the command runner received the exact argv.
- No test may touch real system files: helper and web accept root prefixes / runner interfaces for tests.
- Security tests per endpoint: no session → 401, wrong role → 403 (M2), no CSRF header → 403.
- Frontend: health, formatting and validation logic in pure functions with tests; components
  checked in the browser with mock data (`npm run dev` mock mode) and on the NAS.

## Module acceptance (summary)

Acceptance criteria per feature are in the matrix; module gates:

| Module | Gate |
|---|---|
| System Core | info matches commands; reboot/shutdown preflight blocks during SMART test ⚠ reboot |
| Config Manager | kill -9 of helper during apply → files old or new, never mixed; rollback restores byte-identical files; drift detected |
| HAL | unknown DT → generic read-only; missing hwmon → null + "unknown", no crash |
| Storage | `sdX` renumbering (USB re-plug) keeps ids; system disk ops refused by helper |
| SMART | standby disk is not woken by overview/SMART polling (check `hdparm -C` before/after) |
| Disk power | settings survive reboot ⚠ and re-plug; profile change reflected in `hdparm -B` |
| Users/Shares/SMB | per-user RW/RO/none verified from Windows, macOS, Linux clients; SMB1 refused |
| Network | wrong static IP → automatic rollback in 180 s; rollback also after reboot during pending ⚠ |
| Monitoring | 3 browsers ≈ CPU of 1; RSS flat over 7 days |
| File Station | `..`, absolute, symlink-out paths rejected; 2 GiB upload with flat RSS |
| Backup | dry run makes no changes; no `--delete` unless mirror confirmed; USB unplug mid-run → failed job, no corruption of source |
| Updates | unsigned/foreign-arch package rejected before any write; kernel write refuses when not booted from slot A |

## Scenario catalogue (level 3)

| # | Scenario | Pass criteria | Milestone |
|---|---|---|---|
| S1 | Large file copy over SMB with dashboard open | throughput drop ≤ 5 % vs. closed; no swap storm | M2 |
| S2 | 3–5 SMB clients at once | no OOM, MemAvailable > 40 MiB | M2 |
| S3 | 2 GiB upload via File Station | RSS growth < 4 MiB, completes | M4 |
| S4 | Backup during normal use | UI responsive (< 1 s API), backup completes | M4 |
| S5 | Data disk sleep with monitoring active | disk stays in standby ≥ 30 min with UI open (needs ADR 0007) | M3 |
| S6 | `kill -9` of web backend | systemd restarts in ≤ 10 s; helper unaffected; no half-written files | M1 |
| S7 | Network cable unplugged 2 min | services recover, no crash loop | M2 |
| S8 | Invalid service config | rejected by validator (testparm/sshd -t/exportfs), old config active | M2 |
| S9 | Failed IP change | rollback in 180 s, UI reconnects | M1-00 |
| S10 | Low memory (`stress-ng --vm` or hog) | web degrades (503) but OOM killer does not pick sshd/helper first (`OOMScoreAdjust`) | M3 |
| S11 | Full filesystem (data and root) | UI reports, no crash, config writes fail cleanly with error | M2 |
| S12 | USB disk removed while mounted | marked missing, no hang of overview | M4 |
| S13 | Failed update (simulated bad package) | previous version still runs, error reported | M5 |
| S14 | Reboot / cold start ⚠ | all enabled services up, settings (disk power, fan, LED) applied | each milestone |
| S15 | 7-day idle run | RSS of web/helper flat (±2 MiB), journal ≤ limit, no growing files in state dirs | M3, M5 |

## Current status (2026-11-25)

- Level 1: backend and frontend unit tests exist and pass at the last commit (see CHANGELOG).
- Level 2: read-only overview and SMART verified on the NAS; baseline measurement recorded.
- Level 3: none of S1–S15 executed yet.
