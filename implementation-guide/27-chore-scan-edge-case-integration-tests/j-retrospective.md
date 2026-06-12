# Scan edge-case integration tests - Retrospective
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-12

## Executive Summary
- **Duration**: ~0.5 day actual (estimated ~0.5 day — on estimate).
- **Scope**: Delivered exactly the three in-scope boundary-race tests
  (grow / shrink / file→directory) at the discovery→hash seam. The walk-phase
  lstat-ENOENT case was deferred at plan time (approved) and is carried forward
  as a backlog item.
- **Outcome**: Success. Three deterministic integration tests added, pinning
  dcfh's existing tolerant behaviour. No production code changed; suite green,
  race-clean, lint-clean, security review clean.

## Variance Analysis
### Time and Effort
- **Estimated**: Planning ~0.1d, Implementation ~0.2d, Testing ~0.2d (chore —
  no requirements/design/rollout phases).
- **Actual**: As estimated. No phase overran.
- **Variance**: ~0%. The chore was correctly sized once re-scoped against
  Task 23's existing coverage.

### Scope Changes
- **Additions**: None beyond plan.
- **Removals**: Walk-phase lstat-ENOENT race deferred (no seam exists; testing
  it would need new production code for a narrow, arguably-correct silent-skip).
  Approved at plan time; tracked as a new backlog item (below).
- **Impact**: None on timeline or quality — the three in-scope cases stand
  independently.

### Quality Metrics
- **Test Coverage**: 3/3 new tests pass. Targeted branches confirmed exercised:
  `hash_pool.go worker` swallow-branch (via TC-12 `EISDIR`), `hashEntry`/
  `HashOne` success path (via TC-10/11 over grown/shrunk files).
- **Defect Rate**: 0 defects found; behaviour under test was already correct
  (these tests pin existing tolerance, they did not fix a bug).
- **Performance**: N/A — three sub-second tests.

## What Went Well
- Re-scoping against Task 23 early (via an Explore sweep) avoided duplicating
  TC-7/8/9 and atomic_index_test.go coverage — the chore stayed narrow.
- Reusing the `hashPreReadHook` seam and the TC-7/8 skeletons meant zero new
  test scaffolding ("the best part is no part").
- Coherence-only oracle (success + clean load + per-case hash state) kept the
  grow/shrink tests robust rather than asserting brittle exact-size semantics.
- Both exec-phase security reviews returned clean with no manual intervention.

## What Could Be Improved
- The `unparam` linter fired on the reused `freshFind` helper only after the
  third+ all-`"z.txt"` call site tipped its threshold. The full-repo audit
  caught it; the `--new` staged gate did not. Worth remembering that adding
  call sites to a shared helper can surface a latent full-audit finding that
  the staged gate masks.

## Key Learnings
### Technical Insights
- On Linux, `os.Open` on a directory succeeds; the `EISDIR` failure surfaces on
  the subsequent `file.Read` (`pkg/hash.go:204-216`). `entry.Mode()` returns the
  scan-time stored mode, so a file→dir swap keeps the entry on the non-symlink
  `HashOne` branch — the read then fails and is swallowed.
- The hash-error swallow (`hash_pool.go:87-94`) is the single mechanism behind
  both delete-tolerance (TC-7) and file→dir-tolerance (TC-12).

### Process Learnings
- `unparam` is whole-program: a helper's "always same arg" finding depends on
  the aggregate of all call sites, so it can appear in a later task that merely
  adds callers. Measuring through full `golangci-lint run ./...` (not just the
  `--new` gate) is what caught it.

### Risk Mitigation Strategies
- The plan's coherence-only oracle (flagged as Risk 1) prevented the predicted
  brittleness — the grow/shrink tests assert hash presence, not exact size.

## Recommendations
### Process Improvements
- When adding call sites to a shared test helper, run the full lint audit
  (not only the staged `--new` gate) before committing.

### Tool and Technique Recommendations
- Continue the seam-reuse pattern for boundary-race tests; the
  `hashPreReadHook` design generalises cleanly to new pre-read mutations.

### Future Work
- **Walk-phase lstat-ENOENT race test** — deferred from this task. Needs a small
  production walk-phase seam (mirroring `hashPreReadHook`) to test the
  readdir→lstat mid-walk disappearance deterministically. Added to BACKLOG.md.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to main
**Blockers**: None identified
**Completion Date**: 2026-06-12
**Sign-off**: Matt Keenan (claude@mattkeenan.net)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md, g-testing-exec.md
- Code: pkg/scan_edge_cases_test.go (TC-10/11/12)
- Checkpoint commits: 4ad3402e (f), f1d8cca4 (g), plus a/d/e plan commits
