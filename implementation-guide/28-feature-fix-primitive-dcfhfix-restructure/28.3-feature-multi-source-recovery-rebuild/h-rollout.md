# Multi-source recovery rebuild - Rollout
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Land the multi-source `recovery-rebuild` Fix op onto `main`. As with 28.1/28.2,
"rollout" here is a library/CLI source landing (ff-only branch merge → trunk →
next tagged build), not a service deployment — the template's phased
%/monitoring/alerting sections are mapped accordingly.

## Deployment Strategy
### Release Type
- **Strategy**: Trunk landing — ff-only merge of the task branch onto `main`,
  picked up by the next `goreleaser` build (dcfh/dcfhfind/dcfhfix). No runtime
  service, no staged user cohorts.
- **Rationale**: matches the project branch-landing model (main is the trunk,
  linear history, never `--no-ff`). The change is one new library op routed
  through the existing `RunFix` batch path plus its merge core; no CLI surface
  change, no on-disk format change.
- **Rollback Plan**: `git revert` the squash commit on `main` (single landed
  unit); no data migration, no format change to undo. The op is library-only
  (`writeRoot != ""` asserted) so reverting removes it cleanly.

### Pre-Deployment Checklist
- [x] Code review completed — CWF changeset security review run on both exec
  phases (implementation-exec + testing-exec); both **no findings** (full
  288-line production surface captured after the untracked-file fix). One
  documented residual: `verifyRecoverySnapshot` is presence/size, not
  byte-integrity (safe-here per LD6, scoped for future audit).
- [x] All tests passing — 16/16 recovery TCs pass; full `go test ./pkg/...
  ./cmd/...` green; pre-commit `-race` (`-d=checkptr=0`) green.
- [x] Security scan completed — `golangci-lint run ./...` (gosec floor) 0 issues;
  `govulncheck` 0 applicable. No new gosec suppressions (reused confined paths).
- [x] Performance validated — recovery rebuild reuses the single-writer path; no
  new index passes beyond the source merge fold (informational, NFR1).
- [x] Documentation updated — deviations recorded in f-/g-; no user-doc/CLI help
  change required (no surface change; the op is reached via the library Fix API).
- [N/A] Monitoring/alerting configured — no runtime service.
- [x] Rollback plan ready — single-commit `git revert` (above).

## Rollout Plan
### Phase 1: Limited Release
- **Scope**: the task branch itself — exercised via the 16-case recovery suite
  (merge unit TC-1…TC-8, integration-through-`Repo.Fix` TC-9…TC-15,
  fault-injection atomicity TC-16) in g-testing-exec.md.
- **Duration**: complete (pre-landing gates).
- **Success Metrics**: suite + lint + `-race` green; rebuilt `main.idx` loads
  clean via the production loader; no partial index under sync fault.

### Phase 2: Gradual Rollout
- **Scope**: land on `main`; the `recovery-rebuild` op becomes available to
  library consumers of `Repo.Fix` on the next build.
- **Duration**: n/a (atomic landing).
- **Success Metrics**: `cwf-manage validate` OK post-landing; `main` builds clean.

### Phase 3: Full Release
- **Scope**: next tagged `goreleaser` build ships the recovery-rebuild op
  alongside the 28.2 Fix primitive.
- **Monitoring**: ongoing via the standing test suite + lint gate on `main`.

## Monitoring
### Key Metrics
- **Correctness**: the data-integrity gates hold in CI — empty-merge guard
  (TC-14) and dry-run (TC-10) write nothing; the snapshot-readback gate (TC-13)
  and fault-injection atomicity (TC-16) ensure the rebuilt index is
  valid-or-absent, never a partial.
- **Errors**: the test suite is the regression signal; no telemetry surface.
- **Adoption**: `recovery-rebuild` completes parent Task 28 — the v0.7 recovery
  write path now runs through the single-writer atomic `Repo.Fix` batch.

### Alerting
- CI suite failure on `main` is the alert channel (no runtime alerting).

## Rollback Plan
### Triggers
- Post-landing suite/lint failure on `main`.
- A read-source confinement escape, snapshot-gate bypass, or single-writer
  invariant violation found in review.
- Any partial/corrupt `main.idx` produced by a recovery rebuild in the field.

### Procedure
1. **Immediate**: identify the squash commit on `main`.
2. **Rollback**: `git revert <squash-sha>` (no format/data migration to unwind;
   the op is additive and library-only).
3. **Communication**: n/a (single-maintainer trunk).
4. **Analysis**: root-cause in a follow-up bugfix task via the CWF workflow.

## Success Criteria
- [x] Branch lands ff-only on `main`; linear history preserved.
- [x] `main` builds clean; `cwf-manage validate` OK.
- [x] No CLI surface regression (library-only op; no help/flag/exit-code change).
- [x] No rollback required.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance 28.3
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout strategy mapped to the trunk-landing model: ff-only merge → next
`goreleaser` build. All pre-landing gates green; both exec-phase security reviews
returned no findings (unlike 28.2 the changeset stayed under the auto-cap, so no
belt-and-braces split review is owed). Rollback is a single-commit `git revert`.

## Lessons Learned
A data-destructive op rolls out the same way as the rest of the library once its
safety is bounded by the same trunk gates: the empty-guard + snapshot-readback +
atomic single-writer make the rebuild valid-or-absent, so the SaaS phased model
collapses to "land + next build" with `git revert` as the only rollback lever.
