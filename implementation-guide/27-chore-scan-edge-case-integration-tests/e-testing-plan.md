# Scan edge-case integration tests - Testing Plan
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Template Version**: 2.1

## Goal
Define the three new test cases that pin dcfh's tolerant behaviour at the
discovery→hash boundary. For this chore the test cases ARE the deliverable, so
this plan is the authoritative spec the exec phase implements verbatim.

## Test Strategy
### Test Levels
- **Integration tests only.** Each case drives a full `runUpdate` scan/hash/
  promote cycle against a real on-disk repo (`seedMainRepo` temp dir), then
  re-reads the promoted main index from disk. This is the same level as the
  TC-7/8/9 family they extend — not unit-level, because the behaviour under test
  is the pipeline's end-to-end tolerance, not a single function.
- No new unit tests, no system/acceptance tier (a library-level chore).

### Test Coverage Targets
- **New lines**: 100% of the three added test functions execute.
- **Branch coverage delta**: the hash-error swallow branch
  (`pkg/hash_pool.go:87-94`) is exercised by TC-12 (already hit by TC-7, so this
  is reinforcement, not new); the grow/shrink read paths exercise the normal
  `HashOne` success branch under a changed-size file — behaviour not previously
  asserted.
- **Regression**: full `go test ./pkg/...` stays green.

## Test Cases
### Functional Test Cases

- **TC-10 — grow-before-hash tolerated** (success criterion 1)
  - **Given**: a seeded main repo plus a new file `z.txt` ("zulu\n"); a pre-read
    hook installed that, on `relPath == "z.txt"`, rewrites it with strictly
    longer contents.
  - **When**: `runUpdate(ctx, ms, ms.scanRun(), {})` runs (new file → always
    hashed; hook fires once just before the read).
  - **Then**: the call returns nil; `assertLoadsClean` passes on the promoted
    main; `freshFind("z.txt")` is non-nil; `!IsHashEmpty()` (the read over the
    grown bytes succeeded → coherent hash). We do NOT assert the recorded size
    equals the grown size — the stamped scan-time size and the hashed bytes
    legitimately diverge; coherence, not exact byte semantics, is the oracle
    (the TC-8 philosophy).

- **TC-11 — shrink-before-hash tolerated** (success criterion 2)
  - **Given**: same setup; the hook rewrites `z.txt` with strictly shorter but
    still non-empty contents.
  - **When**: `runUpdate` runs.
  - **Then**: returns nil; loads clean; entry present; `!IsHashEmpty()` (read of
    the shorter file still succeeds). Same coherence-only oracle as TC-10.

- **TC-12 — file→directory-before-hash tolerated** (success criterion 3)
  - **Given**: same setup; the hook does `os.Remove(zAbs)` then
    `os.Mkdir(zAbs, 0o755)`. `entry.Mode()` still reports the scan-time regular
    file, so the entry stays on the non-symlink `HashOne` branch; the read of a
    directory fails (`EISDIR`) and is swallowed.
  - **When**: `runUpdate` runs.
  - **Then**: returns nil (read failure is non-fatal); loads clean; entry
    present; `IsHashEmpty()` (no bytes hashed) — mirroring TC-7's delete
    tolerance.

### Non-Functional Test Cases
- **Reliability / data integrity (NFR5)**: every case asserts `assertLoadsClean`
  on the promoted main — the index must validate (signature, checksum, entry
  layout) after a boundary race. This is the core integrity guarantee the chore
  protects.
- **Determinism (NFR3 testability)**: no sleeps, no wall-clock, no real FS race.
  The single deterministic seam (`hashPreReadHook`) fires synchronously inside
  `hashEntry`, so ordering is fixed. Tests must NOT call `t.Parallel()` — the
  hook is process-global shared state restored by `swapFn`/`t.Cleanup`.
- **Race safety**: all three pass under `go test -race
  -gcflags=all=-d=checkptr=0 ./pkg/`. Single-file fixture → hook fires on one
  worker → no concurrent hook invocation (the TC-12 Remove/Mkdir non-idempotency
  is therefore not reachable; documented inline).
- **Security**: no production attack surface; the two FS-write sites use
  constant test-controlled paths with gosec suppressions mirroring TC-8
  (`G306` on `WriteFile`, `G301` on `Mkdir`).
- **Performance**: N/A — three sub-second integration tests.

## Test Environment
### Setup Requirements
- Standard `go test ./pkg/...` on a Unix host (repo baseline requirement).
- Per-test isolated temp repo via `seedMainRepo(t)` (uses `t.TempDir()`); no
  shared fixtures, no network, no test doubles beyond the existing seam.

### Automation
- Runs in the existing CI `go test ./pkg/...` job and the `.githooks/pre-commit`
  `-race` gate. No new tooling or CI change.

## Validation Criteria
- [ ] TC-10, TC-11, TC-12 present in `pkg/scan_edge_cases_test.go` and passing.
- [ ] `go test ./pkg/... -run 'ScanEdge'` green.
- [ ] `go test ./pkg/...` green (no regressions).
- [ ] `go test -race -gcflags=all=-d=checkptr=0 ./pkg/ -run 'ScanEdge'` green.
- [ ] golangci-lint (incl. gosec) clean on the changed file.
- [ ] Success criteria 1–3 in a-task-plan each map to a passing case
      (1→TC-10, 2→TC-11, 3→TC-12).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All validation criteria met (see g-testing-exec.md): TC-10/11/12 present and
passing, `go test ./pkg/...` green, `-race` green, lint clean, success criteria
1→TC-10 / 2→TC-11 / 3→TC-12. Coverage confirmed the swallow branch
(`hash_pool.go worker`) and `HashOne` success path are exercised.

## Lessons Learned
The Given/When/Then spec mapped 1:1 onto the implementation with no test-plan
revisions needed — writing the per-case oracle precisely at plan time (coherence
vs exact-bytes) avoided rework during exec.
