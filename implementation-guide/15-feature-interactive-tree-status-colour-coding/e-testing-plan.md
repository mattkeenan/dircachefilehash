# interactive-tree status colour coding - Testing Plan
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Verify the status glyph + colour + bold encoding against FR1–FR8 / AC1–AC11,
using a pure-resolver table test plus tcell `SimulationScreen` render tests, with
no regression to the existing viewer behaviour.

## Test Strategy
### Test Levels
- **Unit**: `nodeStyle` is pure — table-driven over all 8 present-sets. Carries
  the bulk of FR1–FR5 coverage with no screen needed.
- **Integration (render)**: `SimulationScreen` + `GetContents()` cell inspection
  (existing pattern in `render_test.go`) for glyph placement, per-cell colour/bold,
  selection compose, stats-pane colour, and narrow-width degradation.
- **Regression**: the existing `tui` suite must stay green (FR7).

### Coverage Targets
- `nodeStyle`: 100% — every switch arm (∅, A, M, D, A|M, A|D, M|D, A|M|D).
- `drawRow` glyph path + stats-pane colour: exercised via simulation.
- Style assertions read `cell.Style.Decompose() → (fg, bg, attr)` and check
  `fg`, `attr&AttrBold`, `attr&AttrReverse`.

### Fixtures
- Reuse `treeForSim`: `docs/old.md`(deleted), `src/main.go`(modified),
  `src/new.go`(added), `readme.md`(unchanged). This already yields a deleted-only
  dir (`docs/`→red), a mixed add+mod dir (`src/`→cyan), an all-three root
  (→white), and an unchanged leaf — covering AC9/AC10 and three leaf cases.
- The pure table supplies the combinations `treeForSim` lacks: M|D (magenta),
  A|D (yellow), and single-category directories.

## Test Cases
### Functional Test Cases

- **TC-U1 (AC1–AC4, FR1–FR5) — `nodeStyle` table**
  - **Given**: a `*dcfh.Node` whose `Stats.{Added,Modified,Deleted}` encode each of
    the 8 present-sets.
  - **When**: `nodeStyle(n)` is called.
  - **Then**: returns the expected `(glyph, fg, bold)` —
    ∅→`(' ', default, false)`; A→`('+',Green,true)`; M→`('~',Blue,true)`;
    D→`('-',Red,true)`; A|M→`('*',Aqua,true)`; A|D→`('*',Yellow,true)`;
    M|D→`('*',Fuchsia,true)`; A|M|D→`('*',White,true)`. Also assert every returned
    glyph ∈ `{'+','~','-','*',' '}` (never `rune(0)` — safety invariant) and
    `bold == (set ≠ ∅)`.

- **TC-S1 (AC1/AC3, FR1/FR3) — glyph placement**
  - **Given**: `treeForSim` rendered wide, directories expanded.
  - **When**: the screen is flattened (`rowLine`).
  - **Then**: `new.go` line contains `+`, `main.go` `~`, `old.md` `-`; `docs/`
    line contains `-`, `src/` contains `*`, the root row contains `*`; `readme.md`
    line shows no status glyph in the slot.

- **TC-S2 (AC1/AC4, FR1) — leaf colour + bold (cell style)**
  - **Given**: the rendered tree.
  - **When**: decompose the style of a cell on each leaf row.
  - **Then**: `new.go`→Green+Bold, `main.go`→Blue+Bold, `old.md`→Red+Bold,
    `readme.md`→default fg, **not** bold.

- **TC-S3 (AC2, FR2) — directory blend colour**
  - **Given**: the rendered tree.
  - **When**: decompose the style of the `docs/`, `src/`, and root rows.
  - **Then**: `docs/`→Red (deleted-only), `src/`→Aqua (add+mod), root→White
    (all-three); all Bold.

- **TC-S4 (AC8) — empty/unchanged boundary**
  - **Given**: `readme.md` (all-unchanged).
  - **Then**: default fg, no glyph, non-bold.

- **TC-S5 (AC9) — deleted-only directory is not "unchanged"**
  - **Given**: `docs/` (only a deleted child; `Stats.Files` excludes it).
  - **Then**: renders Red / `-` / Bold — proving the resolver keys on
    `Added+Modified+Deleted`, not `Files`.

- **TC-S6 (AC10) — all-three white distinguishable from unchanged**
  - **Given**: root (white,`*`,bold) and `readme.md` (default,` `,non-bold).
  - **Then**: their decomposed styles differ in **both** bold and glyph.

- **TC-S7 (AC6, FR6) — selection composes over category style**
  - **Given**: a changed row selected (`m.sel`).
  - **When**: decompose that row's cells.
  - **Then**: `attr&AttrReverse != 0` **and** the status glyph is still present on
    the row.

- **TC-S8 (AC11, FR8) — stats-pane modified is blue**
  - **Given**: a wide screen with `src` (or `main.go`) selected.
  - **When**: decompose the `Modified:` legend-line cell.
  - **Then**: fg == Blue (matching a modified leaf), not Yellow.

- **TC-S9 (AC7, FR7) — narrow-width degradation**
  - **Given**: a narrow width / deep indent where the glyph's +2 columns squeeze
    the right-aligned value.
  - **When**: the row is drawn.
  - **Then**: no panic; the label still renders and the size value is dropped (not
    overwritten) — the `colX > x+1` guard holds.

- **TC-S10 (AC12, FR9) — stats-pane legend**
  - **Given**: a wide screen with any node selected.
  - **When**: the stats pane is rendered and flattened.
  - **Then**: it shows `+ Added` (green), `~ Modified` (blue), `- Deleted` (red)
    lines and a `* mixed` note; the category-line colons remain aligned with the
    other stat lines.

### Non-Functional Test Cases
- **Usability/accessibility (FR5)**: TC-S1 + TC-U1 jointly show status is carried
  by a distinct glyph per category independent of colour (glyph differs even when
  two cases would be hard to tell apart by colour for a CVD user).
- **Reliability (NFR5)**: TC-S5/TC-S9 + existing `TestRun_EmptyTreeNoOp` cover
  deleted-only, narrow, and empty-changeset paths with no panic.
- **Performance (NFR1)**: no dedicated test — the change is per-row pure
  computation over already-aggregated stats; covered by existing suite timing.
- **Security (NFR4)**: none — TUI-only; glyph alphabet is a closed set (asserted
  in TC-U1), preserving the `drawText` sanitised-string contract.

### Regression (FR7)
- Existing tests must stay green: `TestNavigation`, `TestWidthGating`,
  `TestDefaultSortAndKeyToggles`, `TestColumnTracksActiveSortMetric`,
  `TestStatsPaneByteAnnotations`, `TestLiveResortPreservesSelectionNoReRead`,
  `TestRunScreen_*`. Update only assertions that pinned a directory row as
  unstyled (now coloured/bold by design) — update, never disable.

## Test Environment
### Setup Requirements
- Go test only; `tcell.NewSimulationScreen("UTF-8")` (no real TTY).
- New helper(s): a `cellStyleAt(sim, x, y)` / `rowStyle(sim, label)` decompose
  helper alongside the existing `screenText`/`rowLine`/`findRow`.

### Automation
- `go test ./cmd/dcfh/internal/tui/...` (and `./...` for regression); runs under
  the existing `.githooks/pre-commit` race gate.

## Validation Criteria
- [ ] TC-U1 passes for all 8 present-sets (100% switch coverage).
- [ ] TC-S1–TC-S9 pass.
- [ ] Full `go test ./...` green; golangci-lint clean on the staged change.
- [ ] No existing test disabled; any dir-row style assertion updated, not removed.

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
TC-U1 + TC-S1…S10 all PASS; `nodeStyle`/`drawRow` at 100% coverage; full
regression + `-race` green. Full results in g-testing-exec.md.

## Lessons Learned
TC-S3/S6 assumed an all-three "root row" that is never rendered (the implicit
root has label ""). Resolution: all-three white is covered by the pure table
(`TestNodeStyle`) plus a dedicated `treeAllThreeDir` fixture. Lesson: when a
test case names a specific rendered row, confirm it appears in `m.rows` during
the testing-plan phase, not at test-writing time.
