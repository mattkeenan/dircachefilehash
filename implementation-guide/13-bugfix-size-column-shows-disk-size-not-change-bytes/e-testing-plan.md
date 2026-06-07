# size column shows disk size not change bytes - Testing Plan
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1

## Goal
Prove the right-aligned column equals the active-sort metric (not `Stats.Bytes`)
across all sort keys, and that the wire-up holds end-to-end in `drawRow`.

## Test Strategy
### Test Levels
- **Unit** (`sort_test.go`): `columnText` in isolation — pure function over a
  `*dcfh.Node`, one assertion per sort key (the design behaviour table).
- **Integration** (`render_test.go`): the rendered frame via the tcell
  `SimulationScreen` — proves `drawRow` calls `columnText(node, m.sortKey)` and
  the drawn cells carry the right string (catches a helper that's correct but
  not wired up).
- **Regression**: the full existing `tui` + `pkg` suites must stay green
  (no behaviour change to non-interactive output, navigation, width-gating,
  stats pane).

### Coverage Targets
- `columnText`: 100% — every `sortKey` branch (6 keys) plus the `name` remap.
- The `drawRow` column path: exercised by ≥1 integration assertion per unit
  family (one byte key, one count key).
- No coverage % gate; the bug is a specific divergence, so the discriminating
  assertions below are the real target, not a number.

## Test Cases
### Functional — Unit (`columnText`)
- **TC-1 (change_bytes default)**:
  - **Given**: a node with Added 1/50B, Modified 1/200B, Deleted 1/900B
    (change_bytes = 1150, change_files = 3) built as a literal
    `dcfh.Node{Stats: dcfh.Stats{Added:1, AddedBytes:50, Modified:1,
    ModifiedBytes:200, Deleted:1, DeletedBytes:900}}` (existing `node()`/
    `nodeBytes()` helpers set counts XOR bytes, so a literal is needed).
  - **When**: `columnText(n, sortChangeBytes)`.
  - **Then**: equals `dcfh.FormatHumanSize(1150)`.
- **TC-2 (name → change volume, NOT 0 — locks design F1)**:
  - **Given**: the TC-1 node.
  - **When**: `columnText(n, sortName)`.
  - **Then**: equals `dcfh.FormatHumanSize(1150)`; explicitly assert it is
    `!= dcfh.FormatHumanSize(0)` so a regression to `metric(n, sortName)==0`
    fails loudly.
- **TC-3 (count keys)**:
  - **Given**: the TC-1 node.
  - **When**: `columnText` with `sortChangeFiles` / `sortAdded` /
    `sortModified` / `sortDeleted`.
  - **Then**: `"3"`, `"1"`, `"1"`, `"1"` respectively (decimal, no humanising).
- **TC-4 (bytes ≠ change-bytes discriminator)**:
  - **Given**: a deleted-only node (Deleted 1/900B, `Stats.Bytes` 0).
  - **When**: `columnText(n, sortChangeBytes)`.
  - **Then**: equals `dcfh.FormatHumanSize(900)`, proving the column is NOT
    reading `Stats.Bytes` (which would be `0 B`).

### Functional — Integration (`render_test.go`, SimulationScreen)
- **TC-5 (default change_bytes column wired up)**:
  - **Given**: `treeForSim` (the `docs` dir aggregates `old.md` deleted,
    `Stats.Bytes` 0 / change_bytes 900) on a wide sim screen.
  - **When**: draw with the default `change_bytes` sort; flatten via
    `screenText`.
  - **Then**: the `docs` row contains `dcfh.FormatHumanSize(900)` and does NOT
    contain `"0 B"` on that row. (`docs` is the discriminating node — see
    d-plan fixture note; `src` collides at 250 == 250 and must not be used.)
- **TC-6 (toggle to a count key swaps the column)**:
  - **Given**: the TC-5 screen.
  - **When**: press `f` (change_files), redraw.
  - **Then**: the `docs` row now shows the change_files count (`"1"`), matching
    the `sort:change_files` header.

### Non-Functional
- **No-regression**: `go test ./cmd/... ./pkg/...` green — existing
  `TestDefaultSortAndKeyToggles`, `TestNavigation`, `TestWidthGating`,
  `TestStatsPaneByteAnnotations`, `TestLiveResortPreservesSelectionNoReRead`
  unaffected (the stats pane still reads `Stats.Bytes` for its `Size:` line).
- **Static gate**: `golangci-lint run ./...` clean — the new `strconv` import
  and `strconv.FormatInt` introduce no gosec findings.
- **Manual smoke**: `./dcfh status --interactive-tree` on a churned dir;
  confirm a directory's number is its change volume, and `f`/`a`/`m`/`d`/`c`
  swap the column to match the header label/unit.

## Test Environment
### Setup
- Standard Go toolchain (Go 1.25.0); `tcell` SimulationScreen (already a test
  dependency) — no TTY, no real terminal, no filesystem fixtures.
- All fixtures are in-memory `dcfh.Node`/`dcfh.Tree` literals; no index files,
  no test database (none involved — pure render-layer).

### Automation
- `go test ./cmd/dcfh/internal/tui/...` for the targeted suite;
  `go test ./cmd/... ./pkg/...` for regression.
- Runs under the existing pre-commit gate (golangci-lint staged `--new`, race).

## Validation Criteria
- [ ] TC-1…TC-4 unit assertions pass (every sort key + name remap).
- [ ] TC-5/TC-6 integration assertions pass (wire-up + toggle).
- [ ] `docs` row asserts on `FormatHumanSize(900)`, never `0 B` (regression
      guard for the exact reported bug).
- [ ] Full `cmd`+`pkg` suites green; golangci-lint clean.
- [ ] Manual smoke confirms column tracks header across key toggles.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 13
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1…TC-6 all PASS (see g-testing-exec.md). `columnText` 100% covered;
package 82.1%. Full `cmd`+`pkg` regression and `-race` green; golangci-lint
0 issues; govulncheck 0 called. Manual TTY smoke deferred to user review (the
SimulationScreen integration test covers the same path headlessly).

## Lessons Learned
Asserting the rendered per-row *value* (not just header/legend text) is the
guard task 12 lacked — the gap that let the bug ship.
