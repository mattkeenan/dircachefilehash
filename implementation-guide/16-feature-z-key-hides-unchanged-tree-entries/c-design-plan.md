# z key hides unchanged tree entries - Design
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Design the `z` hide-unchanged toggle as a pure render-layer filter inside the
existing `rebuildRows` flatten, mirroring the established `r`-key toggle.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### Decision 1 — Model state: one persistent boolean field
- **Decision**: Add `hideUnchanged bool` to `model` (`render.go`). Default false
  (zero value) on `newModel`, so launch behaviour is unchanged.
- **Rationale**: The mode must survive sort/reverse/expand rebuilds (FR1/FR6).
  A model field consulted by `rebuildRows` is the same shape as `sortKey`,
  `reverse`, and `expanded` — no new pattern.
- **Trade-offs**: Persistent state vs a one-shot transform; persistent is
  required by FR6 and is the cheaper, simpler choice. No drawback at this size.

### Decision 2 — Filter at the single `rebuildRows` choke point
- **Decision**: Apply the hide predicate inside the existing `walk` closure in
  `rebuildRows`, per child, *before* the append and recursion:
  skip (don't append, don't recurse into) a child `c` when
  `m.hideUnchanged && !hasChange(c)`.
- **Rationale**: `rebuildRows` is the one function every view-changing key
  (`r`, sort keys, expand/collapse) already calls, so filtering there makes the
  mode compose with all of them for free (FR6) with no second code path
  (NFR3). Filtering *before* recursion prunes a wholly-unchanged directory's
  whole subtree in one step and never force-expands a collapsed directory
  (FR4/AC4b) — visibility of a directory derives only from its own aggregated
  `Stats`, which is already subtree-wide.
- **Trade-offs**: Filtering inside `walk` (vs a post-filter on `m.rows`) is the
  only option that gets the directory-pruning and no-orphan-path behaviour
  without an ancestor walk. A post-pass over the flat `m.rows` would have to
  re-derive parent/child relationships — strictly worse. Chosen: in-`walk`.

### Decision 3 — `hasChange` predicate: pure, keyed on the change-sum
- **Decision**: Add a pure free function
  `hasChange(n *dcfh.Node) bool { return n.Stats.Added+n.Stats.Modified+n.Stats.Deleted > 0 }`
  (in `render.go`, next to `nodeStyle`).
- **Rationale**: Keying on `Added+Modified+Deleted` (never `Stats.Files`, which
  excludes deletions) is the one non-obvious correctness point (FR2) — a
  deletion-only node must count as changed. A named pure predicate is unit
  testable without a screen (Testability) and reads at the call site. It is the
  exact complement of `nodeStyle`'s "unchanged" arm (`set == 0` → `' '`), so the
  glyph and the filter agree by construction; a comment notes this relationship
  rather than coupling the two (only two call sites — no shared abstraction
  warranted, Rule of Three).
- **Trade-offs**: A tiny duplicate of the present-set test in `nodeStyle` vs
  coupling the two functions. Duplication of a one-line predicate is clearer
  than threading a shared helper through `nodeStyle`'s glyph/colour switch.

### Decision 4 — Key binding mirrors the `r` toggle exactly
- **Decision**: Add `case 'z':` to `handleRune` (`tui.go`) using the existing
  reverse-toggle shape: `cur := m.current(); m.hideUnchanged = !m.hideUnchanged;
  m.rebuildRows(); m.selectNode(cur)`.
- **Rationale**: Consistency — the `r` case is the precedent for a
  view-mutating toggle that preserves selection. `selectNode(cur)` keeps the
  selected node if still visible and falls back via `clampSel` if it was hidden
  (FR5); `clampSel` already handles the zero-row case (FR8). Note the fallback
  is an *index clamp into the surviving row set*, not an ancestor-walk — when the
  selected node is pruned the selection lands on a valid in-range row, not
  necessarily the nearest ancestor. This matches existing `r`/sort behaviour.
- **Trade-offs**: None — it is the established idiom.

### Test-phase notes (carried forward from design review)
- **FR5 assertion shape**: assert the post-toggle selection is in-bounds (a valid
  row index) when the selected node was pruned — do not pin it to a specific row.
- **Highest-value case**: name an explicit test for a directory whose *only*
  descendant change is a deletion staying visible with hide mode on (guards the
  predicate against regressing to `Stats.Files`-based logic).
- **Footer/binding drift**: the footer help is a hand-maintained literal accreting
  per-key edits; the testing plan should assert the footer advertises `z`
  (FR7/AC7) so a future binding change that forgets the footer is caught.

### Decision 5 — Footer help advertises `z`
- **Decision**: Extend the footer string in `drawFooter` to name `z` and its
  effect (e.g. append `  z hide/show unchanged`).
- **Rationale**: Discoverability (FR7/NFR2). The footer already lists every
  binding; `z` joins the list.
- **Trade-offs**: Footer length grows; at narrow widths `drawText` already clips
  on rune boundaries, so no overflow risk.

### Technology Stack
- N/A — no new dependencies. Uses the existing `github.com/gdamore/tcell/v2`
  stack and the exported `dcfh.Node`/`dcfh.Stats` types already consumed.

## System Design
### Component Overview
- **`model.hideUnchanged` (new field, `render.go`)**: persistent toggle state.
- **`hasChange` (new pure func, `render.go`)**: the unchanged/changed predicate.
- **`rebuildRows.walk` (modified, `render.go`)**: the single filter point.
- **`handleRune` (modified, `tui.go`)**: binds `z` to the toggle.
- **`drawFooter` (modified, `render.go`)**: advertises the binding.

### Data Flow
1. User presses `z` → `handleKey` → `handleRune('z')`.
2. `handleRune` captures `current()`, flips `m.hideUnchanged`, calls
   `rebuildRows()`, then `selectNode(cur)`.
3. `rebuildRows.walk` consults `m.hideUnchanged` + `hasChange(c)` per child,
   pruning unchanged nodes (and their subtrees) before append/recurse.
4. `draw` renders the filtered `m.rows`; an empty result hits the existing
   `(no changes to display)` body path; header/stats read root/selection
   independently (FR8).

## Interface Design
### API Endpoints
- N/A — local TUI; no network/API surface.

### Data Models
- No new types. One new bool field on the unexported `model`; one new free
  function. The exported `dcfh.Tree`/`Node`/`Stats` contract is unchanged
  (read-only consumption, no mutation).

## Constraints
- Read-only viewer: no index/filesystem mutation (package contract, NFR4).
- Pure over already-aggregated `Stats`: no I/O, no extra traversal (NFR1).
- `z` confirmed unbound in `handleRune`/`keyForRune`; binding it cannot shadow
  an existing key.
- Glyph/label sanitisation contract unchanged — the filter adds/removes existing
  rows and renders no new node-derived strings (NFR4).

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one field, one predicate, one
      filter site, one binding, one footer line.
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Can parts be worked on separately? No.

**Result**: 0 signals — no decomposition.

## Validation
- [x] Design review completed (4-agent map/reduce, this phase)
- [x] Architecture consistent with existing toggle/sort design
- [x] Integration points verified against `render.go`/`tui.go` source

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 5 decisions implemented verbatim: `hideUnchanged` field, in-`walk` filter at
the single `rebuildRows` choke point, pure `hasChange` predicate, `z` case
mirroring `r`, footer hint. No design revision needed during exec. See
f-implementation-exec.md.

## Lessons Learned
Subtree-aggregated `Stats` made directory visibility a derived consequence (no
ancestor walk, no force-expand) — the key simplification. See j-retrospective.md.
