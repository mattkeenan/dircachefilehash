# dcfhfix non-destructive fix-to-new-file - Maintenance
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Define ongoing maintenance and operator-support concerns for the `dcfhfix`
non-destructive default. This is a local CLI binary — no service uptime, SLA,
on-call, or autoscaling — so the maintenance surface is code-level
(known limitations, follow-up work) and operator-level (what the new files and
messages mean, how to recover/clean up).

## Monitoring Requirements
Not applicable in the service sense (no running process to monitor). The
in-band signals are:
- **Operator-facing**: `dcfhfix` prints where the original was preserved
  (`.pre-fix-<UTC>` sibling) on the default path, and a prominent
  destructive-action warning on the `--edit-in-place` path. These messages are
  the "telemetry" — they tell the operator exactly what happened to their index.
- **CI/dev gate**: `golangci-lint run ./...` (gosec floor) + `go test ./...` +
  the `-race -short` gate guard the code on every commit. The
  `cwf-security-reviewer-changeset` agent covers semantic regressions per task.

## Maintenance Tasks
### Known Limitations (carry-forward)
- **`.pre-fix-<UTC>` siblings accumulate**: every default-path repair writes a
  new preserved sibling next to the index; there is **no automatic pruning**.
  Repeated `dcfhfix` runs against the same index leave a growing set of
  `<index>.pre-fix-*` files. Operators prune them manually (see Troubleshooting).
  A future enhancement could add a retention/cleanup subcommand mirroring the
  `fixes` stack — not in scope for this task.
- **Collision-suffix bound = 100**: `preserveOriginal` tries `base`, then
  `base-1`…`base-100`. Exhausting all 101 candidates (same-second reruns or
  stale occupants) is a **hard refusal** with no rename — by design, but worth
  knowing when triaging a "could not preserve" error.
- **Residual test gap**: `preserveOriginal` is 75.8% covered; the
  `Sync`/`Close`/`Lstat`-error defensive branches need a fault-injection seam not
  yet warranted. Revisit if those branches ever change.

### Follow-up Work (tracked elsewhere)
- **Very High backlog — "Re-enable checkptr in the race gate"**: the `-race`
  gate runs with `-d=checkptr=0`, so it cannot catch genuine pointer-arithmetic
  bugs tree-wide. Filed during this task (2026-06-04). See
  [[project_race_checkptr_disabled]]. Not a regression from this feature, but the
  gate's reduced power is a standing maintenance concern.
- **Dead-code audit**: include `cmd/dcfhfix/promote.go` in the next periodic
  sweep (`.cwf/docs/dead-code-audit.md`) — all five functions are reachable from
  the four write paths and dry-run branches, but confirm on the next pass.

### Routine
- Re-run the `dcfhfix` suite (`go test ./cmd/dcfhfix/...`) on any change to the
  four write paths (`main.go`, `entry_workflow_main.go`, `entry_append_remove.go`)
  or to `promote.go`; the preserve-before-rename ordering (NFR5) is the property
  most easily broken by an unrelated refactor.

## Incident Response
### Common Issues
- **"Where did my repaired index go? There's an extra `.pre-fix-…` file."**
  Working as intended. The repaired index is at the **original path** (atomic
  rename unchanged); the `.pre-fix-<UTC>` sibling is the **preserved pre-repair
  original**. Nothing was lost.
- **`dcfhfix` refused: sibling path occupied by a non-regular file.**
  The intended `.pre-fix-<UTC>` (or a `-N` variant) path is a symlink/dir/device,
  not a regular file. This is a deliberate safety refusal (TC-U3/U4) — `dcfhfix`
  will not write through it. Remove/relocate the occupant and retry.
- **`dcfhfix` refused: preservation bound exhausted.**
  All of `base`…`base-100` exist. Prune stale `.pre-fix-*` siblings (below) and
  retry, or use `--edit-in-place --force` if in-place is genuinely wanted.
- **"I want the old in-place behaviour back."**
  Pass `--edit-in-place` (requires `--force`). Without `--force` the gate refuses
  and the filesystem is untouched (TC-I4).

### Troubleshooting Guide
- **Recover the pre-repair index**: copy the most recent
  `<index>.pre-fix-<UTC>` back over `<index>` (the siblings are byte-identical
  copies of the original, verified by TC-U2/I1).
- **Prune accumulated siblings** (manual, operator-owned):
  inspect `ls -1 <index>.pre-fix-*` and delete the ones you no longer need.
  Keep the newest if you still want a recovery point.
- **Confirm a sibling is intact**: it should be a regular file the same size as
  the index it was copied from at repair time.

### Escalation
N/A — single local tool. "Escalation" is filing a BACKLOG.md item (e.g. the
checkptr gate item) or a new CWF task for an enhancement (e.g. sibling retention).

## Documentation
- **Help text**: `dcfhfix --help` documents the non-destructive default and
  `--edit-in-place`.
- **DESIGN**: `cmd/dcfhfix/DESIGN.md` "Safety Features" records the
  non-destructive-by-default contract.
- **Tests as runbook**: `cmd/dcfhfix/promote_test.go` /
  `promote_integration_test.go` are the executable specification of every
  behaviour above (default preserve, in-place suppress, gate refusal, dry-run,
  failure ordering, message/quiet).

## Success Criteria
- [x] Maintenance surface defined for a local-CLI reality (no service monitoring)
- [x] Known limitations + follow-up work documented and cross-linked to backlog
- [x] Common operator issues documented with resolutions
- [x] Recovery + sibling-pruning procedures captured
- [x] Next steps suggested (retrospective)

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
- Retargeted the maintenance template from the generic service model to the
  real surface: `.pre-fix-*` sibling lifecycle, collision bound, the residual
  coverage gap, and the checkptr follow-up — plus an operator troubleshooting
  guide keyed to the new messages and refusals.
- No automated monitoring/alerting applies; the printed notices and the CI
  gates are the standing guards.

## Lessons Learned
- The maintenance burden of a "non-destructive" default is the **artefact it
  leaves behind**: preserved siblings accumulate with no GC. Documenting the
  prune/recover procedure now (rather than after the first "why is my dir full
  of `.pre-fix` files" question) is the cheap win. A retention subcommand is the
  natural follow-up if it becomes a real annoyance.
