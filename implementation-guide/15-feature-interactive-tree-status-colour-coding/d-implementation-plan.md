# interactive-tree status colour coding - Implementation Plan
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Implement the status glyph + additive-RGB colour + bold encoding in the
interactive-tree viewer per the approved design, confined to `render.go`.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes
- `cmd/dcfh/internal/tui/render.go`:
  - `styleModified`: `ColorYellow` → `ColorBlue` (FR8; updates stats pane + resolver).
  - Replace `categoryStyle(n) tcell.Style` with `nodeStyle(n) (rune, tcell.Style)`
    — one `switch` on the 3-bit present-set → (glyph, colour, bold).
  - `drawRow`: call `nodeStyle`, insert the glyph slot into `left`.
  - `drawStats`: glyph-prefix the Added/Modified/Deleted legend lines + add a
    `* mixed (directory)` line (FR9 legend); 2-col leading slot on every stats
    line to keep colon-alignment.

### Supporting Changes
- `cmd/dcfh/internal/tui/render_test.go`: table-driven `nodeStyle` tests + a
  glyph-rendering / narrow-width simulation test (detailed in `e-testing-plan.md`).

## Implementation Steps
### Step 1: Patterns first
- [ ] Re-read `drawRow` (`render.go:174-207`), `categoryStyle` (276), the `style*`
      vars (269-271), and the `render_test.go` `SimulationScreen`/`GetContents`
      pattern + `treeForSim` fixture (the deleted-only `docs/` case = AC9 fixture).

### Step 2: Colour var (FR8)
- [ ] Change `styleModified` foreground from `tcell.ColorYellow` to `tcell.ColorBlue`.

### Step 3: Resolver (FR1–FR5, Decision 1)
- [ ] Replace `categoryStyle` with `nodeStyle` keyed on `n.Stats.{Added,Modified,
      Deleted} > 0`. Unchanged → `(' ', tcell.StyleDefault)`. Else
      `Foreground(colour).Bold(true)` with glyph per the table. Glyph alphabet is
      exactly `{'+','~','-','*',' '}` — never `rune(0)` (safety invariant).

### Step 4: Row rendering (FR6, Decision 2)
- [ ] In `drawRow`, `glyph, base := nodeStyle(row.node)`; build
      `left = fmt.Sprintf("%*s%s%c %s", indent, "", marker, glyph, label)`.
      Leave the `if selected { base = base.Reverse(true) }` compose unchanged.

### Step 5: Build & regression
- [ ] `make build`; run `go test ./cmd/... ./pkg/...`. No current test pins the
      modified colour (verify: `grep -rn ColorYellow cmd/dcfh/internal/tui`), so
      the only style-baseline work is the new tests in Step 6. If any existing
      simulation test asserts a *directory* row is unstyled, update it — dirs are
      now coloured/bold by design (Decision 1). Update, never disable.

### Step 5b: Stats-pane legend (FR9, Decision 4)
- [ ] In `drawStats`, give every line a 2-col leading slot: `+ `/`~ `/`- ` on the
      Added/Modified/Deleted lines (keeping their `styleAdded/Modified/Deleted`),
      two spaces on the others, so the colons stay aligned. Append a
      `{"* mixed (directory)", tcell.StyleDefault.Dim(true)}` line.

### Step 6: Tests (per e-testing-plan)
- [ ] Add `nodeStyle` table tests + simulation tests (glyph presence, selected row,
      narrow-width value-drop, stats-pane modified=blue, legend present).

## Code Changes
### Before (`render.go:268-290`)
```go
var (
	styleAdded    = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	styleModified = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	styleDeleted  = tcell.StyleDefault.Foreground(tcell.ColorRed)
)

// categoryStyle colours a node by its change category. Directories use
// the default style (their composition shows in the stats pane).
func categoryStyle(n *dcfh.Node) tcell.Style {
	if n.IsDir {
		return tcell.StyleDefault
	}
	switch n.Cat {
	case dcfh.Added:
		return styleAdded
	case dcfh.Modified:
		return styleModified
	case dcfh.Deleted:
		return styleDeleted
	default:
		return tcell.StyleDefault
	}
}
```

### After
```go
var (
	styleAdded    = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	styleModified = tcell.StyleDefault.Foreground(tcell.ColorBlue)
	styleDeleted  = tcell.StyleDefault.Foreground(tcell.ColorRed)
)

// nodeStyle maps a node's present change-category set (Stats counts > 0) to its
// status glyph and base style (foreground colour + bold). Pure; identical for
// leaf and dir. Unchanged → (' ', default). Glyph is always one of
// '+','~','-','*',' ' — never a control rune (drawText sanitised-string contract).
// Colours are ANSI-palette names (0–15) so terminal themes remap them.
func nodeStyle(n *dcfh.Node) (rune, tcell.Style) {
	const (
		bA = 1 << iota // added
		bM             // modified
		bD             // deleted
	)
	set := 0
	if n.Stats.Added > 0 {
		set |= bA
	}
	if n.Stats.Modified > 0 {
		set |= bM
	}
	if n.Stats.Deleted > 0 {
		set |= bD
	}
	var (
		glyph  rune
		colour tcell.Color
	)
	switch set {
	case 0:
		return ' ', tcell.StyleDefault
	case bA:
		glyph, colour = '+', tcell.ColorGreen
	case bM:
		glyph, colour = '~', tcell.ColorBlue
	case bD:
		glyph, colour = '-', tcell.ColorRed
	case bA | bM:
		glyph, colour = '*', tcell.ColorAqua // cyan
	case bM | bD:
		glyph, colour = '*', tcell.ColorFuchsia // magenta
	case bA | bD:
		glyph, colour = '*', tcell.ColorYellow
	default: // bA | bM | bD
		glyph, colour = '*', tcell.ColorWhite
	}
	return glyph, tcell.StyleDefault.Foreground(colour).Bold(true)
}
```

### `drawRow` change (`render.go:174-198`)
```go
// before:
base := categoryStyle(row.node)
...
left := fmt.Sprintf("%*s%s%s", indent, "", marker, label)

// after:
glyph, base := nodeStyle(row.node)
...
left := fmt.Sprintf("%*s%s%c %s", indent, "", marker, glyph, label)
```
(The `if selected { base = base.Reverse(true) }` line and the right-aligned
`colVal` logic are unchanged; the existing `colX > x+1` guard already prevents
overwrite when the +2 columns squeeze the value — see design narrow-width note.)

## Test Coverage
**See e-testing-plan.md for the complete test plan** — table tests for all
4 leaf + 7 directory combinations + unchanged + bold; simulation tests for glyph
presence, selected-row compose, narrow-width value-drop, and stats-pane blue.

## Validation Criteria
**See e-testing-plan.md.** Plus: `make build` clean, `go test ./...` green
(updating any baseline test that pinned modified=yellow), golangci-lint clean on
the staged change.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
No deferral expected — single file, two functions, one var.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All steps (2, 3, 4, 5, 5b, 6) executed as planned with no deviation in the
production code. The "Before/After" code blocks matched the committed change
exactly. Recorded in f-implementation-exec.md.

## Lessons Learned
Writing the exact "After" code into the plan made implementation a transcription
step and gave the security reviewer a precise target. The unused-var compiler
nudge (declaring `glyph` before wiring it into the format string) was the only
intra-step friction, resolved immediately.
