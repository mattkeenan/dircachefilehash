# Harden env-fragile dcfhfind and dcfhfix CI tests - Plan
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Baseline Commit**: 46c25e4b5933e37ea9d7ab425c958d38f0f99011
- **Template Version**: 2.1

## Goal
Make two environment-fragile tests deterministic so `make test` passes on a clean
CI checkout without changing shared production code.

## Success Criteria
- [x] `go test ./...` passes from a clean checkout (no pre-built binaries, no ambient `.dcfh/`)
- [x] `cmd/dcfhfind` `TestPerformanceWarning` skips (not fails) when `./dcfhfind` is absent, matching its sibling tests
- [x] `cmd/dcfhfix` `TestHandleFixesCommand/List` succeeds against a hermetic `.dcfh/` it controls, not the ambient repo dir
- [x] No change to shared production code (`getBackupDir`, `listBackups`, `handleFixesCommand` behaviour unchanged)
- [ ] CI workflow run on the branch goes green — to confirm post-merge on `main`

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None (test-only changes)

## Major Milestones
1. **dcfhfind fix**: `TestPerformanceWarning` uses `t.Skip` on a missing build artifact
2. **dcfhfix fix**: `TestHandleFixesCommand/List` runs against a `t.TempDir()`-rooted `.dcfh/`
3. **Verify**: `make test` green locally; CI green on branch

## Risk Assessment
### High Priority Risks
- **Risk 1**: A "hermetic" temp dir still discovers a real `.dcfh/` higher up the tree (e.g. under the test's working dir), reintroducing flakiness.
  - **Mitigation**: Root the index path under `t.TempDir()` (an isolated `/tmp` path with no `.dcfh/` ancestors) and create the `.dcfh/` dir there explicitly.

### Medium Priority Risks
- **Risk 2**: Skipping `TestPerformanceWarning` hides a real `--help` regression when binaries aren't built.
  - **Mitigation**: Acceptable — it is an integration test of the built binary, consistent with sibling tests that skip; the build+integration path is still exercised whenever `make build` precedes the run.

## Dependencies
- None — changes are confined to `cmd/dcfhfind/integration_test.go` and `cmd/dcfhfix/main_test.go`.

## Constraints
- Test-only: must not alter `getBackupDir`/`listBackups` (shared with `createBackup`).
- British spelling in prose; match surrounding test idiom.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: Will this take >1 week? No
- [x] **People**: Does this need >2 people? No
- [x] **Complexity**: 3+ distinct concerns? No — two small, isolated test fixes
- [x] **Risk**: High-risk components needing isolation? No
- [x] **Independence**: Can parts be worked on separately? Trivially, but not worth separate tasks
- **Verdict**: No decomposition — single small chore.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered as scoped: `cmd/dcfhfind/integration_test.go` (4× `t.Fatal`→`t.Skip`) and
`cmd/dcfhfix/main_test.go` (hermetic `t.TempDir()/.dcfh/` + per-case `indexFile`). No
production code changed. Local `make test` green; the clean-checkout SKIP path verified by
moving the binary aside. CI-on-branch green is the only criterion left to confirm post-merge.

## Lessons Learned
See j-retrospective.md. Headline: ambient-state coupling in tests (gitignored `.dcfh/`,
in-place build artifacts) stays invisible until a clean-checkout CI runs them; hermetic temp
roots and skip-on-missing-artifact remove it.
