# Fault-injection tests for atomic replacement - Plan
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Baseline Commit**: 3326e867d8514bbd66e0b350512914fe96378790
- **Template Version**: 2.1

## Goal
Prove the atomic index-replacement guarantee ("main index is only updated on
complete success") holds under injected I/O failures, and close the uneven
scan edge-case coverage (mid-scan interrupt, concurrent modification), by adding
a minimal test seam plus the failure-path tests it enables.

## Success Criteria
- [ ] A minimal injectable seam exists for the os-level write primitives on the
      replacement path (`os.Rename`, `file.Sync`, and the temp `os.OpenFile`),
      overridable from tests only — no behaviour change on the production path.
- [ ] Fault-injection tests assert the integrity invariant for each failure
      point: a failed `Rename`/`Sync`/temp-write leaves the prior `main.idx`
      and `cache.idx` intact and loadable, and the orphaned temp file is removed
      (covers both `finaliseMainIndex` and `finaliseStatusCache` cleanup paths).
- [ ] Scan edge cases are exercised: a file deleted between scan and hash, and a
      file modified between stat and hash, each complete without corrupting the
      resulting index (no crash, entry either consistently present or absent).
- [ ] Mid-scan interrupt coverage is concrete: either `pkg/shutdown_test.go` is
      un-skipped against the v0.7 pipeline, or an equivalent context-cancellation
      test asserts no partial index is promoted.
- [ ] `go test ./pkg/...` passes (including `-race` per the repo gate); new tests
      fail if the seam is bypassed (verified by a deliberate temporary break).

## Original Estimate
**Effort**: 1.5-2 days
**Complexity**: Medium
**Dependencies**: None blocking. Builds only on existing pipeline (`pipeline_update.go`,
`status.go`, `temp_index_writer.go`); no new third-party deps.

## Major Milestones
1. **Seam**: Introduce the minimal swappable hook(s) for `Rename`/`Sync`/temp-open
   on the replacement path (idiomatic package-level `var` indirection, not a new
   interface unless design shows it is needed). Production path unchanged.
2. **Atomic-replacement faults (#2)**: Tests injecting failure at each write
   point; assert prior indices survive intact and temps are swept.
3. **Scan edge cases (#1)**: Tests for delete-between-scan-and-hash,
   modify-between-stat-and-hash, and a concrete mid-scan interrupt assertion.
4. **Gate**: Full `./pkg/...` suite green under `-race`; seam-bypass sanity check.

## Risk Assessment
### High Priority Risks
- **Risk 1**: Over-abstraction — a full `FileOps` interface threaded through
  constructors would be a large blast radius for test-only needs, violating
  "best part is no part".
  - **Mitigation**: Default to package-level `var osRename = os.Rename`-style
    indirection swapped via a test helper with `t.Cleanup` restore. Escalate to
    an interface only if the design phase finds the var approach insufficient.
- **Risk 2**: Flaky concurrency tests — modify/delete-during-scan races can be
  timing-dependent and intermittently green.
  - **Mitigation**: Drive the race deterministically through the seam (inject the
    fs mutation at a known pipeline stage) rather than relying on wall-clock
    timing; keep wall-clock sleeps out of the assertions.

### Medium Priority Risks
- **Risk 3**: `pkg/shutdown_test.go` is skipped pending pipeline migration; un-skipping
  may reveal it no longer matches the v0.7 channel pipeline.
  - **Mitigation**: Treat un-skip as best-effort; if the old test is structurally
    stale, write a fresh focused cancellation test instead and note the old one
    for separate retirement.
- **Risk 4**: Three os-level write sites exist (main via `pipeline_update.go:189`,
  cache via `status.go:139`, wire-handler via `wire_handler.go:445`). Scope creep
  if all three are seamed.
  - **Mitigation**: Scope to the main+cache daily-operation paths; treat the wire
    handler as out-of-scope (audit-mode, separate cache format) unless trivially shared.

## Dependencies
- Existing pipeline scaffolding: `pkg/pipeline_update.go`, `pkg/status.go`,
  `pkg/temp_index_writer.go`, `pkg/hash_pool.go`, `pkg/scan.go`.
- Existing test patterns: `pkg/hash_pool_test.go` (ctx cancellation),
  `pkg/basic_integration_test.go` (full scan/update flow) as scaffolding to copy.

## Constraints
- Production code path must be byte-for-byte unchanged in behaviour; the seam is
  inert outside tests (no runtime branch, no perf cost on the hot path).
- Tests interacting with index files use temp dirs only (no production `.dcfh`).
- Must pass the repo's `-race` (with `-d=checkptr=0`) pre-commit gate.
- British spelling in prose/comments; match surrounding test idiom.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — ~2 days.
- [ ] **People**: Does this need >2 people? No.
- [x] **Complexity**: 3+ distinct concerns — seam + atomic faults + scan edge cases.
- [ ] **Risk**: High-risk components needing isolation? No — test-only additions.
- [x] **Independence**: Atomic-fault tests and scan-edge tests can be written
      separately once the shared seam exists.

**2 signals triggered.** A split is defensible (23.1 = seam + atomic faults #2,
23.2 = scan edge cases #1), but the work is cohesive test-hardening sharing one
seam, and two CWF cycles add process overhead disproportionate to ~2 days of
test code. **Recommendation: keep as one task with the four milestones above**;
revisit a split only if requirements/design materially grows the seam.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met in ~1 day (vs 1.5-2 day estimate). The minimal
package-level seam (`io_seam.go`) was sufficient — no `FileOps` interface needed,
so the production diff stayed at 37 lines across 5 files. Both triggered
decomposition signals notwithstanding, the task stayed cohesive as one cycle as
recommended. The FR6 mid-scan-cancel partial-promotion bug was fixed in scope
(~2 lines) and proven by TC-9. All 9 fault tests pass; teeth T-A/T-B/T-C confirm
they bite; `-race`/lint/govulncheck green. See j-retrospective.md for full
variance analysis.

## Lessons Learned
Both high-priority risks (over-abstraction, flaky concurrency) were neutralised
exactly by their planned mitigations — the var-indirection held, and driving
faults through a pipeline-stage hook kept the concurrency tests deterministic.
The unplanned cost was test-oracle assumptions (three mid-exec corrections, D1-D3)
that were discoverable from existing source during the testing-plan phase.
