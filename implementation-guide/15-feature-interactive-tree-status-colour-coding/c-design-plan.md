# interactive-tree status colour coding - Design
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Define the rendering design that satisfies the status-encoding requirements
(FR1–FR8) with a single pure resolver and a minimal `drawRow` change.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### Decision 1 — One pure resolver keyed on `Stats` counts, for leaves *and* directories
- **Decision**: Replace the existing `categoryStyle(n) tcell.Style` (`render.go:276`,
  which special-cases `IsDir` → default) with a single pure function
  `nodeStyle(n *dcfh.Node) (glyph rune, style tcell.Style)`. It derives a
  **present-category set** from `n.Stats.{Added,Modified,Deleted} > 0` — the
  *same* logic for a leaf (set size ≤ 1) and a directory (set size 0–3).
- **Rationale**: A leaf's `Stats` already carries its single category
  (`leafStats`, `treeview.go`), and a directory's `Stats` is the aggregate
  (`aggregate()`), so one count-driven rule covers both with no `IsDir` branch.
  Keying on `Added+Modified+Deleted` (not `Files`, which **excludes** deleted —
  see `treeview.go` `Files == Added+Modified+Unchanged`) is what makes a
  deleted-only directory correctly render as changed (AC9).
- **Trade-offs**: Removes the `IsDir → default` shortcut, so directories now get
  colour/glyph (the whole point). `categoryStyle`'s sole caller is `drawRow`
  (`render.go:175`), so the replacement is localised.

### Decision 2 — Glyph as a fixed-width field inside the existing `left` string
- **Decision**: In `drawRow`, insert the glyph (+ one trailing space) immediately
  after the expand/collapse marker:
  `left = fmt.Sprintf("%*s%s%c %s", indent, "", marker, glyph, label)`.
  Unchanged nodes use `glyph == ' '`, preserving the 2-column slot.
- **Rationale**: Directories need *both* the existing `▾`/`▸` marker and a status
  glyph, so the glyph must be its own field, not an overload of the marker. Fixed
  width keeps sibling labels aligned whether or not a row is changed (NFR2).
- **Trade-offs**: Adds 2 columns to every row. Rejected alternative: a global
  status gutter in column 0 — cleaner scanning but tangles with indent/selection
  background painting for no real gain.
- **Safety invariant**: `nodeStyle` MUST return `' '` (U+0020) — never `rune(0)` —
  for the unchanged case, and the glyph alphabet is the fixed safe set
  `{'+','~','-','*',' '}`. `%c` with a zero rune would emit a NUL past
  `drawText`'s sanitised-string contract (`render.go:249-252`); the unit table
  asserts the alphabet so a future edit cannot leak a control rune.
- **Narrow-width degradation (FR7 interaction)**: the +2 columns raise the
  left-cursor `x`, so the existing `colX > x+1` guard (`render.go:204`) can now
  drop the right-aligned size value at narrow widths / deep indents where it
  previously fit. Intended behaviour: the **label truncates first and the value
  drops** (no overwrite/panic — the guard already prevents corruption). A
  narrow-width test pins this (deferred to the testing plan). Alignment reasoning
  uses `drawText`'s returned *column* count, not `len()` (the marker runes
  `▾`/`▸` are multi-byte).

### Decision 3 — Reuse the existing selection compose; change `styleModified` in place
- **Decision**: Leave `drawRow`'s `if selected { base = base.Reverse(true) }`
  (`render.go:176-177`) untouched — it now composes over the coloured/bold base
  for free (FR6). Change the package var `styleModified` from `ColorYellow` to
  `ColorBlue` (`render.go:270`); this updates the stats-pane legend
  (`render.go:235`) and the single-category resolver path in lockstep (FR8).
- **Rationale**: The additive model needs Blue for the modified channel so
  add+mod=cyan and mod+del=magenta hold; yellow is freed for add+del. Reusing
  `Reverse` avoids new selection logic (Simplicity, Reversibility).
- **Trade-offs**: A deliberate, visible change to the shipped modified colour
  (yellow→blue) everywhere it appears — documented in requirements Constraints.
- **Second visible behaviour change (intended, per FR4)**: today changed *leaves*
  are coloured but **not** bold; the `set ≠ ∅ → bold` rule now bolds changed
  files as well as directories. This is the requested "change > 0 → bold"
  behaviour, called out here so it isn't mistaken for a regression.

## System Design
### Component Overview
- **`nodeStyle(n) (rune, tcell.Style)`** (`cmd/dcfh/internal/tui/render.go`):
  pure resolver — present-category set → (glyph, foreground colour, bold). No I/O.
- **`drawRow`** (same file): calls `nodeStyle`, composes the glyph into `left`,
  applies `Reverse` when selected (unchanged), draws.
- **`styleAdded/styleModified/styleDeleted`** package vars: single-category
  colours, reused by both `nodeStyle` and the stats pane; `styleModified` → blue.
- **`drawStats`** (same file): glyph-prefixed colour-matched category lines + a
  `* mixed` note — the on-screen legend (Decision 4).

### Data Flow
1. `drawTree` iterates visible rows → `drawRow(row)` (`render.go:166-170`).
2. `drawRow` calls `nodeStyle(row.node)` → `(glyph, base)`.
3. `nodeStyle` reads `node.Stats.{Added,Modified,Deleted}` → 3-bit present-set →
   colour + bold + glyph.
4. `drawRow` builds `left = indent + marker + glyph + " " + label`, applies
   `base` (or `base.Reverse(true)` if selected), draws left + right-aligned value.

## Interface Design
### Resolver contract
```
// nodeStyle maps a node's present change-category set to its status glyph and
// base style (foreground colour + bold). Pure; identical for leaf and dir
// because it keys on Stats counts. glyph is ' ' (space) when unchanged.
func nodeStyle(n *dcfh.Node) (glyph rune, style tcell.Style)
```

### Present-set → (colour, glyph) table
Bits: A=added, M=modified, D=deleted (present iff that `Stats` count > 0).
Bold = (set ≠ ∅).

```
set        colour    glyph
∅          default   ' '      (non-bold)
A          green     '+'
M          blue      '~'
D          red       '-'
A,M        cyan      '*'
A,D        yellow    '*'
M,D        magenta   '*'
A,M,D      white     '*'
```
Leaves can only reach ∅ or a singleton (a file has one category), so `*` is a
directory-only outcome in practice; the table needs no leaf/dir branch.

Implement as a **single `switch` on the 3-bit present-set**, returning glyph and
colour together, so the two can never drift apart (one source of truth).

### Decision 4 — Legend in the stats pane (no layout change) [FR9, approved]
- **Decision**: In `drawStats`, prefix the `Added/Modified/Deleted` legend lines
  with their glyph (`+`/`~`/`-`, each already in its category colour) and add a
  `* = mixed (directory)` line. Keep colon-alignment by giving every stats line a
  2-column leading slot (glyph for the three category lines, spaces for the rest).
  No change to the header/footer row budget.
- **Rationale**: the stats pane already renders these three lines in their
  category colours — adding the glyph turns it into the legend with one extra
  static line, avoiding footer/row-budget surgery (Simplicity, Reversibility).
- **Trade-off**: the legend is hidden on narrow screens where the stats pane is
  already suppressed; acceptable because the tree glyphs remain and widening
  restores it. The blend colours (cyan/magenta/yellow/white) have no per-colour
  legend entry — the `*` note covers "mixed"; the specific blend is intentionally
  a coarse hint (de-collapse to see detail).

## Constraints
- tcell named colours only (`ColorGreen/Blue/Red/Cyan/Yellow/Magenta/White`);
  themes may remap. No truecolour.
- No change to `pkg/treeview.go` — resolver reads existing `Stats`/`Node.Cat`.
- Glyph field is fixed-width so alignment with marker and the size column holds.
- TUI-only; no index/scan/status changes.

## Decomposition Check
- [ ] **Time**: >1 week? No
- [ ] **People**: >2 people? No
- [ ] **Complexity**: 3+ distinct concerns? No — one resolver + one row tweak
- [ ] **Risk**: high-risk components needing isolation? No
- [ ] **Independence**: separable parts? No

**Result**: 0 signals — no decomposition.

## Validation
- [ ] Design review completed (plan-review subagents, Step 8)
- [ ] Resolver is pure and table-testable (Testability priority satisfied)
- [ ] Integration points verified: `categoryStyle`'s only caller is `drawRow`;
      `styleModified` feeds both the stats pane and the resolver

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All four design decisions implemented verbatim: one pure `nodeStyle` resolver
(Decision 1), the fixed-width glyph slot in `left` (Decision 2), in-place
`styleModified`→blue reusing the selection compose (Decision 3), and the
stats-pane legend (Decision 4). The safety invariant (glyph never `rune(0)`) is
pinned by `TestNodeStyle`.

## Lessons Learned
Decisions 1–3 survived implementation unchanged — a sign the design was at the
right granularity. The only design-vs-reality gap was downstream in the test
plan (root row not rendered), not in the design itself.
