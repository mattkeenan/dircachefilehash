# Harden env-fragile dcfhfind and dcfhfix CI tests - Retrospective
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-11

## Executive Summary
- **Duration**: <0.5 day (estimated: <0.5 day, variance: ~0%)
- **Scope**: Unchanged from plan — two test-only fixes; no production code touched.
- **Outcome**: Success. Both env-fragile tests are deterministic; full suite green; the
  failures task 19's repaired CI surfaced will no longer occur on a clean checkout.

## Variance Analysis
### Time and Effort
- **Estimated**: Implementation <0.5 day; requirements/design skipped (chore).
- **Actual**: As estimated. The work was two localised edits guided by direct verification.
- **Variance**: None material.

### Scope Changes
- **Additions**: One beyond the strict failing case — converted *all four* `t.Fatal`
  binary-missing sites in `cmd/dcfhfind/integration_test.go` to `t.Skip`, not just the one
  CI hit (`TestPerformanceWarning`). Rationale: the other three shared the same latent bug
  (guarded only by an earlier test-repo skip); testing-exec confirmed all four now skip
  cleanly when the binary is absent.
- **Removals**: None.
- **Impact**: Slightly wider edit, materially more robust; no timeline cost.

### Quality Metrics
- **Test Coverage**: No production-code delta. Diff confined to two `_test.go` files.
- **Defect Rate**: 0 new defects; the two pre-existing test defects are resolved.
- **Gates**: golangci-lint 0 issues, govulncheck 0 affecting, `go test -race` green, two
  security-review passes (`no findings`).

## What Went Well
- Root cause was pinned precisely before editing (ambient `.dcfh/` discovery; `t.Fatal`
  vs sibling `t.Skip`), and every load-bearing claim was verified against source.
- Followed an existing in-repo precedent (`promote_integration_test.go:157-163`) for the
  hermetic temp `.dcfh/` rather than inventing a variant — a plan-review finding applied.
- Verified the clean-checkout condition directly by moving the binary aside, rather than
  trusting that the change "should" skip.

## What Could Be Improved
- I began editing before starting the CWF workflow and had to revert. Recorded as a memory
  (`feedback_use_cwf_for_changes`) so changes in this repo route through CWF from the outset.

## Key Learnings
### Technical Insights
- Tests that read ambient repo state (a gitignored `.dcfh/`, a built binary in the package
  dir) pass on a developer machine and fail only on a clean checkout — exactly the gap a
  stale CI hides until repaired. Hermetic `t.TempDir()` roots and `t.Skip` on missing build
  artifacts remove the ambient coupling.
- `getBackupDir`'s parent-walk is hermetic only if the index path sits directly in the temp
  dir (so the walk terminates on iteration one and never escapes to system-temp ancestors).

### Process Learnings
- For test-hardening chores the "code under test" is the suite itself; the most convincing
  validation is reproducing the failing environment (binary removed) and observing SKIP.

## Recommendations
### Future Work
- None required. If more integration tests are added to `cmd/dcfhfind`, keep the
  skip-on-missing-binary guard consistent across them.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to main
**Blockers**: None identified
**Completion Date**: 2026-06-11
**Sign-off**: Matt Keenan / Claude (CWF)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Links to planning documents and artefacts
- Links to implementation PRs and commits
- Links to test results and quality reports
- Links to deployment and monitoring dashboards
