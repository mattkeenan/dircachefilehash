# Scan edge-case integration tests - Implementation Plan
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Template Version**: 2.1

## Goal
Add three deterministic edge-case tests for the discovery→hash boundary
(file grow, file shrink, file→directory replacement), reusing the existing
`hashPreReadHook` seam and the TC-7/TC-8 test helpers. Test-only; no production
change for the in-scope cases.

## Mechanism (verified against production code)
- `pkg/hash_pool.go:115-117` — `hashEntry` invokes `hashPreReadHook(relPath)`
  immediately before the file is read. A hook that mutates the file changes what
  the subsequent read sees (the lever TC-8 already exploits).
- `pkg/hash_pool.go:120` — `entry.Mode()` returns the **scan-time stored** mode,
  not a fresh lstat. So replacing a regular file with a directory in the hook
  keeps the entry on the non-symlink branch (`pkg/hash_pool.go:131-132`,
  `HashOne`). The read then fails on a directory: on Linux `os.Open` on a
  directory *succeeds*, but the subsequent `file.Read` returns `EISDIR`
  (`pkg/hash.go:204-216`) — a non-nil hash error either way.
- `pkg/hash_pool.go:87-94` — a `hashEntry` error is logged and **swallowed**; the
  entry proceeds with an empty hash and the pipeline still succeeds. This is the
  tolerance TC-7 asserts for the delete case and that case 3 (file→dir) reuses.
- Net behaviour the tests pin:
  - **grow / shrink**: read succeeds over the new bytes → entry present,
    non-empty hash, run succeeds, index loads clean.
  - **file→directory**: read fails non-fatally → entry present, **empty** hash,
    run succeeds, index loads clean.

## Decision: walk-phase lstat-ENOENT race — DEFERRED
The readdir→lstat mid-walk race (`pkg/scan.go:224` `statAndFilter`) has **no
seam** (io_seam.go exposes only `fsRename`/`fsOpenFile`/`fsSync`/
`hashPreReadHook`). Testing it deterministically needs a new production lstat
seam, for a narrow race whose handling (silent skip) is arguably correct and
low-risk. Per "the best part is no part", this task stays **test-only** and
defers the walk-phase seam. A backlog note is added in the retrospective so the
gap is tracked, not lost. The three boundary cases above stand without it.

## Files to Modify
### Primary Changes
- `pkg/scan_edge_cases_test.go` — append three test functions (TC-10/11/12). No
  other file changes. New tests sit alongside TC-7/8/9 and share their imports.

### Supporting Changes
- None. No production code, no new helpers, no new test file.

## Helpers reused (all present on baseline — verified)
- `seedMainRepo(t)`, `writeFile(t, root, rel, contents)` — fixture setup
  (used by TC-7/8/9).
- `withHashPreReadHook(t, hook)` — installs the pre-read hook
  (`pkg/fault_inject_test.go:45`).
- `runUpdate(ctx, ms, ms.scanRun(), map[string]string{})` — drives a full scan.
- `assertLoadsClean(t, ms, ms.IndexFile)` — index-validates the promoted main.
- `freshFind(t, ms, rel)` — reloads the on-disk main and returns the entry.
- `entry.IsHashEmpty()` / `entry.HashString()` — hash-coherence assertions.

## Implementation Steps
### Step 1: Setup
- [ ] Confirm helpers above compile-resolve in `pkg/scan_edge_cases_test.go`
      (they are already referenced by TC-7/8/9 in the same file).

### Step 2: Add TC-10 — grow-before-hash tolerated
- [ ] Seed a new file `z.txt` ("zulu\n"); in the hook, when `relPath == "z.txt"`,
      rewrite it with strictly **longer** contents.
- [ ] Assert: `runUpdate` returns nil; `assertLoadsClean`; `freshFind("z.txt")`
      non-nil; `!IsHashEmpty()` (read over grown bytes succeeded → coherent hash).

### Step 3: Add TC-11 — shrink-before-hash tolerated
- [ ] Same shape, hook rewrites `z.txt` with strictly **shorter** (non-empty)
      contents.
- [ ] Assert: nil error; loads clean; entry present; `!IsHashEmpty()`.

### Step 4: Add TC-12 — file→directory-before-hash tolerated
- [ ] In the hook, `os.Remove(zAbs)` then `os.Mkdir(zAbs, 0o755)` for `z.txt`.
- [ ] Add a one-line comment: the `Remove`+`Mkdir` pair is safe because `z.txt`
      is a single new entry hashed by exactly one worker, so the hook fires once
      (same single-file assumption as TC-7/8). It would be non-idempotent under
      concurrent hook invocation if the test were generalised to many files.
- [ ] Assert: nil error (swallowed read failure); loads clean; entry present;
      `IsHashEmpty()` (mirrors TC-7's read-failure tolerance).

### Step 5: Validation
- [ ] `go test ./pkg/... -run 'ScanEdge'` green.
- [ ] `go test ./pkg/...` green (no regressions).
- [ ] `go test -race -gcflags=all=-d=checkptr=0 ./pkg/ -run 'ScanEdge'` green.
- [ ] gosec/golangci clean on the new file (the test-path exclusion covers
      `_test.go`; the one `os.WriteFile` mirrors TC-8's `//nolint:gosec // G306`).

## Code Changes
### Pattern (mirrors TC-8 at pkg/scan_edge_cases_test.go:60-82)
The new tests follow the exact TC-8 skeleton; only the hook body and the final
hash assertion differ per case. No before/after of production code — none is
touched. Illustrative hook bodies:

```go
// TC-10 grow
withHashPreReadHook(t, func(relPath string) {
    if relPath == "z.txt" {
        _ = os.WriteFile(zAbs, []byte("ZULU-GROWN-MUCH-LONGER-THAN-BEFORE\n"), 0o644) //nolint:gosec // G306: test temp file
    }
})

// TC-12 file→directory (single-file: hook fires once, see Step 4 note)
withHashPreReadHook(t, func(relPath string) {
    if relPath == "z.txt" {
        _ = os.Remove(zAbs)
        _ = os.Mkdir(zAbs, 0o755) //nolint:gosec // G301: test temp dir
    }
})
```

## Test Coverage
**See e-testing-plan.md for the complete test plan.** Summary: three new tests
(TC-10 grow, TC-11 shrink, TC-12 file→dir) extending the TC-7/8/9 family, each
asserting success-exit + clean-load + per-case hash coherence.

## Validation Criteria
**See e-testing-plan.md.** All five Step-5 checks above must pass; success
criteria 1–3 in a-task-plan map 1:1 to TC-10/11+TC-11/TC-12.

## Scope Completion
All three in-scope tests land in this task. The walk-phase seam is a recorded,
approved deferral (see Decision above), tracked via a retrospective backlog note
— not silent descope. No other work is deferred.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Implemented as planned: three test functions appended to
`pkg/scan_edge_cases_test.go`, no production change. The only deviation was a
full-audit `unparam` finding on the reused `freshFind` helper (tipped over by
the added all-`"z.txt"` call sites), suppressed with a rationale rather than
expanding the diff into the clean TC-7/8/9 lines. EISDIR-on-read mechanism for
TC-12 confirmed in practice (empty hash).

## Lessons Learned
The mechanism analysis (Linux `os.Open` on a directory succeeds; `EISDIR`
surfaces on `Read`) was correct and load-bearing for the TC-12 assertion. The
`unparam` whole-program behaviour is a reminder to run the full lint audit, not
just the `--new` staged gate, when touching shared helpers.
