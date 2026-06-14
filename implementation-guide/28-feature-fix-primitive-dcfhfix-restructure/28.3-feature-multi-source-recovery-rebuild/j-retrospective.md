# Multi-source recovery rebuild - Retrospective
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-13

## Executive Summary
- **Duration**: ~1 day (estimated: 2–3 days; variance ≈ −60%). All 9 checkpoint
  commits landed 2026-06-13 (`04baef59` … `706f4ca4`), baseline `61efe765`.
- **Scope**: Delivered as planned — a multi-source `recovery-rebuild` Fix op that
  rebuilds `main.idx` from a precedence-ordered merge of surviving sources
  (timestamped caches newest→oldest > cache.idx > main.idx), through the
  single-writer atomic path, gated by a snapshot-readback precondition and
  fault-injection-proven atomicity. Completes parent Task 28 (FR8/AC5/NFR5).
- **Outcome**: Success. All 5 success criteria met; 16/16 TCs pass; full suite +
  `golangci-lint` (0) + `govulncheck` (0) + pre-commit `-race` green; both
  exec-phase changeset security reviews **no findings**.

## Variance Analysis
### Time and Effort
- **Estimated**: 2–3 days, High complexity (data-destructive write path).
- **Actual**: single day, three milestones:
  - Plan phases (a–e): same-day, leveraged the 28-parent decomposition.
  - Implementation (f, M1–M3): `53b8f70a` (merge core + RunFix branch + faults).
  - Testing (g): `f4762417` (16 TCs mapped, coverage).
  - Rollout/maintenance/retrospective (h/i/j): docs.
- **Variance**: ≈ −60%. The "High complexity" label tracked *consequence*
  (overwriting the user's only index), not *residual* effort: 28.2 had already
  landed `RunFix`/`confineWriteDest`/`writeRepairedIndex`, and the reuse surface
  (`collectForEdit`, `createPreRecoverySnapshot`, the Task-23 fault seams) meant
  the write path was assembly of proven parts. Net new: 288 production lines
  (`pkg/fix_recovery.go` 265 + `pkg/fix_run.go` 23) + 533 test lines.

### Scope Changes
- **Additions**:
  - `mergeSourcesIntoEntries` returns `contributing []string` (sources that fed
    ≥1 entry) — needed so the snapshot readback verifies only contributing
    sources, not all ordered candidates (resolves the AC1↔AC4 tension).
  - Concrete checksum policy (refines LD5): first contributing source sets the
    output type; a later disagreeing source is skipped with its entries counted
    as discards (no abort, no re-hash). Not spelled out in design; made concrete
    at exec.
- **Removals / deferrals**:
  - `mergeSourcesIntoEntries` `err` return dropped (unparam — a failing source is
    skipped tolerantly, never errors). The d-plan signature carried it.
  - The optional under-floor guard (FR5 second clause) stayed descoped per design
    LD7 — not deferred here.
- **Impact**: net simplification (no parallel reader, no error plumbing); no
  timeline cost.

### Quality Metrics
- **Test Coverage**: 16/16 TCs pass (merge unit TC-1…TC-8, integration-through-
  `Repo.Fix` TC-9…TC-15, fault-injection atomicity TC-16). Per-function:
  `verifyRecoverySnapshot` 100%, `orderedSourcePaths` 93.8%,
  `mergeSourcesIntoEntries` 93.3%, `runRecoveryRebuild` 86.7%. Uncovered lines
  are defensive I/O-error returns.
- **Defect Rate**: zero product defects. One fixture-modelling correction
  (destroyed-main fixtures must `os.Remove` the empty `main.idx` that
  `CreateMetaStore` seeds — 8 integration tests initially failed) and one test
  restructure (TC-16 fault scope) — both test-side, no production change.
- **Performance**: each source folded once via `collectForEdit`, then one write
  via the single-writer path — no extra index passes (NFR1).

## What Went Well
- **`collectForEdit` with an empty pathSet was a perfect fit**: it keeps every
  entry unchanged *including deleted tombstones* (needed for cross-source
  suppression) **and** is truncation-tolerant (readable prefix kept, truncated
  tail discarded). Reusing it meant the merge needed no new reader — "the best
  part is no part."
- **Changeset stayed under the security-review auto-cap** (288 production lines
  vs 28.2's ~1700), so both f and g reviews ran clean to `no findings` — the
  exact split the 28.2 retrospective recommended for from-scratch primitives
  paid off as soon as the concern was scoped to one file.
- **Fault-injection seam isolation**: the snapshot copy uses `os.WriteFile`
  (not the `fsSync` seam), so `withSyncFault` was scoped cleanly to
  `writeRepairedIndex` — TC-16 proves no partial `main.idx` without collateral.
- **The high risk (data-destructive write) was contained exactly as planned**:
  empty-guard → snapshot + fatal readback → atomic single-writer, each
  test-covered; it never became a blocker.

## What Could Be Improved
- **Net-new file invisible to the security changeset**: the first f-exec review
  returned `findings` only because `pkg/fix_recovery.go` was untracked, so
  `git diff` captured just the 23-line `RunFix` branch. Staging the file and
  re-running gave the real 288-line surface and a clean verdict — but this is the
  second time the untracked-file gap has bitten the changeset helper. A
  pre-review "stage net-new files" step would prevent it.
- **`CreateMetaStore` seeding an empty `main.idx`** cost test-debugging time
  (8 integration tests failed before the destroyed-main model was corrected).
  Non-obvious behaviour now documented in i-maintenance.md.
- **Cosmetic stderr wart**: `PromoteRepairedIndex` prints a hardcoded "original
  is NOT preserved" warning even though the pre-recovery snapshot *did* preserve
  it. Harmless but misleading on the recovery path; a message refinement is owed.

## Key Learnings
### Technical Insights
- A reader built for one purpose (in-place edit collection) can be a dual-purpose
  primitive: `collectForEdit`'s empty-pathSet behaviour (keep-all +
  truncation-tolerant) is exactly a merge source-reader. Check existing readers
  before writing a recovery-specific one.
- **Verify *contributing*, not *ordered***: gating the snapshot readback on the
  sources that actually fed entries (not every candidate) resolves the tension
  between "rebuild from whatever survives" (AC1) and "snapshot must exist" (AC4)
  — a present-but-zeroed `main.idx` must not force a false abort.
- The `writeRoot != ""` assertion keeps the destructive op **library-only** —
  structurally unreachable from the unconfined CLI explicit-subject exemption.
  The trust boundary lives at the call site, mirroring 28.2's model.

### Process Learnings
- Estimation: a "High complexity" label that tracks *consequence* (blast radius)
  over-predicts effort once the predecessor task has de-risked the mechanics.
  Distinguish inherent from residual complexity — same lesson as 28.2, confirmed.
- Scoping a from-scratch surface to a single new file kept it under the
  security-review auto-cap and gave two clean automated verdicts — the practical
  fix for the 28.2 "auto-cap is a poor fit for large changesets" pain.

### Risk Mitigation Strategies
- The pre-identified high risk (data-destructive write) was isolated by its own
  ACs and a layered guard (empty → snapshot-readback → atomic rename), each with
  a dedicated test (TC-13/TC-14/TC-16). Landing the fault-injection gate *before*
  trusting the path was the right sequence.
- The presence-only snapshot-readback residual (TOCTOU, byte-integrity not
  checked) was surfaced and bounded in both reviews rather than silently
  accepted — recorded with the explicit "audit if reachable across a privilege
  boundary" condition (LD6, i-maintenance.md).

## Recommendations
### Process Improvements
- **Stage net-new files before `security-review-changeset`**: add an explicit
  "git add untracked production files" step to the exec-phase security flow so
  the changeset helper sees the full surface on the first run. (Recurring — hit
  28.2-context and 28.3-f.)

### Tool and Technique Recommendations
- Keep favouring single-file, single-concern surfaces for new ops — they stay
  under the review auto-cap and map cleanly onto the `Repo.Fix` batch branch.

### Future Work
- **Cosmetic "NOT preserved" stderr message on the recovery path** (bugfix,
  Low): `PromoteRepairedIndex` should not claim the original was discarded when a
  pre-recovery snapshot preserved it. Documented in i-maintenance.md.
- **Parent Task 28 retrospective**: 28.1/28.2/28.3 all landed — the parent
  (`/cwf-retrospective 28`) can now close out the fix-primitive + dcfhfix
  restructure as a whole.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-13
**Sign-off**: Matt Keenan / Claude Opus 4.8

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md … e-testing-plan.md (all Finished).
- Implementation: f-implementation-exec.md (`53b8f70a`; merge core + RunFix
  branch + fault-injection); deviations + security review (no findings) recorded.
- Testing: g-testing-exec.md (`f4762417`; 16 TCs, coverage, no findings).
- Rollout/maintenance: h-rollout.md (`c8d4cf93`), i-maintenance.md (`706f4ca4`).
- Production: `pkg/fix_recovery.go`, `pkg/fix_run.go` (288 lines); tests
  `pkg/fix_recovery_test.go`, `pkg/fix_recovery_run_test.go` (533 lines).
- Baseline commit: `61efe765`.
