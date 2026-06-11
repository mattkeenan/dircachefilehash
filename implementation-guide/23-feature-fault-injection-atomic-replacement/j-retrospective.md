# Fault-injection tests for atomic replacement - Retrospective
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-11

## Executive Summary
- **Duration**: ~1 day (estimated: 1.5-2 days; variance: ~−40%).
- **Scope**: Delivered all five success criteria — a test-only seam for the
  os-level write primitives (`os.Rename`/`os.OpenFile`/`*os.File.Sync`),
  fault-injection tests for each failure point on both main and cache paths,
  scan edge-case coverage (delete- and modify-before-hash), and concrete mid-scan
  cancellation coverage. One in-scope addition: the FR6 cancellation bug was
  fixed (not just tested), turning TC-9 into a bugfix-plus-proving-test.
- **Outcome**: Success. 9 fault tests (TC-1..TC-9) all pass; production footprint
  is 37 lines across 5 files; both CWF security reviews returned "no findings".

## Variance Analysis
### Time and Effort
- **Estimated**: 1.5-2 days total (Medium complexity), front-loaded on the seam +
  atomic-fault tests, with scan edge cases second.
- **Actual**: ~1 day across all phases. The seam landed as a single small
  `io_seam.go` + five one-line call-site swaps (no interface needed), which was
  the bulk of the risk and turned out cheap.
- **Variance**: Under estimate (~40% under). Driver: the package-level `var`
  indirection (Risk-1 mitigation) was sufficient as predicted — no escalation to
  a `FileOps` interface — so the "blast radius" risk never materialised. Time
  that *was* spent went into three test-oracle corrections (below), not the seam.

### Scope Changes
- **Additions**:
  - **FR6 cancellation fix (in-scope by user decision)**: the mid-scan-cancel
    path was promoting a partial index. Added a ~2-line `if ctx.Err() != nil`
    guard in `performPipelineScan`; TC-9 proves it. This shifted FR6 from
    "test existing behaviour" to "fix + prove", a net quality gain.
- **Removals**:
  - **`pkg/shutdown_test.go` un-skip deferred (Risk-3 path)**: the old skipped
    test is structurally stale against the v0.7 channel pipeline. Per the
    pre-agreed mitigation, wrote a fresh focused cancellation test (TC-9) instead
    of resurrecting it; the stale test is left for separate retirement.
  - **Wire-handler write site left unseamed (Risk-4)**: `wire_handler.go:445`
    (audit-mode SSH path, separate cache format) stayed out of scope; keeping the
    seam off the untrusted surface was also the sound security choice.
- **Impact**: Net positive — one real bug fixed, scope held to the main+cache
  daily-operation paths as planned.

### Quality Metrics
- **Test Coverage**: Behavioural — every Production-Contract row asserted on both
  main and cache (open/sync/rename), plus FR5 delete/modify and FR6 cancel. No
  numeric line target (the point was a previously-uncovered failure path). Teeth
  checks T-A/T-B/T-C confirm none of the 9 pass vacuously.
- **Defect Rate**: One production bug found and fixed (FR6 partial-promotion).
  One test-only data race found by `-race` (TC-9 cancel hook on multiple
  goroutines) and fixed with `sync.Once`. Zero post-implementation defects.
- **Performance**: NFR1 met by inspection — one pointer indirection per primitive,
  one nil-check per hashed entry, one `ctx.Err()` per update; none on the hot
  byte path. `golangci-lint` 0 issues, `govulncheck` clean, 20× `-race` loop no
  flakes.

## What Went Well
- **The minimal seam held.** The planned `var osRename = os.Rename` style
  indirection was enough; resisting a `FileOps` interface kept the production
  diff to 37 lines and the blast radius near zero. "Best part is no part" paid off.
- **Deterministic fault injection, not timing.** Driving delete/modify/cancel
  through a `hashPreReadHook` fired at a known pipeline stage (Risk-2 mitigation)
  produced zero flakes across a 20× race loop — no wall-clock sleeps in any
  assertion.
- **`-race` earned its keep.** It caught the multi-goroutine cancel-hook race
  that a plain bool would have hidden, and even cascaded a spurious failure into
  an unrelated wire test until fixed — a useful forcing function.
- **Teeth checks proved the tests bite.** Reverting each guard (T-A/T-B/T-C) and
  watching the right test fail confirmed the suite isn't vacuous; T-B notably
  showed TC-2 *correctly* still passes (an open fault never creates a temp), so
  TC-3 carries the cleanup-path teeth.

## What Could Be Improved
- **Test-oracle assumptions cost the most time.** Three planned assertions were
  wrong about reality and needed correction mid-exec (D1: `ValidateIndexHeader`
  fails even a normal index; D2: modify-before-hash yields a *coherent* hash, not
  an empty one; D3: the cancel hook races). Each was a "the obvious assertion is
  wrong" discovery. Cheap to fix but they'd have been cheaper to anticipate by
  reading the validator/hash-tolerance code during the *testing-plan* phase
  rather than at exec time.
- **Two divergent checksum validators is a latent trap.** D1 exposed that the
  repair/dcfhfind `validateHeaderChecksum` disagrees with the production
  `verifyHeaderChecksum` — a normally-promoted `main.idx` fails the former. Not
  this task's bug, but a sharp edge the next person will hit.

## Key Learnings
### Technical Insights
- **Package-level function-pointer seams are the right weight** for injecting
  os-primitive failures in Go: inert in production (default to the real call),
  test-swappable with `t.Cleanup`, no interface plumbing. Documenting the
  "never assigned outside `_test.go`" invariant inline in `io_seam.go` is what
  keeps it from degrading into a runtime override vector (both security reviews
  endorsed this framing).
- **A successful re-read produces a valid hash** — "modify before hash" is *not*
  a corruption case (D2). The genuine empty-hash case is "delete before hash",
  where the read itself fails and the pipeline tolerates it non-fatally.
- **The production loader is the only trustworthy "loads clean" oracle** here
  (`loadIndexFromFileWithTracking`), because the repair-path validator's checksum
  routine doesn't match the writer (D1).

### Process Learnings
- **Read the code an assertion depends on during testing-*plan*, not testing-exec.**
  All three deviations were discoverable from existing source (the validator, the
  hash-pool tolerance branch, the multi-worker hook fan-out). Front-loading that
  read would have made the plan's expected-values column right the first time.
- **Folding a bugfix into a test task worked cleanly** because it was tiny (~2
  lines) and the task already owned the proving test. The CWF flow absorbed it
  without a separate cycle.

### Risk Mitigation Strategies
- The two pre-identified high risks (over-abstraction, flaky concurrency) were
  both neutralised exactly by their planned mitigations — evidence the risk
  section was well-targeted, not boilerplate.
- Deferring the stale `shutdown_test.go` rather than forcing an un-skip (Risk-3)
  avoided sinking time into structurally-obsolete test code.

## Recommendations
### Process Improvements
- During testing-plan, for any assertion about an *existing* subsystem's output
  (checksums, hashes, validators), read that subsystem's code and pin the
  expected value to what it actually produces before writing the expected-results
  column. Treat "the obvious expected value" as a hypothesis to verify.

### Tool and Technique Recommendations
- Standardise the `swapFn[T any](t, target *T, newFn T)` + `t.Cleanup` restore
  helper for any future os-primitive fault injection in this repo — it's generic
  and already proven here.
- Keep running new concurrency tests under `-count=20 -race` before declaring
  them stable; it caught the TC-9 race that a single run did not.

### Future Work
- **Backlog (low)**: reconcile `ValidateIndexHeader` / `validateHeaderChecksum`
  with the production writer so the repair and dcfhfind paths can validate a
  normally-promoted index (surfaced by D1). Added to BACKLOG.
- **Backlog (low)**: retire the stale skipped `pkg/shutdown_test.go` now that
  TC-9 supplies fresh v0.7 cancellation coverage. Added to BACKLOG.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-11
**Sign-off**: Matt Keenan / Claude Opus 4.8

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md` … `e-testing-plan.md` (this task directory).
- Implementation/testing exec: `f-implementation-exec.md`, `g-testing-exec.md`
  (with security-review verdicts), `h-rollout.md`, `i-maintenance.md`.
- Production change: `pkg/io_seam.go` (new), `pkg/pipeline_update.go`,
  `pkg/status.go`, `pkg/temp_index_writer.go`, `pkg/hash_pool.go`.
- Tests: `pkg/fault_inject_test.go`, `pkg/atomic_index_test.go`,
  `pkg/scan_edge_cases_test.go`.
- Checkpoint commits preserved on the task's checkpoints branch.
