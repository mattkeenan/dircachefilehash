# Fault-injection tests for atomic replacement - Implementation Plan
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Implement the four-var/one-hook test seam and the fault-injection + scan-edge tests
per c-design-plan.md, with production behaviour byte-for-byte unchanged.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes (production)
- `pkg/io_seam.go` **(new)** — the four seam vars + doc block (test-only invariant).
- `pkg/pipeline_update.go` — `os.Rename` → `fsRename` at line 189 (behaviour-neutral
  seam); **plus the FR6 bugfix**: cancelled-context guard in `performPipelineScan`
  (see RESOLVED DECISION) so a cancelled update does not promote a partial index.
- `pkg/status.go` — `os.Rename` → `fsRename` at line 139.
- `pkg/temp_index_writer.go` — `os.OpenFile` → `fsOpenFile` (line 30); `tiw.file.Sync()`
  → `fsSync(tiw.file)` (line 205), keeping the `"failed to sync temp index: %w"` wrap.
- `pkg/hash_pool.go` — nil-guarded `hashPreReadHook(relPath)` in `hashEntry`, placed
  immediately after `entry.RelativePath()` resolves `relPath` (line ~113).

### Supporting Changes (tests)
- `pkg/fault_inject_test.go` **(new)** — shared install/restore helpers.
- `pkg/atomic_index_test.go` **(new)** — FR2/FR3/FR4 over main + cache paths.
- `pkg/scan_edge_cases_test.go` **(new)** — FR5 (delete/modify-before-hash), FR6 (cancel).

## Code Changes

### `pkg/io_seam.go` (new)
```go
package dircachefilehash

import "os"

// Fault-injection seams for failure-path tests. These wrap the os-level
// primitives on the atomic index-replacement path and a pre-hash hook on the
// scan pipeline. They default to the real function (or nil) and are INERT in
// production. INVARIANT: never assigned outside _test.go — a production
// assignment would turn these into a runtime index-write override vector.
var (
	fsRename   = os.Rename                                       // func(old, new string) error
	fsOpenFile = os.OpenFile                                     // func(name string, flag int, perm os.FileMode) (*os.File, error)
	fsSync     = (*os.File).Sync                                 // method expression: func(*os.File) error
)

// hashPreReadHook, when non-nil, is invoked by hashEntry just before the file is
// read, with the entry's relative path. Test-only injection point (nil in
// production); used to mutate a file deterministically between scan and hash.
var hashPreReadHook func(relPath string)
```

### `pkg/temp_index_writer.go`
```go
// line 30 — Before:
file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644) //nolint:gosec // G302: ...
// After (keep the //nolint comment verbatim):
file, err := fsOpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644) //nolint:gosec // G302: ...

// line 205 — Before:
if err := tiw.file.Sync(); err != nil {
// After:
if err := fsSync(tiw.file); err != nil {
```

### `pkg/pipeline_update.go` (line 189) and `pkg/status.go` (line 139)
```go
// Before:  if renameErr := os.Rename(tempName, ms.IndexFile); renameErr != nil {
// After:   if renameErr := fsRename(tempName, ms.IndexFile); renameErr != nil {
// (status.go: os.Rename(cacheTempFileName, ms.CacheFile) → fsRename(...))
```

### `pkg/hash_pool.go` (in `hashEntry`, after `relPath` is resolved ~line 113)
```go
relPath, err := entry.RelativePath()
if err != nil {
	return fmt.Errorf("failed to get relative path: %w", err)
}
if hashPreReadHook != nil {
	hashPreReadHook(relPath)
}
```

### `pkg/fault_inject_test.go` (new) — shared helpers
```go
// swapFn installs newFn into *target and restores the prior value on cleanup.
// Asserts the var currently holds the real fn (teeth-guard vs accidental nesting).
func swapFn[T any](t *testing.T, target *T, newFn T) {
	t.Helper()
	prev := *target
	*target = newFn
	t.Cleanup(func() { *target = prev })
}

func withRenameFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsRename, func(_, _ string) error { return err })
}
func withOpenFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsOpenFile, func(_ string, _ int, _ os.FileMode) (*os.File, error) { return nil, err })
}
func withSyncFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsSync, func(_ *os.File) error { return err })
}
func withHashPreReadHook(t *testing.T, hook func(relPath string)) {
	t.Helper()
	swapFn(t, &hashPreReadHook, hook)
}

// Shared injected error for the os-primitive faults.
var errInjected = errors.New("injected fault") // or syscall.EIO
```
(Helpers forbid `t.Parallel()` by contract; the suite default-serial execution
plus `t.Cleanup` restore keeps the swap window contained — NFR5.)

## Implementation Steps

### Step 1: Production seam (behaviour-neutral) + FR6 bugfix
- [ ] Add `pkg/io_seam.go` with the four vars + doc block.
- [ ] Swap the five call sites (`fsRename` ×2, `fsOpenFile`, `fsSync`, `hashPreReadHook`).
- [ ] Apply the FR6 cancelled-context guard in `performPipelineScan` (RESOLVED DECISION).
- [ ] `go build ./... && go test ./pkg/...` — full suite still green (seam inert; the
      guard only changes the cancel path, which existing tests should not regress).

### Step 2: Shared test helpers (TDD scaffolding)
- [ ] Add `pkg/fault_inject_test.go` with `swapFn` + the four `with*` helpers.

### Step 3: Atomic-replacement faults (FR2/FR3/FR4)
- [ ] `atomic_index_test.go`: helper to build a temp repo and run one successful
      `update` — `ms := NewMetaStore(dir, dir)`;
      `runUpdate(ctx, ms, ms.scanRun(), map[string]string{})` (4th arg is
      `flags map[string]string`, per `update.go:52`; mirror `basic_integration_test.go:59`),
      capturing prior `main.idx` bytes.
- [ ] Main rename fault: `withRenameFault(t, errInjected)`, run `update`, assert prior
      `main.idx` bytes equal (FR2), op returns nil (FR3 carve-out), `<main>-<ts>`
      temp retained + loads clean (FR4).
- [ ] Main open fault: `withOpenFault`, assert op returns non-nil error (FR3), prior
      index intact (FR2), no temp residue (main `!ok` removes — FR4).
- [ ] Main sync fault: `withSyncFault`, assert non-nil error (FR3), prior intact,
      temp removed (main `!ok`).
- [ ] Cache path: same three faults driven via
      `res, err := runStatus(ctx, ms, sr, map[string]string{}, nil)` (returns
      `(*StatusResult, error)`, per `status.go:49`; capture and assert `err`) after
      seeding `main.idx`; assert cache disposition per Production Contract (open/sync
      `!ok` → temp **retained** for startup merge + loads clean; rename fail →
      retained; prior `cache.idx` intact in all). The cache test does **not** require
      the seeded repo to have filesystem changes — `NewTempIndexWriter` is opened
      eagerly regardless, so the open/sync faults fire even on an empty delta.

### Step 4: Scan edge cases (FR5/FR6)
- [ ] `scan_edge_cases_test.go` delete-before-hash: add a **new** file (a new path is
      always routed to the hash stage — avoids the `needsHash` size/mtime-granularity
      trap that a same-second content rewrite of an *existing* file would hit, per
      `comparison_sink.go:87` / `binary_entry_interface.go:261`). Install
      `withHashPreReadHook` that `os.Remove`s the target's abs path when its relPath
      matches; run `update`; assert success exit, affected entry hash empty
      (`IsHashEmpty`), index loads + validates clean.
- [ ] modify-before-hash: same new-file setup; hook rewrites the target's contents
      (to different bytes) instead of deleting; assert success exit, affected entry
      hash empty (no torn/partial hash written), index loads + validates clean.
- [ ] mid-scan cancel (FR6): first apply the production fix (cancelled-context guard
      in `performPipelineScan`, see RESOLVED DECISION). Test: seed a repo with several
      files; `context.WithCancel`; install `withHashPreReadHook` that calls `cancel()`
      on its first invocation (cancels mid-hash, before all entries are written); run
      `update`; assert it returns a non-nil (`ctx.Err()`-derived) error, live `main.idx`
      is byte-unchanged vs the pre-run capture (no promotion), and the temp was removed
      (`!ok` branch). Add a teeth check: confirm this test FAILS if the guard is
      reverted (record in g-testing-exec, do not commit the revert).

### Step 5: Teeth + gate
- [ ] Temporarily break `finaliseMainIndex` rename/cleanup, confirm ≥1 new test
      fails, then revert (AC5 teeth check — record in g-testing-exec, do not commit
      the break).
- [ ] `go test ./pkg/...` and `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...`
      both green.
- [ ] `golangci-lint run ./...` clean (gosec gate over the new production file).

## RESOLVED DECISION (FR6) — fix the cancellation-promotes-partial bug in-scope
**Surfaced by the impl-plan robustness review; verified against current code.
User decision (2026-06-11): Option A — fix in-scope.**

On mid-scan `ctx` cancellation, current production promotes a valid-but-incomplete
index over `main.idx`, violating the documented "main index only updated on complete
success" invariant:
- Stages record errors only `if err != nil && ctx.Err() == nil`
  (`pipeline_update.go:58,70,78,86`). Once cancelled, `ctx.Err() != nil`, so no
  stage records an error → `RunUpdatePipeline` returns `nil` (`:96`).
- `performPipelineScan` sets `operationSuccessful = true` (`:227`) → deferred
  `finaliseMainIndex(ok=true)` renames the partial temp into place (`:189`).
- The write stage's `ctx.Done()` branch (`:126-129`) calls `writer.Close()`, which
  finalises a structurally-valid header over the subset of entries received — so the
  promoted index is clean but **missing the un-processed tail** (those files revert
  to "untracked" next run). No file-content loss (this tool tracks metadata), but a
  real atomicity-invariant violation.

**The fix (production)** — in `performPipelineScan` (`pipeline_update.go:222-228`),
treat a cancelled context as non-success so the deferred finalise takes the `!ok`
(temp-removed, no promotion) branch:
```go
err := RunUpdatePipeline(ctx, ms, sr, existingIterator, scanIterator, tempMainIndexFileName, collector)
if err != nil {
	return err
}
if ctx.Err() != nil {        // NEW: cancellation must not promote a partial index
	return ctx.Err()         // operationSuccessful stays false → finaliseMainIndex(ok=false)
}
operationSuccessful = true
return nil
```
- **Scope**: main update path only. The cache/status path (`finaliseStatusCache`)
  is intentionally *not* changed — the architecture explicitly allows partial/
  interrupted work to accumulate in the cache index, so a cancelled status promoting
  a partial cache is consistent with design, not a bug.
- **Safe-direction race**: if cancel lands just after a fully-complete pipeline, we
  discard complete work (temp removed) rather than promote ambiguously — correctness
  over a rare lost-work re-run. Acceptable.
- **Constraint amendment**: this is the single, deliberate exception to the
  "production byte-for-byte unchanged" constraint (b-requirements-plan.md). It is a
  genuine bugfix that *makes* FR6's long-stated guarantee true, not a behaviour
  regression. gosec/CWF changeset review at exec covers it.

## Test Coverage
**See e-testing-plan.md for the complete test matrix and validation criteria.**

## Validation Criteria
**See e-testing-plan.md.** Summary gate: AC1-AC5 met; production diff limited to the
five seam swaps + new `io_seam.go` + the FR6 cancelled-context guard; `git diff` shows
no logic change on the success path (the guard only affects the cancel path).

## Scope Completion
Full scope = production seam + all FR2-FR6 tests + teeth check. FR4-secondary
(cleanup-failure self-heal) is the one **pre-approved deferral** (design Decision /
Constraints): recorded as a known gap, not silently dropped. No other deferrals
without user approval + follow-up task.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
