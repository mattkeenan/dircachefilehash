# Fix outstanding golangci-lint cyclop and unparam - Plan
**Task**: 7 (chore)

## Task Reference
- **Task ID**: internal-7
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/7-fix-golangci-lint-cyclop-and-unparam
- **Baseline Commit**: 989f89016d0207cdf7cf5e34788cbdecee5d9183
- **Template Version**: 2.1

## Goal
Clear the three outstanding `golangci-lint run ./...` findings (two `cyclop` overruns and one `unparam`) via behaviour-preserving refactors so the lint gate is clean.

## Success Criteria
- [ ] `golangci-lint run ./...` reports **0 issues** (currently 3).
- [ ] `resolveOneSelector` (pkg/filter_run.go) and `parseTestToken` (cmd/dcfhfind/main.go) each have cyclomatic complexity ≤ 20.
- [ ] `createTestEntry` (pkg/binary_entry_scan_test.go) no longer carries the unused `t *testing.T` parameter.
- [ ] `go test ./...` passes — no behaviour change to selector resolution or expression parsing.

## Original Estimate
**Effort**: < 0.5 day
**Complexity**: Low
**Dependencies**: None

## Major Milestones
1. **Cyclop fixes**: Extract-method refactors on the two over-threshold functions.
2. **Unparam fix**: Drop the unused test-helper parameter and update its caller(s).
3. **Verify**: Lint clean + full test suite green.

## Risk Assessment
### High Priority Risks
- None. All three changes are mechanical and locally scoped.

### Medium Priority Risks
- **Behaviour drift during extraction**: a careless extract-method could alter the `--start-date`/`--end-date` or `scan`-selector logic.
  - **Mitigation**: Pure code movement only (no logic edits); rely on existing tests for `dcfhfind` parsing and selector resolution to catch regressions.

## Dependencies
- None.

## Constraints
- Behaviour-preserving only: no change to CLI surface, selector semantics, or parser output.
- No lowering of the `cyclop` threshold or adding `//nolint` suppressions — fix the code, not the gate.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: Will this take >1 week? No.
- [x] **People**: Does this need >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? No — one concern (lint cleanup), three small sites.
- [x] **Risk**: High-risk components needing isolation? No.
- [x] **Independence**: Can parts be worked on separately? Trivially, but not worth separate tasks.

**Verdict**: No decomposition — single small chore.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
