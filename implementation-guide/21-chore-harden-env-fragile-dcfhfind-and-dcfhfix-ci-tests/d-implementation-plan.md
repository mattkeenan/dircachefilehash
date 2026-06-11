# Harden env-fragile dcfhfind and dcfhfix CI tests - Implementation Plan
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Template Version**: 2.1

## Goal
Implement Harden env-fragile dcfhfind and dcfhfix CI tests following the approved design and requirements.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Root-Cause Summary
Both failures are false negatives from tests that assume ambient state present only on
a developer machine, surfaced now that task 19's repaired CI runs `make test` (which runs
`go test ./...` without first running `make build`) on a clean checkout:

1. **dcfhfind** — `TestPerformanceWarning` (and 3 sibling integration tests) `t.Fatal` when
   `./dcfhfind` is absent. The 3 siblings are shielded by an earlier `t.Skip` on the missing
   `test-data/test-repo`, but `TestPerformanceWarning` has no such guard (it only runs
   `--help`), so it is the one that fails. A missing build artifact is a "cannot run" not a
   "test failed" condition → should `t.Skip`.
2. **dcfhfix** — `TestHandleFixesCommand/List` calls `handleFixesCommand("nonexistent.idx",
   ["list"], …)` and expects success. `list` → `fixesList` → `listBackups` → `getBackupDir`
   walks parent dirs for a `.dcfh/`. On a dev machine it finds the repo's gitignored
   `.dcfh/`; on a clean CI checkout there is none → "could not find .dcfh directory". The
   test must control its own `.dcfh/` rather than depend on the ambient repo.
   (Verified: `handleFixesCommand` validates args before dispatch — the "No subcommand" and
   "Unknown subcommand" cases never reach discovery, so only the `list` case needs a path.)

## Files to Modify
### Primary Changes
- `cmd/dcfhfind/integration_test.go` — convert the four `t.Fatal("dcfhfind executable not found…")`
  binary-missing checks to `t.Skip(…)` so a missing build artifact skips rather than fails.
- `cmd/dcfhfix/main_test.go` — make `TestHandleFixesCommand` hermetic: add `os` + `path/filepath`
  imports, give each case its own `indexFile`, and point the `list` case at an index path inside
  a `t.TempDir()` that has its own `.dcfh/` directory.

### Supporting Changes
- None. No production code changes (`getBackupDir`/`listBackups`/`handleFixesCommand` untouched).

## Implementation Steps
### Step 1: dcfhfind — skip on missing binary
- [ ] Replace all four `t.Fatal(…)` binary-missing checks (sites at `integration_test.go:34,
      171, 253, 328`) with `t.Skip("dcfhfind executable not found. Run 'make build' first.")`.
      NB: the four currently carry *two* different messages — line 34 already has the
      `…Run 'make build' first.` suffix; lines 171/253/328 have the bare
      `"dcfhfind executable not found"`. Unify all four on the suffixed `t.Skip` message.

### Step 2: dcfhfix — hermetic `.dcfh/` for the list case
Follow the existing precedent in this package: `promote_integration_test.go:157-163`
(`TestEntryRemove_BackupStackCoexistsWithSibling`) already does `dir := t.TempDir()` +
`os.Mkdir(filepath.Join(dir, ".dcfh"), 0755)`. Mirror it exactly (`os.Mkdir`, bare `0755`).
- [ ] Add `"os"` and `"path/filepath"` to the import block.
- [ ] In `TestHandleFixesCommand`, create `tmpDir := t.TempDir()` and
      `os.Mkdir(filepath.Join(tmpDir, ".dcfh"), 0755)`.
- [ ] Add an `indexFile string` field to the test-case struct; set the error cases to
      `"nonexistent.idx"` and the `list` case to `filepath.Join(tmpDir, "nonexistent.idx")`.
      The index path must sit **directly** in `tmpDir` (not a deeper subdir) so
      `filepath.Dir(indexFile) == tmpDir`: `getBackupDir` finds the `.dcfh/` on its first
      walk iteration and never consults ancestors of the system temp dir — that is what
      makes the case hermetic.
- [ ] Change the call site to `handleFixesCommand(tt.indexFile, tt.args, options)`.
- [ ] Leave the loop body below the call site unchanged — the two error cases still assert
      `strings.Contains(err.Error(), tt.errMsg)` under `if tt.wantErr`.

### Step 3: Validate
- [ ] `make test` passes locally.
- [ ] Simulate a clean checkout for the list path: confirm `getBackupDir` resolves the
      temp `.dcfh/` and `listBackups` returns no backups (success).
- [ ] Confirm `TestPerformanceWarning` reports SKIP when `./dcfhfind` is removed.
      (Known coverage gap, acceptable for a chore: the skip path itself is not asserted by
      an automated test, so a future `t.Fatal` regression would not be caught in CI.)

## Code Changes
### dcfhfind (×4 sites: lines 34, 171, 253, 328)
```go
// Before — note two existing message variants across the four sites:
//   line 34:            t.Fatal("dcfhfind executable not found. Run 'make build' first.")
//   lines 171,253,328:  t.Fatal("dcfhfind executable not found")
if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
    t.Fatal("dcfhfind executable not found"...)
}
// After (all four unified):
if _, err := os.Stat(dcfhfindPath); os.IsNotExist(err) {
    t.Skip("dcfhfind executable not found. Run 'make build' first.")
}
```

### dcfhfix `TestHandleFixesCommand`
```go
// Hermetic .dcfh so getBackupDir resolves without depending on the ambient repo.
// Mirrors promote_integration_test.go:157-163.
tmpDir := t.TempDir()
if err := os.Mkdir(filepath.Join(tmpDir, ".dcfh"), 0755); err != nil {
    t.Fatalf("failed to create temp .dcfh: %v", err)
}

tests := []struct {
    name      string
    args      []string
    indexFile string
    wantErr   bool
    errMsg    string
}{
    {name: "No subcommand", args: []string{}, indexFile: "nonexistent.idx", wantErr: true, errMsg: "requires subcommand"},
    {name: "Unknown subcommand", args: []string{"unknown"}, indexFile: "nonexistent.idx", wantErr: true, errMsg: "unknown fixes subcommand"},
    {name: "List command (will succeed with no backups)", args: []string{"list"}, indexFile: filepath.Join(tmpDir, "nonexistent.idx"), wantErr: false},
}
// ... loop unchanged except the call site:
err := handleFixesCommand(tt.indexFile, tt.args, options)
```

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

## Plan Review
4 parallel reviewers (improvements, misalignment, robustness, security). All confirmed the
root-cause analysis against source and found the scope minimal (test-only, no production
change). Applied: cite + mirror the `promote_integration_test.go:157-163` precedent
(`os.Mkdir`/bare `0755`); show both existing `t.Fatal` message variants so all four sites
are unified; state the loop body is unchanged and why the parent-walk stays hermetic.
Security: no FR4 concerns — the change narrows env coupling (hermetic `.dcfh/` under a
trusted `t.TempDir()`).

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
