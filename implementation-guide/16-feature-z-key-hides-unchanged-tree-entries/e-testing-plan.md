# z key hides unchanged tree entries - Testing Plan
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Verify the `z` hide-unchanged toggle against FR1–FR8 / AC1–AC8 (+AC4b) with a
pure-predicate table test plus tcell `SimulationScreen` tests, with no regression
to the existing viewer behaviour.

## Test Strategy
### Test Levels
- **Unit**: `hasChange` is pure — a small table over the change-category
  combinations (carries FR2 and the deletion-only correctness point with no screen).
- **Integration (render/event)**: `SimulationScreen` + the existing
  `screenText`/`rowLine`/`findRow`/`rowYOf` helpers, driving `handleKey` with a
  `'z'` rune event (the real input path) to assert filtered `m.rows`, footer text,
  selection state, and the empty-state body.
- **Regression**: the full existing `tui` suite must stay green (FR-none-broken).

### Test Coverage Targets
- `hasChange`: 100% (changed and unchanged arms).
- The new `rebuildRows` hide branch and the `z` `handleRune` case: exercised by
  the simulation/event tests below.
- No drop in package coverage; no existing test disabled or weakened.

### Fixtures
- **Reuse `treeForSim`**: `docs/old.md`(deleted), `src/{main.go(mod),new.go(add)}`,
  `readme.md`(unchanged). Gives an unchanged leaf (`readme.md` → hidden), a
  deletion-only **collapsed** dir (`docs/` → stays, covering AC2+AC4b in one), and
  a changed dir (`src/`).
- **New `treeWithUnchangedDir()`**: root with `src/`(changed), a wholly-unchanged
  `vendor/lib.go` dir, and `readme.md`(unchanged) — the only fixture that exercises
  hiding a *wholly-unchanged directory* (AC4).
- **New `treeAllUnchanged()`**: root with only unchanged leaves — for the
  all-hidden empty-state (FR8/AC8).

## Test Cases
### Functional Test Cases

- **TC-U1 (AC2, FR2) — `hasChange` table**
  - **Given**: a `*dcfh.Node` with `Stats.{Added,Modified,Deleted}` set to each of:
    all-zero, added-only, modified-only, deleted-only, and a mixed combination.
  - **When**: `hasChange(n)` is called.
  - **Then**: false only for all-zero; true for every set with any of
    Added/Modified/Deleted > 0 — including **deleted-only** (the regression-guard
    case that a `Stats.Files` predicate would get wrong).

- **TC-1 (AC1, FR1) — toggle round-trip**
  - **Given**: a freshly opened viewer over `treeForSim` (hide off).
  - **When**: `handleKey('z')` once, then again.
  - **Then**: after the first `z`, `readme.md` is absent from `m.rows`; after the
    second `z`, `readme.md` is present again (row count returns to the original).

- **TC-2 (AC3, FR3) — unchanged leaf hidden**
  - **Given**: `treeForSim`, all dirs expanded, hide on.
  - **When**: rows are flattened.
  - **Then**: `readme.md` (unchanged leaf) is absent; `new.go`/`main.go`/`old.md`
    (changed leaves) are present.

- **TC-3 (AC2/AC4b, FR2/FR4) — deletion-only collapsed dir stays**
  - **Given**: `treeForSim`, `docs/` collapsed (default), hide on.
  - **When**: rows are flattened.
  - **Then**: `docs/` is still present (its only change is a deleted child;
    visibility derives from aggregated `Stats`, and it is not force-expanded —
    `m.expanded[docs]` stays false). Guards the predicate against `Stats.Files`.

- **TC-4 (AC4, FR4) — wholly-unchanged directory hidden**
  - **Given**: `treeWithUnchangedDir()`, hide on.
  - **When**: rows are flattened.
  - **Then**: the `vendor/` dir (and its unchanged child) is absent; `src/`
    (changed) is present.

- **TC-5 (AC6, FR6) — composition with sort + reverse**
  - **Given**: `treeForSim`, hide on, all expanded.
  - **When**: switch sort to `a` (added), then press `r`.
  - **Then**: unchanged entries stay hidden across both rebuilds; the visible
    changed rows reorder by the added metric and reverse — confirming the filter
    re-applies on every `rebuildRows` (persistent model flag).

- **TC-6 (AC5, FR5) — selection preserved (changed node)**
  - **Given**: `treeForSim`, select a changed node (`src/`).
  - **When**: `handleKey('z')`.
  - **Then**: `m.current()` is still `src/`.

- **TC-7 (AC5, FR5) — selection clamped (hidden node)**
  - **Given**: `treeForSim`, select the unchanged `readme.md`.
  - **When**: `handleKey('z')` (which hides `readme.md`).
  - **Then**: no panic; `m.sel` is a valid in-range index (`0 <= m.sel <
    len(m.rows)`) — asserts the clamp branch, not a specific row (per design note).

- **TC-8 (AC7, FR7) — footer advertises `z`**
  - **Given**: any rendered viewer.
  - **When**: the footer line is flattened.
  - **Then**: it contains a `z` hide hint (e.g. `z hide`).

- **TC-9 (AC8, FR8) — all-hidden empty-state + safe navigation**
  - **Given**: `treeAllUnchanged()`, hide on.
  - **When**: the screen is drawn, then `moveDown()`/`expand()` are invoked while
    `m.rows` is empty.
  - **Then**: the body shows `(no changes to display)`; the header still shows the
    root file count/size; the stats pane shows `(nothing selected)`; `m.current()`
    is nil; no panic. A second `z` restores the full tree.

### Non-Functional Test Cases
- **Usability (NFR2)**: TC-8 confirms the binding is discoverable; TC-1 confirms it
  is reversible. TC-3/TC-4 confirm the path to a changed leaf is never broken.
- **Reliability (NFR5)**: TC-7 (hidden selection) and TC-9 (empty + navigation)
  cover the no-panic / clamp paths; existing `TestRun_EmptyTreeNoOp` still holds.
- **Performance (NFR1)**: no dedicated test — per-row pure predicate over
  already-aggregated stats; covered by existing suite timing.
- **Security (NFR4)**: none — TUI-only; the filter renders no new node-derived
  strings, so the `drawText` sanitised-string contract is unchanged.

### Regression
- Existing tests must stay green unchanged: `TestNavigation`, `TestWidthGating`,
  `TestDefaultSortAndKeyToggles`, `TestColumnTracksActiveSortMetric`,
  `TestStatsPaneByteAnnotations`, `TestLiveResortPreservesSelectionNoReRead`,
  `TestRunScreen_*`, `TestRun_EmptyTreeNoOp`, and the task-15 `TestNodeStyle` /
  `TestRender*` set. The hide default is OFF, so no existing test (all run hide-off)
  changes behaviour; none should need edits.

## Test Environment
### Setup Requirements
- Go test only; `tcell.NewSimulationScreen("UTF-8")` (no real TTY), via the
  existing `newSimModel` helper and the two new fixtures.
- New event-injection: drive the toggle with
  `m.handleKey(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone))` — the same
  pattern existing sort/reverse tests use.

### Automation
- `go test ./cmd/dcfh/internal/tui/...` (and `./...` for regression); runs under
  the `.githooks/pre-commit` `-race` gate.

## Validation Criteria
- [ ] TC-U1 + TC-1…TC-9 pass.
- [ ] `hasChange` at 100%; new filter branch + `z` case exercised.
- [ ] Full `go test ./...` green; golangci-lint clean on the staged change;
      pre-commit `-race` green.
- [ ] No existing test disabled or weakened.

## Decomposition Check
- [ ] Time >1wk? No   - [ ] People >2? No   - [ ] Complexity 3+? No
- [ ] Risk isolation? No   - [ ] Independence? No

**Result**: 0 signals — no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-U1 + TC-1…TC-9 all implemented and PASS; both new fixtures
(`treeWithUnchangedDir`, `treeAllUnchanged`) added. `hasChange` 100%,
`rebuildRows` hide branch 100%; package 88.1%. Full regression + `-race` green.
See g-testing-exec.md.

## Lessons Learned
Driving the toggle through the real key path (`handleKey('z')`) rather than
setting the flag directly exercised the `handleRune` `z` case end-to-end, not just
the filter. See j-retrospective.md.
