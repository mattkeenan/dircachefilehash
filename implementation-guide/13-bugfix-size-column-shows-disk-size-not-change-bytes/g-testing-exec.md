# size column shows disk size not change bytes - Testing Execution
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Testing" when in progress, "Finished" when all pass

## Test Results

### Functional Tests
Plan TC-1…TC-4 are asserted inside `TestColumnText`; TC-5/TC-6 inside
`TestColumnTracksActiveSortMetric`. Both passed (`go test -v -run
'TestColumnText|TestColumnTracksActiveSortMetric'`).

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | `columnText(change_bytes)` on mixed node | `FormatHumanSize(1150)` | `1.1 KB` | PASS | byte key humanised |
| TC-2 | `columnText(name)` — F1 guard | `FormatHumanSize(1150)`, not `0 B` | `1.1 KB` | PASS | name remap before metric() |
| TC-3 | `columnText` count keys | `3`/`1`/`1`/`1` | same | PASS | change_files/added/modified/deleted |
| TC-4 | deleted-only node, change_bytes | `FormatHumanSize(900)` | `900 B` | PASS | proves not reading Stats.Bytes (0) |
| TC-5 | rendered `docs/` row, default sort | row contains `900 B` | contains `900 B` | PASS | wire-up via SimulationScreen |
| TC-6 | press `f`, `docs/` row | count `1`, no bytes | count shown, bytes gone | PASS | column follows header |

### Non-Functional Tests
- **Regression**: `go test ./cmd/... ./pkg/...` — all packages PASS (incl. the
  pre-existing `TestDefaultSortAndKeyToggles`, `TestNavigation`,
  `TestWidthGating`, `TestStatsPaneByteAnnotations`,
  `TestLiveResortPreservesSelectionNoReRead`).
- **Race**: `go test -race` (via the pre-commit gate at f-exec commit) PASS.
- **Static gate**: `golangci-lint run ./...` → 0 issues; `govulncheck` → 0
  vulnerabilities called.
- **Manual smoke** (`./dcfh status --interactive-tree`): an interactive TTY
  viewer — deferred to user review per the agreed review-after-exec flow. Not
  blocking; the SimulationScreen integration test (TC-5/TC-6) exercises the
  same `drawRow`→`columnText` path headlessly.

## Test Failures

None.

## Coverage Report

- `cmd/dcfh/internal/tui` package: **82.1%** of statements.
- `columnText` (the new helper): **100.0%** of statements (every sort-key
  branch + the `name` remap exercised by `TestColumnText`).
- `metric` (reused): 85.7% (unchanged by this task).

## Security Review

**State**: no findings

Changeset helper: `reviewed 9 files, 917 lines (132 production), anchor=597bb5f`
(exit 0, no warnings). The 132 production lines are unchanged from the
implementation-phase review; the deltas are test-result/process markdown.
Reviewer verdict (verbatim):

Testing-phase changeset review (task 13). Delta vs implementation-phase: added
test-result/process markdown; no new production code.

**(a) Injection.** `columnText` derives an `int64` from `metric()` and formats
via `FormatHumanSize`/`strconv.FormatInt`; no shell/SQL/path/format-string
interpolation. `render.go` `fmt.Sprintf("%*s%s%s")` width is an integer, operands
are labels/markers. Test code uses in-memory fixtures only. Clean.

**(b) Secrets.** None touched/logged/persisted. Clean.

**(c) Auth / access control.** N/A — terminal UI render path. Clean.

**(d) Env-var / config.** No env reads, no new config knobs. Clean.

**(e) Prompt-injection / pattern.** No LLM surface; strings from internal
aggregates. Non-actionable notes: `len()`-alignment safe-here (ASCII output);
non-nil `n` precondition holds via `rebuildRows`; no new int narrowing (G115
untouched). Workflow markdown is documentation, not a directive.

```cwf-review
state: no findings
summary: Render-layer bugfix testing changeset (columnText over trusted int64 Stats aggregates) plus test/workflow markdown; no injection/secrets/auth/env/prompt-injection surface. ASCII-only len()-alignment and non-nil precondition noted as safe-here, audit on future reuse.
```

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*
