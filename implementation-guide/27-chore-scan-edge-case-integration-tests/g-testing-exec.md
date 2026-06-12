# Scan edge-case integration tests - Testing Execution
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify the implementation
from d-implementation-plan.md: the three discovery→hash boundary-race cases.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (Unix host, `go test`)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-10 | grow-before-hash (`TestScanEdge_GrowBeforeHash_Tolerated`) | nil error, loads clean, entry present, `!IsHashEmpty()` | as expected | PASS | read over grown bytes succeeds → coherent hash |
| TC-11 | shrink-before-hash (`TestScanEdge_ShrinkBeforeHash_Tolerated`) | nil error, loads clean, entry present, `!IsHashEmpty()` | as expected | PASS | read over shrunk bytes succeeds → coherent hash |
| TC-12 | file→directory-before-hash (`TestScanEdge_FileToDirBeforeHash_Tolerated`) | nil error, loads clean, entry present, `IsHashEmpty()` | as expected | PASS | read fails `EISDIR`, swallowed → empty hash |

Command: `go test ./pkg/ -run 'ScanEdge_(Grow|Shrink|FileToDir)' -v` → 3/3 PASS.

### Non-Functional Tests

| NFR | Criterion | Method | Result |
|-----|-----------|--------|--------|
| NFR5 reliability/integrity | promoted main validates (signature/checksum/layout) after each boundary race | `assertLoadsClean` in every case | PASS |
| NFR3 determinism | no sleeps/wall-clock/real FS race; single synchronous `hashPreReadHook` seam; no `t.Parallel()` | code inspection + repeated runs | PASS |
| Race safety | `go test -race -gcflags=all=-d=checkptr=0 ./pkg/ -run 'ScanEdge'` | race detector | PASS (no data races) |
| Security | gosec/golangci clean on changed file (G306/G301 suppressions scoped to test temp paths) | `golangci-lint run ./pkg/` (phase f) | PASS — clean |
| Regression | full suite stays green | `go test ./pkg/...` | PASS |

Full repo `-race` gate also passed in the phase-f pre-commit hook
(`go test -race` across all packages).

## Test Failures
None.

## Coverage Report
- The three new tests in isolation
  (`-coverpkg=./pkg/ -run 'ScanEdge_(Grow|Shrink|FileToDir)'`): 18.2% of `./pkg`
  statements — these are integration tests that drive the whole scan/hash/
  promote path, not a coverage-maximising unit set.
- Targeted branch confirmation (the point of the chore):
  - `pkg/hash_pool.go:73 worker` — 71.4% (the hash-error **swallow** branch is
    reached via TC-12's `EISDIR`).
  - `pkg/hash_pool.go:107 hashEntry` — 75.0% (pre-read hook + `HashOne` success
    path exercised under grown/shrunk files by TC-10/11).

## Validation Criteria (from e-testing-plan.md)
- [x] TC-10, TC-11, TC-12 present in `pkg/scan_edge_cases_test.go` and passing.
- [x] `go test ./pkg/... -run 'ScanEdge'` green.
- [x] `go test ./pkg/...` green (no regressions).
- [x] `go test -race -gcflags=all=-d=checkptr=0 ./pkg/ -run 'ScanEdge'` green.
- [x] golangci-lint (incl. gosec) clean on the changed file.
- [x] Success criteria 1–3 map to passing cases (1→TC-10, 2→TC-11, 3→TC-12).

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

I have the full changeset and threat model. The diff is entirely test-only Go code plus CWF process documentation (markdown plan files). Let me reason through the five threat categories.

The changeset comprises:
1. Four CWF process-doc markdown files in `implementation-guide/27-chore-scan-edge-case-integration-tests/` (`a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`, `f-implementation-exec.md`).
2. A modification to `pkg/scan_edge_cases_test.go` — three new Go test functions (TC-10/11/12) plus a comment/`//nolint:unparam` tweak on the `freshFind` helper.

**(a) Bash injection / unsafe command construction.** No shell command construction in the diff. The new Go test code uses `os.WriteFile`, `os.Remove`, `os.Mkdir`, and `filepath.Join` with constant string literals. No `exec.Command`, no `os/exec`. The markdown files quote `go test …` commands only as illustrations. Clean.

**(b) Perl helpers consuming git/user output.** No Perl in the changeset; no git-porcelain parsing. Not applicable.

**(c) Prompt injection via user-supplied strings.** The new test functions flow no untrusted strings into LLM context. No `{arguments}` substitution surface is introduced. `f-implementation-exec.md` embeds a prior security-review verdict including a fenced `cwf-review` block — but that lives inside the reviewed diff, not the reviewer's own output, and the classifier parses only the reviewer's actual response. No injection risk. Clean.

**(d) Unsafe environment-variable handling.** No env-var reads in the diff. All paths derive from `ms.RootDir` (a `t.TempDir()`-backed test root) joined with the constant literal `"z.txt"`. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** Two patterns, both already self-documented in the diff and non-actionable: (1) the hook `os.WriteFile`/`os.Mkdir` writes to a constant path under a per-test temp root — safe, with G306/G301 suppressions scoped to test temp files; audit future uses where the path becomes variable-derived. (2) the TC-12 `os.Remove`+`os.Mkdir` pair is safe because the single-file fixture means the hook fires once — already flagged inline.

Overall: test-only Go plus CWF process documentation, all filesystem writes confined to per-test temp roots using constant path components, no shell/Perl/env/injection/prompt-injection surface.

```cwf-review
state: no findings
summary: Test-only Go changes plus CWF plan docs; FS writes confined to per-test temp roots with constant paths; no injection/secrets/auth/env/prompt-injection surface.
```

## Lessons Learned
*To be captured during retrospective*
