# Byte-weighted default sort for interactive-tree - Testing Execution
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Execute the TC-1..TC-16 cases from e-testing-plan.md against the
implemented change and record results.

## Environment
- Go per go.mod (≥1.25.0); `tcell/v2` SimulationScreen for render tests
  (no real TTY needed in CI). Temp-repo fixtures via `newTestRepo` /
  `t.TempDir()` — test indices only, never a real user index.
- Commands: `go test ./...`, `go test -v -run <TC>`,
  `go test -race -gcflags=all=-d=checkptr=0 ./pkg/`,
  `golangci-lint run ./...` (gosec inside).

## Test Results

### Functional Tests

| Test ID | Test (Given/When/Then) | Test func | Status |
|---------|------------------------|-----------|--------|
| TC-1 | per-category byte aggregation sums up; live Bytes/Files exclude deleted | `TestBuildTree_ByteAggregation` (+ existing `TestBuildTree_AggregationSumsUp`) | PASS |
| TC-2 | deleted-byte dual source (index tombstone vs `DeletedSizes`) identical; >2³² size | `TestBuildTree_DeletedBytes_DualSourceIdentical` | PASS |
| TC-3 | both-present precedence — tombstone size wins, one node, no double-count | `TestBuildTree_DeletedBytes_BothPresentPrecedence` | PASS |
| TC-4 | modified-byte = current size + deleted-byte identity, cross-path (real temp repo, status + update) | `TestPostRunTree_CrossPathByteIdentity` (subtests status, update) | PASS |
| TC-5 | `Apply(CollectChanges:true)` populates `DeletedSizes`; path-sets unchanged | `TestApply_CollectChangesDeletedSizes`, `TestApply_NoDeletedSizesByDefault` | PASS |
| TC-6/TC-7 | `change_bytes` orders by byte sum, int64-correct past 2³¹, default desc, reverse flips | `TestSortNodes_ChangeBytes` | PASS |
| TC-7 | default header `sort:change_bytes(desc)` before any keypress | `TestDefaultSortAndKeyToggles` | PASS |
| TC-8 | rename labels: `change_bytes`/`change_files`; no bare `change` label | `TestSortKeyLabels`, `TestDefaultSortAndKeyToggles` | PASS |
| TC-9 | key map `c`→bytes `f`→files `a/m/d/n`; live re-sort; no data re-read | `TestKeyForRune`, `TestLiveResortPreservesSelectionNoReRead` | PASS |
| TC-10 | direction toggle `r` flips desc↔asc for active metric | `TestDefaultSortAndKeyToggles` (asserts `change_files(asc)` after `r`) | PASS |
| TC-11 | stats-pane byte annotations `Added: N (…)` etc.; narrow screen omits pane without panic | `TestStatsPaneByteAnnotations`, `TestWidthGating` | PASS |
| TC-12 | no second walk — re-sort derives from loaded merge, no fs access | `TestLiveResortPreservesSelectionNoReRead`, `TestPostRunTree_*` (builder fs-free) | PASS |

### Non-Functional Tests

| Test ID | Check | Result |
|---------|-------|--------|
| TC-13 | byte-identity regression: collect-off vs collect-on main.idx identical | PASS (`TestApply_CollectChangesByteIdentical`) |
| TC-14 | `-race -d=checkptr=0 ./pkg/` clean (collector single-writer / read post-pipeline) | PASS |
| TC-15 | `golangci-lint run ./...` (gosec) — 0 issues; no new G115 suppression (int64 end-to-end) | PASS |
| TC-16 | sizes via `FormatHumanSize`; British spelling in prose | PASS (verified in render.go + comments) |

### Manual real-terminal checklist
- Off-TTY smoke (piped): `dcfh status --interactive-tree` cleanly skips
  the viewer and prints the normal status output; `--json` unaffected —
  **verified** in a throwaway temp repo (`/tmp` fixture, removed after).
- Live in-TTY toggle (`c`/`f`/`r`, pane byte breakdown, biggest-by-bytes
  first): the rendered header/footer/pane assertions are covered headless
  by the SimulationScreen tests (TC-7/TC-8/TC-10/TC-11). A real-terminal
  visual pass is left to the operator at rollout; not headlessly drivable.

## Test Failures
None.

## Coverage Report
No numeric line-coverage gate (consistent with the repo). Coverage is the
TC case list above: all critical paths (change_bytes aggregation,
deleted-byte dual source incl. precedence, int64 metric, rename labels +
key map, default-sort selection) and edge cases (empty/no-change, >2³¹
subtree, both-source deletion, nil `DeletedSizes`, modified current size)
exercised. Full suite `go test ./...` green.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
SimulationScreen render tests stand in for the headless-undrivable
real-terminal checklist (header/footer/pane assertions). The semantic
security review did not run (cap exceeded by test LOC) — config follow-up
filed. See j-retrospective.md.

## Security Review

**State**: error

error: cap exceeded: 577 production lines > 500

Helper `security-review-changeset --phase=testing --max-lines=500` exited 2
(production-weighted count over cap), so per the cwf-testing-exec contract the
`cwf-security-reviewer-changeset` subagent was NOT invoked and this is recorded
as `error` (never silently downgraded to "no findings").

Same root cause as the implementation phase: the production-weighted count is
dominated by the newly-added Go **test** files, which this repo does not list
in `security.review.max-lines-exclude-paths` and so count as production
(`reviewed 20 files, 1927 lines (577 production)`). The actual production-code
change remains ~154 lines and adds no new untrusted-input path (deleted sizes
are `int64` from the index/collector, carried in a `map[string]int64`, rendered
only via numeric `FormatHumanSize`; `sanitiseLabel` untouched). The full diff
was printed to stdout by the helper for manual review.
