# Fault-injection tests for atomic replacement - Rollout
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Land task 23 on `main`: fault-injection tests for atomic index replacement
(TC-1..TC-9) plus the FR6 cancellation guard that stops a mid-scan cancel from
promoting a partial index.

## Deployment Strategy
### Release Type
- **Strategy**: Single linear fast-forward merge to `main` (no phased/canary
  rollout). This is a developer-facing library/CLI repo, not a running service:
  "deploy" means the change becomes part of the trunk and ships in the next
  tagged build. There is no user cohort to ramp, no live traffic to canary.
- **Rationale**: The production surface is 37 lines across 5 files — four inert
  `os`-primitive seam swaps (`fsRename`/`fsOpenFile`/`fsSync` default to the real
  calls), one nil-guarded `hashPreReadHook` (nil in production), and the FR6
  `ctx.Err()` guard. The seam vars have no production writer, so behaviour for
  any non-test caller is byte-identical to before; the only behavioural change is
  the FR6 fix, which strictly *narrows* when an index is promoted (cancelled runs
  now correctly abort instead of promoting a partial). The remaining ~700 lines
  are `_test.go` and process markdown, which never ship in a binary.
- **Rollback Plan**: `git revert` the squash commit. The change is self-contained
  (new `io_seam.go`, three new `_test.go` files, five small call-site edits) with
  no migration, no on-disk format change, and no config/flag surface, so a revert
  is clean and leaves no residue.

### Pre-Deployment Checklist
- [x] Code review completed and approved — CWF changeset security review on both
      exec phases returned **no findings**; FR6 guard reviewed as integrity
      hardening.
- [x] All tests passing (unit, integration, system) — `go test ./pkg/...` green;
      `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...` green; new tests looped
      `-count=20` under `-race` with no flakes.
- [x] Security scan completed with no critical issues — `golangci-lint run ./...`
      (gosec inside) **0 issues**; `govulncheck` clean; G302 nolint rationale
      preserved verbatim across the `fsOpenFile` rename.
- [x] Performance testing validated against requirements — by inspection (NFR1):
      one function-pointer indirection per primitive, one nil-check per hashed
      entry, one `ctx.Err()` per update; none on the hot byte path. No benchmark
      warranted.
- [x] Documentation updated — process docs (f/g exec) record actual results and
      deviations D1-D3; no user-facing docs affected (test-only + internal guard).
- [x] Monitoring and alerting configured — N/A for a library merge; the standing
      CI gate (test + race + lint + govulncheck) is the monitor (see below).
- [x] Rollback plan tested and ready — revert is a single `git revert`; teeth
      checks T-A/T-B/T-C already proved each guarded path fails loudly if removed,
      so a regression would surface in CI, not silently.

## Rollout Plan
### Phase 1: Limited Release
- **Scope**: Land the task branch on `main` (the trunk). No partial-cohort
  release exists for this repo.
- **Duration**: Immediate; validated by the pre-merge gate.
- **Success Metrics**: Pre-commit + CI gate green (build, `go test`, `-race`,
  `golangci-lint`, `govulncheck`).

### Phase 2: Gradual Rollout
- **Scope**: N/A — no staged user rollout for a developer library/CLI.
- **Duration**: N/A.
- **Success Metrics**: N/A.

### Phase 3: Full Release
- **Scope**: Change is on `main` and included in the next tagged build that
  downstream consumers pick up.
- **Monitoring**: Ongoing CI on `main`.

## Monitoring
### Key Metrics
- **Performance**: No new runtime hot-path cost; no metric to track beyond the
  existing test-suite wall-clock.
- **Errors**: CI test/race/lint/govulncheck status on `main`. The new fault tests
  themselves are the regression sentinels for the atomic-replacement and FR6
  invariants going forward.
- **Business**: N/A.

### Alerting
- A red CI run on `main` is the alert channel; failure of any TC-1..TC-9 case
  flags a regression in the atomic-replacement or cancellation-integrity
  guarantees.

## Rollback Plan
### Triggers
- Any of TC-1..TC-9 becomes flaky or fails on `main`.
- The FR6 guard is shown to over-abort (a legitimate, non-cancelled run fails to
  promote) — not observed, but the named trigger.
- A security regression surfaced by gosec/govulncheck attributable to the seam.

### Procedure
1. **Immediate**: Identify the failing case from CI output (each assertion names
   the invariant it guards).
2. **Rollback**: `git revert` the task 23 squash commit on `main`.
3. **Communication**: Note the revert reason in the commit; no external users to
   notify.
4. **Analysis**: Re-open via `/cwf-implementation-exec` to fix and re-run the
   teeth checks before re-landing.

## Success Criteria
- [x] Deployment completed without issues — both exec checkpoints committed; gates
      green.
- [x] All monitoring metrics within acceptable ranges — CI gate green.
- [x] User feedback positive or neutral — N/A (internal, no user-facing change).
- [x] Business objectives met — atomic-replacement + scan-edge + FR6 paths now
      have proving fault-injection coverage.
- [x] No rollbacks required.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout is a trunk merge, not a service deployment. All pre-deployment checks
pass: tests + `-race` + `golangci-lint` + `govulncheck` green, both CWF security
reviews "no findings", FR6 guard confirmed load-bearing by teeth check T-A.
Production footprint is 37 lines (inert seams + FR6 guard); the rest is tests and
process docs. Rollback path is a single `git revert` with no migration to unwind.

## Lessons Learned
The standard rollout template is service-deployment-shaped (canary %, user
cohorts, business metrics); for a library/CLI test-and-guard change most phases
collapse to "merge to trunk, CI is the monitor, `git revert` is the rollback".
Recording that mapping explicitly is more useful than forcing canary semantics
onto a change that has no traffic to ramp.
