# Scan edge-case integration tests - Plan
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Baseline Commit**: a48c35521d8bd5148505bce3bf2e63b0e0d897d9
- **Template Version**: 2.1

## Goal
Close the residual scan-time concurrent-modification coverage gaps that Task 23
left open, by asserting dcfh's existing tolerant behaviour at the
discovery→hash boundary (size TOCTOU and file-type replacement) using the
`hashPreReadHook` seam already shipped in Task 23.

## Context (what Task 23 already covers — do NOT re-test)
The original High-priority backlog item predates Task 23, which already landed:
- `pkg/scan_edge_cases_test.go`: delete-before-hash (TC-7), modify-before-hash
  (TC-8), mid-scan context cancel / no partial promotion (TC-9).
- `pkg/atomic_index_test.go`: write-path faults (rename/open/sync) on both
  main.idx and cache.idx — the "partial write" half of the backlog item.

This task is the narrow remainder: the two **handled-but-untested** races at the
stat→hash boundary, plus an optional walk-phase race (see Decisions).

## Success Criteria
- [ ] A test asserts a file **growing** between stat and hash completes with
      success, a clean-loading index, and a coherent (present, parseable) entry.
- [ ] A test asserts a file **shrinking** between stat and hash completes with
      the same coherence guarantees.
- [ ] A test asserts a regular file **replaced by a directory** before hash is
      tolerated non-fatally (success exit, index loads clean, entry present with
      empty hash — mirroring TC-7's read-failure tolerance).
- [ ] All new tests are deterministic (no sleeps/wall-clock races) and pass
      under the project's `-race` gate.
- [ ] `go test ./pkg/...` is green; no production code changes are required for
      the in-scope criteria above (seam reuse only).

## Out of Scope (explicitly excluded, with rationale)
- **Cross-process concurrent writers** (two `dcfh update` runs): this is an
  unhandled *feature* gap (no flock/O_EXCL), not a missing test for existing
  behaviour. Belongs in a feature task, not a test chore.
- **Stale/partial temp-file cleanup at startup**: already tracked as its own
  Low-priority backlog item ("Clean up stale scan temp files at startup"). Not
  duplicated here.

## Original Estimate
**Effort**: ~0.5 day
**Complexity**: Low
**Dependencies**: Task 23's `hashPreReadHook` seam (`pkg/fault_inject_test.go`)
and helpers in `pkg/scan_edge_cases_test.go` (`seedMainRepo`, `freshFind`,
`assertLoadsClean`, `withHashPreReadHook`). All present on the baseline.

## Major Milestones
1. **Requirements**: Pin the per-case acceptance oracle (success exit + clean
   load + entry-coherence wording) and rule on the optional walk-phase seam.
2. **Implementation**: Add the grow / shrink / file→directory tests to
   `pkg/scan_edge_cases_test.go`, reusing the existing seam and helpers.
3. **Verification**: Suite green under `-race`; coverage delta confirmed on the
   stat→hash and (if taken) walk-phase branches.

## Risk Assessment
### Medium Priority Risks
- **Risk 1**: Over-asserting implementation-defined tolerance (e.g. demanding an
  exact recorded size/hash when grow/shrink makes the stamped size and the
  hashed bytes legitimately diverge), producing a brittle test.
  - **Mitigation**: Assert *coherence* (success + clean load + entry
    present/parseable), not exact byte semantics — the philosophy TC-8 already
    adopted over the stricter e-testing-plan wording.
- **Risk 2**: The optional walk-phase (lstat ENOENT mid-walk) race has **no
  seam**; testing it deterministically would require a small production change
  (a walk-phase hook mirroring `hashPreReadHook`).
  - **Mitigation**: Treat it as a Decision in requirements — take it only if a
    minimal, low-surface seam is justified; otherwise defer with a note. The
    three in-scope criteria stand without it.

## Dependencies
- No external/team dependencies. Builds entirely on Task-23 test infrastructure.

## Constraints
- Test-only for the in-scope criteria (seam reuse, no production change).
- Must hold British-spelling, gosec, and `-race` (`-d=checkptr=0`) gates.
- Reuse existing helpers; do not introduce parallel scaffolding (Rule of Three
  not met for new abstractions).

## Decomposition Check
- [ ] **Time**: >1 week? No — ~half a day.
- [ ] **People**: >2 people? No — single author.
- [ ] **Complexity**: 3+ distinct concerns? No — one concern (boundary-race
      tolerance tests reusing one seam).
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Separable parts? No — one cohesive test family.

**Verdict**: 0 signals triggered — no decomposition. Single focused chore.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 27
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. TC-10 (grow), TC-11 (shrink), TC-12 (file→dir)
added to `pkg/scan_edge_cases_test.go`, all passing and `-race`-clean, no
production change. The walk-phase lstat-ENOENT case was deferred at plan time
(approved) and re-filed as a Low-priority backlog item. Effort matched the
~0.5-day estimate.

## Lessons Learned
Re-scoping against Task 23 early (Explore sweep) was the key move — it kept the
chore narrow and avoided re-testing TC-7/8/9 + `atomic_index_test.go`. The
coherence-only oracle (Risk 1 mitigation) held up against the grow/shrink
brittleness it was meant to prevent.
