# Fix primitive + dcfhfix restructure - Plan
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Baseline Commit**: c385cb886760e14e6a24d0892d9d1c01e38061a1
- **Template Version**: 2.1

## Goal
Complete the `Repo` library primitive set by adding the symmetric `Fix`
primitive (the fsck-shaped counterpart to the already-landed `Filter`),
moving the bounds-checked repair helpers out of `cmd/dcfhfix` into `pkg/`,
and re-landing the v0.7 recovery write path as a multi-source `Fix` batch.

## Success Criteria
- [ ] `Repo.Fix(ctx, FixRequest) (*FixResult, error)` exists on the interface and `localRepo.Fix` implements it, with `FixRequest{IndexSelector, Commands, DryRun, Backup}` / `FixResult{RepairsApplied, EntriesDiscarded, BackupID}` encoding today's interactive subcommands as batches.
- [ ] The fsck helpers (`ValidatedEntry`, `processEntriesWithWorkflow`, backup stack) live in `pkg/` and are driven by `localRepo.Fix`; `cmd/dcfhfix` becomes a thin CLI→`FixRequest` translator with no behaviour change to its subcommands.
- [ ] Recovery rebuild of `main.idx` from any readable combination of `main.idx` / `cache.idx` / timestamped-cache files works via a `Fix` batch with a multi-source `IndexSelector` (closes the v0.7 recovery write-path gap left when the orchestration shell was deleted).
- [ ] Both fsck operating modes work: per-entry interactive (legacy default) and auto-fix (apply all safe repairs in one shot), writing to a new index file (no in-place mutation).
- [ ] Existing dcfhfix tests (`main_test.go`, `options_test.go`) and recovery tests (`recovery_test.go`) still pass; new coverage exists for the `Fix` primitive and the recovery write path.

## Original Estimate
**Effort**: ~1.5–2 weeks (decomposition recommended — see below)
**Complexity**: High
**Dependencies**: None blocking. `pkg/format.SafeEntry` (bounds-checked accessor) already moved (Task 3.1). Phase 2 (audit mode) does not depend on this.

## Major Milestones
1. **Helpers in pkg/**: `ValidatedEntry`, `processEntriesWithWorkflow`, and the backup stack relocated from `cmd/dcfhfix` to `pkg/` behind a clean surface, with dcfhfix still green (pure refactor, no behaviour change).
2. **Fix primitive**: `FixRequest`/`FixResult` defined, `Repo.Fix` added, `localRepo.Fix` delegating to the moved helpers; dcfhfix subcommands re-expressed as `FixRequest` batches; both interactive and auto-fix modes wired.
3. **Recovery write path**: multi-source `IndexSelector` rebuild of `main.idx` landed as a `Fix` batch, reusing the surviving validation/processor and snapshot helpers in `pkg/recovery.go`.

## Risk Assessment
### High Priority Risks
- **Data-integrity regression in recovery write path**: rebuilding `main.idx` from partial/corrupt sources is the highest-consequence code in the repo; a bug can destroy the user's only index.
  - **Mitigation**: write to a new index + atomic rename only (never in-place); reuse the existing `createPreRecoverySnapshot` before any write; gate behind fault-injection tests (the Task 23 atomic-replacement harness is a model); keep `rm -rf .dcfh && dcfh init . && dcfh update` as the documented fallback.
- **dcfhfix behaviour regression during cmd→pkg migration**: the fsck helpers assume data may be corrupt and must make forward progress; relocating them risks subtle changes to bounds-checking / forward-progress semantics.
  - **Mitigation**: move first as a pure refactor with dcfhfix tests as the regression gate; only then re-route through `Repo.Fix`; no signature/behaviour change in the same step as the move.

### Medium Priority Risks
- **API shape churn**: `FixRequest{Commands}` is a new batch workflow distinct from the Diff/Apply/Filter path; getting the command encoding wrong forces rework of both the primitive and the CLI translator.
  - **Mitigation**: dedicate the design phase to the batch encoding before any code; enumerate every current interactive subcommand and map it to a command variant first.
- **Scope creep into Phase-2 / forensic features**: dcfhfix is forensic tooling; easy to gold-plate.
  - **Mitigation**: hold scope to re-expressing today's subcommands + the recovery rebuild; defer the `.pre-fix-*` GC and fault-injection-seam backlog items (already separate Low entries).

## Dependencies
- `pkg/format.SafeEntry` — already in place (no work needed).
- Surviving `pkg/recovery.go` validation/processor + snapshot helpers — reused, not rewritten.
- No external or team dependencies.

## Constraints
- Single entry writing path: `TempIndexWriter` remains the only writer of binaryEntries to disk; auto-fix writes a new index via vectorio → atomic rename (no in-place mutation).
- Main/Cache read-only mmap, Temp pure-vectorio file-type separation must be preserved.
- British spelling in prose; gosec floor + CWF changeset review both apply.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: Will this take >1 week? Yes — estimate is ~1.5–2 weeks across three milestones.
- [ ] **People**: Does this need >2 people? No — solo.
- [x] **Complexity**: Does this involve 3+ distinct concerns? Yes — (1) cmd→pkg helper migration, (2) Fix primitive API + CLI translation, (3) recovery write path.
- [x] **Risk**: Are there high-risk components that need isolation? Yes — the recovery write path is data-destructive-on-bug and warrants its own isolated task.
- [x] **Independence**: Can parts be worked on separately? Yes — the migration is a self-contained prerequisite; the Fix primitive builds on it; recovery consumes the primitive.

**Result: 4 of 5 signals triggered → decomposition strongly recommended.**

Proposed subtasks:
- **28.1 (chore/refactor)**: Move `ValidatedEntry`, `processEntriesWithWorkflow`, and the backup stack from `cmd/dcfhfix` into `pkg/`. Pure refactor, dcfhfix tests as the gate. Prerequisite for the rest.
- **28.2 (feature)**: Define `FixRequest`/`FixResult`, add `Repo.Fix`, implement `localRepo.Fix` over the moved helpers, re-express dcfhfix subcommands as batches, wire interactive + auto-fix modes.
- **28.3 (feature)**: Land the v0.7 recovery write path as a multi-source `IndexSelector` `Fix` batch, with fault-injection coverage.

## Status
**Status**: Finished
**Next Action**: /cwf-new-subtask (create 28.1) — decomposition recommended
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Decomposition (4/5 signals) was the right call: 28.1/28.2/28.3 mapped 1:1 to the
three milestones and each shipped under its own estimate. All five success
criteria met on the assembled branch; ~3 days actual vs ~1.5–2 week estimate.

## Lessons Learned
A High-complexity, 3-concern, data-destructive-on-bug task is exactly the profile
the decomposition signals are tuned for — the up-front split, not heroics, drove
the −75% time variance and kept every changeset under the security-review cap.
