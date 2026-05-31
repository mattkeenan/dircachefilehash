# Fix outstanding golangci-lint cyclop and unparam - Implementation Plan
**Task**: 7 (chore)

## Task Reference
- **Task ID**: internal-7
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/7-fix-golangci-lint-cyclop-and-unparam
- **Template Version**: 2.1

## Goal
Clear the three `golangci-lint run ./...` findings via behaviour-preserving extract-method refactors and an unused-parameter removal.

## Workflow
Patterns first → behaviour-preserving extraction → lint + full test suite green → commit explains "why".

## Files to Modify
### Primary Changes
- `pkg/filter_run.go` — extract the `scan` selector case out of `resolveOneSelector` into a new unexported helper `resolveScanSelector(metaDir string) ([]IndexRef, error)`; the `case "scan":` body becomes `return resolveScanSelector(metaDir)`. Drops ~4 decision points (the glob `if err`, the `for`, the inner `HasPrefix && HasSuffix`), taking cyclop from 21 to ~17.
- `cmd/dcfhfind/main.go` — extract the second `switch` in `parseTestToken` (the four state-dependent cases `--min-size`, `--max-size`, `--start-date`, `--end-date`) into a new method `(p *ExpressionParser) parseStatefulArgTest(token string) (Expression, bool, error)` (named receiver `p` — the moved body calls `p.requireArg`/`p.globalArgs`). `parseTestToken` calls it and returns early when `handled` is true. Removes the whole 4-case switch and its ~8 error branches, taking cyclop from 21 to well under 20.
  - **Invariant**: each moved case already returns `handled=true` on its error paths (e.g. `return nil, true, err`); preserve that verbatim so a malformed `--min-size`/`--start-date` surfaces its error and does **not** fall through to the `argTestTable` lookup. The default (token not matched) returns `nil, false, nil`. The `false` branch keeps the new `bool` return from itself tripping unparam.
- `pkg/binary_entry_scan_test.go` — remove the unused `t *testing.T` parameter from `(h *scanTestHelper).createTestEntry`; update the 4 call sites (lines ~79, 105, 133, and the `&testing.T{}` throwaway at ~229).
  - **Caveat**: touch **only** the `scanTestHelper` receiver. Two sibling methods share the name `createTestEntry` — `skiplistTestHelper` (binary_entry_skiplist_test.go) and `indexFileMmapTestHelper` (binary_entry_index_file_mmap_test.go) — and both legitimately use `t` (e.g. `t.Fatalf`), which is why unparam flags only the scan one. Do not blanket find-replace; that would break ~9 unrelated call sites.

### Supporting Changes
- None. No production behaviour, CLI surface, or config changes.

## Implementation Steps
### Step 1: resolveOneSelector (cyclop)
- [ ] Add `resolveScanSelector(metaDir string) ([]IndexRef, error)` containing the current `case "scan":` body verbatim.
- [ ] Replace the `case "scan":` body with `return resolveScanSelector(metaDir)`.
- [ ] `golangci-lint run ./...` no longer flags `resolveOneSelector`.

### Step 2: parseTestToken (cyclop)
- [ ] Add `(*ExpressionParser) parseStatefulArgTest(token string) (Expression, bool, error)` holding the four inlined cases verbatim; default return `nil, false, nil`.
- [ ] In `parseTestToken`, after the no-arg switch, call the helper and `if handled { return ... }` before falling through to `argTestTable`.
- [ ] `golangci-lint run ./...` no longer flags `parseTestToken`.

### Step 3: createTestEntry (unparam)
- [ ] Change signature to `(h *scanTestHelper) createTestEntry() (*BEScanEntry, func())`.
- [ ] Update the 4 call sites (drop the `t` / `&testing.T{}` argument).
- [ ] `golangci-lint run ./...` no longer flags `createTestEntry`.

### Step 4: Validation
- [ ] `golangci-lint run ./...` → 0 issues.
- [ ] `go test ./...` → all pass (regression check for parser + selector behaviour).
- [ ] `go build ./...` clean.

## Code Changes
Pure code movement; see Implementation Steps for the exact extractions. No logic edits inside the moved blocks.

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
