# dcfhfix non-destructive fix-to-new-file - Retrospective
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Baseline Commit**: 4598a81b2f76d1838462d249952b0ef95ecf56b9
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-04

## Executive Summary
- **Duration**: ~1 day across a 9-commit checkpoint trail (a–j), within the
  1–2 day estimate.
- **Scope**: Delivered as planned — `dcfhfix` now preserves the pre-repair index
  at a visible `<index>.pre-fix-<UTC>` sibling by default across all four write
  paths (header edit, entry edit, entry append, entry remove), with a
  `--force`-gated `--edit-in-place` opt-out for the legacy behaviour. No scope
  creep; the only mid-task deviation was a forced Go toolchain bump (security).
- **Outcome**: Success. Every a-task-plan success criterion met; build, full
  test matrix, gosec gate, and `-race` (repo config) all green.

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): 1–2 days, Medium complexity.
- **Actual**: ~1 day. Implementation was small (143 LOC in `promote.go` + four
  one-line rename-site rewrites); the bulk of effort was tests (739 LOC) and
  two unplanned detours (below).
- **Variance**: On/under estimate. The feature surface was smaller than the
  "Medium" guess once requirements reconciled it with the *existing*
  `--backup`/`fixes` stack — the new sibling **complements** rather than replaces
  it, so no rework of the backup machinery was needed.

### Scope Changes
- **Additions**:
  - Go toolchain bump `go1.26.3 → go1.26.4` — forced, not planned: govulncheck
    (a hard-fail gate) flagged GO-2026-5039/5037 in the 1.26.3 stdlib during the
    f-phase commit. Surfaced to the user, who approved the bump.
- **Removals / deferred**:
  - `preserveOriginal` Sync/Close/Lstat-error branches left uncovered (75.8%) —
    deferred for want of a fault-injection seam; documented in g/i.
  - Sibling retention/GC — explicitly out of scope; noted as a future
    enhancement in i-maintenance.
- **Impact**: None on timeline or quality; both deferrals are documented residual
  items, not silent gaps.

### Quality Metrics
- **Test Coverage**: `promote.go` — 4/5 functions 100%; `preserveOriginal` 75.8%
  (defensive branches only). Full per-write-path integration matrix + message/
  quiet matrix + NFR5 failure-ordering, all green.
- **Defect Rate**: 0 escaped defects. One *self-inflicted process* error (the
  `-race`/checkptr false alarm) caught and corrected within the task.
- **Security**: `golangci-lint run ./...` → 0 issues; f-phase
  `cwf-security-reviewer-changeset` → no findings.

## What Went Well
- **Requirements caught the stale premise early.** The a-plan flagged two real
  risks — overlapping safety models (`--backup`/`fixes` already exists) and
  command-surface drift (docs said `scan`, reality is `fixes`). Resolving these
  in b-requirements meant the implementation targeted only the four real write
  paths and slotted *alongside* the backup stack instead of fighting it.
- **One choke-point for the change.** Routing all four write paths through a
  single `promoteRepairedIndex` helper made the behaviour change a four-line
  diff at the call sites plus one new file — easy to review, easy to test, easy
  to roll back (additive, non-destructive on-disk contract).
- **Failure-ordering was designed, not bolted on.** NFR5 (preserve-before-rename,
  clean up the partial sibling on copy failure, never consume the temp on a
  failed preserve) was specified up front and has direct test coverage.

## What Could Be Improved
- **Verify the gate before escalating.** I treated a plain `go test -race`
  checkptr failure as a commit blocker and raised it to the user twice before
  reading `.githooks/pre-commit` and discovering checkptr is *deliberately*
  disabled (`-d=checkptr=0`) for the zero-copy core. The signal was a
  pre-existing, known condition — not a regression from this task. Reading the
  gate config first would have saved two round-trips. Captured in
  [[project_race_checkptr_disabled]].
- **Tooling-hygiene reminders cost cycles.** Two rejected Bash commands (an
  `&& echo "…rc=$?"` suffix and an over-built `echo/cat/wc` redirect) repeated
  mistakes already in memory. Broadened [[feedback_no_echo_exit]] to ban the
  suffix in any form.

## Key Learnings
### Technical Insights
- **A "non-destructive default" is an additive on-disk contract.** Because the
  repaired index still lands at the original path (atomic rename unchanged) and
  the sibling is purely extra, rollback is a binary swap with no state to unwind.
  That property is what made the rollout/maintenance story trivial — worth
  reaching for deliberately in future safety changes.
- **`O_WRONLY|O_CREATE|O_EXCL` + Lstat-classify is the safe sibling-write idiom.**
  EXCL prevents clobbering; classifying an EEXIST occupant (regular → advance the
  `-N` counter, non-regular → hard refuse) prevents writing through a
  symlink/dir. Bounded retry (100) keeps it from spinning.

### Process Learnings
- **Estimation**: "Medium" over-weighted the feature when the real cost was tests
  + reconciliation. Reconciling against existing mechanisms *before* sizing would
  have landed closer.
- **Gate literacy beats assumption.** When a gate behaves unexpectedly, read its
  config before assuming breakage — this is now a memory reflex.

### Risk Mitigation Strategies
- The a-plan's two named risks (overlapping safety models, command-surface drift)
  both materialised and were both pre-empted by the requirements reconciliation —
  evidence the up-front risk pass paid for itself.

## Recommendations
### Process Improvements
- Add "read `.githooks/pre-commit` before declaring a gate failure a blocker" to
  the personal pre-escalation checklist (done via memory).

### Tool and Technique Recommendations
- The single-choke-point pattern (`promoteRepairedIndex`) is a good template for
  any future "change what happens at the write boundary" task in this tool.

### Future Work
- **Re-enable checkptr in the race gate** (Very High, already in BACKLOG) — make
  the zero-copy accessors checkptr-clean so the gate can catch real pointer bugs.
- **`.pre-fix-*` sibling retention/GC** (Low) — optional cleanup subcommand
  mirroring the `fixes` stack, if accumulation becomes a real annoyance.
- **Fault-injection seam for `preserveOriginal`** (Low) — to cover the residual
  Sync/Close/Lstat branches.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to user
**Blockers**: None identified
**Completion Date**: 2026-06-04
**Sign-off**: Matt Keenan / Claude (CWF workflow)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning trail: a-task-plan.md … i-maintenance.md (this task directory).
- Implementation: `cmd/dcfhfix/promote.go` (+ four write-path rewrites in
  `main.go`, `entry_workflow_main.go`, `entry_append_remove.go`); tests in
  `promote_test.go` / `promote_integration_test.go`.
- Checkpoint commits: 2a21d23 (a) … 14594ea (i), squashed at retrospective.
- Backlog item filed: "Re-enable checkptr in the race gate" (Very High).
