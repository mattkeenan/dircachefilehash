# Reconcile RelativePath pathStart offset - Testing Plan
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Template Version**: 2.1

## Goal
Pin the two corrected behaviours (`calculatePathLength` path length, `validateLayout`
no-panic) and prove `ValidateEntry` is genuinely live (accepts well-formed, rejects
corrupt) — all as fast unit tests in `pkg/format`.

## Test Strategy
### Test Levels
- **Unit Tests only** — the change is to two unexported helpers in `pkg/format`.
  New file `pkg/format/entry_test.go` (package `format`, white-box: can call
  unexported `calculatePathLength`/`validateLayout` and reuse `layEntry`).
- **Regression** — full `go test ./pkg/...` to confirm goldens/roundtrips and all
  downstream consumers are unaffected.

### Test Coverage Targets
- `calculatePathLength`: 100% (single return line).
- `validateLayout`: the Path-offset assertion branch exercised on a valid entry
  (no panic) — the previously-always-panicking path is now the pass path.
- `ValidateEntry`: both the success return (`nil`) and the size-mismatch error
  return (entry.go:186) exercised — the latter was dead code before this task.
- No coverage regression elsewhere.

## Test Cases
### Functional Test Cases
- **TC-1 (positive pin): `TestEntry_PathLength_MatchesRelativePath`**
  - **Given**: entries laid down by `layEntry` for paths of differing mod-8
    lengths — `"a"` (1), `"abcdefg"` (7), `"some/relative/path.go"` (21) — so the
    12-byte over-count cannot be masked by `expectedSize` padding.
  - **When**: reading `RelativePath()`, `calculatePathLength()`, calling
    `validateLayout()` (in a panic-catching closure), and `ValidateEntry()`.
  - **Then**: `RelativePath()==path`; `calculatePathLength()==len(path)` (the
    primary pin — was `len(path)+12` pre-fix); `validateLayout()` does not panic
    (pre-fix it always panicked); `ValidateEntry()==nil` as a genuine pass.

- **TC-2 (negative pin): `TestEntry_ValidateEntry_RejectsCorruptSize`**
  - **Given**: a well-formed `layEntry("some/relative/path.go")` buffer (Size 168).
  - **When**: `e.Size -= 8` (→160: in-bounds, 8-aligned, `> minSize`, inconsistent
    with the path), then `ValidateEntry()`.
  - **Then**: returns a non-nil error (size-consistency branch fires). Pre-fix this
    returns `nil` (swallowed `validateLayout` panic), so the assertion distinguishes
    live-validator from no-op.
  - **Guard**: corrupt **downward** only — inflating `Size` makes post-fix
    `RelativePath` scan past the exactly-sized buffer (OOB heap read → `checkptr`
    fatal under `-race`).

### Baseline (failing-first) expectations — run BEFORE applying the fix
- TC-1 `calculatePathLength` assertion: FAIL (returns len+12).
- TC-1 `validateLayout` no-panic assertion: FAIL (panics).
- TC-1 `ValidateEntry()==nil`: passes pre-fix too (vacuously, via swallowed panic)
  — not a discriminating assertion on its own; recorded as such.
- TC-2: FAIL (pre-fix `ValidateEntry` returns `nil`).
Record these in g-testing-exec as proof the tests exercise the bugs.

### Non-Functional Test Cases
- **Memory safety**: both new tests must stay in-bounds; run under the repo race
  gate (`go test -race -d=checkptr=0` per pre-commit) — no OOB, no checkptr fatal.
- **Performance**: N/A (microsecond unit tests; `calculatePathLength` change is
  zero-allocation via `unsafe.String`).
- **Security**: net reduction in unsafe-pointer surface (one path-offset owner);
  no new gosec finding after the G115 suppression is removed
  (`golangci-lint run ./pkg/format/...`).

## Test Environment
### Setup Requirements
- No external data, DB, or fixtures beyond the in-package `layEntry` helper.
- Standard `go test`; no network or filesystem.

### Automation
- `go test ./pkg/format/` (focused) and `go test ./pkg/...` (regression).
- `golangci-lint run ./pkg/format/...` for the gosec floor.
- CI runs the same suite; pre-commit runs the race gate.

## Validation Criteria
- [ ] TC-1 and TC-2 fail on baseline (pre-fix), pass post-fix.
- [ ] `go test ./pkg/...` green; race gate green.
- [ ] `golangci-lint run ./pkg/format/...` clean.
- [ ] No coverage regression.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1 and TC-2 implemented and green; baselines flipped red→green as planned.
Coverage targets met. See g-testing-exec.md.

## Lessons Learned
Pinning `validateLayout` no-panic directly (not via ValidateEntry's recover) and
the negative liveness test were both necessary to avoid vacuous assertions. See
j-retrospective.md.
