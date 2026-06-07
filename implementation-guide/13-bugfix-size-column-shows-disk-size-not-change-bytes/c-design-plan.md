# size column shows disk size not change bytes - Design
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1

## Goal
Make the interactive-tree viewer's right-aligned per-row value track the active
sort metric, so the number beside each row always explains its position in the
ordering — fixing the case where `change_bytes(desc)` shows on-disk size.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Problem Statement
`cmd/dcfh/internal/tui/render.go:201` hardcodes the column to
`row.node.Stats.Bytes` (live on-disk footprint). The comparator in
`sort.go:metric` already ranks by the correct per-key metric, so ordering is
right but the displayed number contradicts it. The fix is render-layer only:
the number drawn must come from the same metric the active sort uses.

## Key Decisions

### KD1 — Column tracks the active sort metric (confirmed scope)
- **Decision**: The right-aligned per-row value is the value of the *active
  sort key* for that node, formatted by unit:
  - `change_bytes` → `FormatHumanSize(AddedBytes+ModifiedBytes+DeletedBytes)`
  - `change_files` / `added` / `modified` / `deleted` → decimal integer count
  - `name` → falls back to `change_bytes` (name has no numeric key, so the
    column shows change volume — the default, useful metric)
- **Rationale**: User-confirmed (review Q, 2026-06-07: "Track active sort").
  The number always matches the column you're sorting by, which is the whole
  point of the byte-weighted sort. The header sort indicator
  (`sort:<label>(<dir>)`) already names the active metric, so the column's
  unit (bytes vs count) is never ambiguous despite being heterogeneous.
- **Trade-offs**: The column unit changes when the user toggles sort key
  (e.g. `112.3 MB` under `change_bytes` → `69` under `added`). Accepted: the
  header disambiguates, and the stats pane still shows the full count+byte
  breakdown for the selected node. The alternative ("always change bytes")
  was rejected because count sorts would then show a number unrelated to the
  rank order.

### KD2 — Single metric→display mapping, co-located with the comparator
- **Decision**: Add one helper in `sort.go` (package `tui`) that maps
  `(*dcfh.Node, sortKey)` to the formatted column string, reusing the existing
  `metric()` function for the numeric value. `render.go` calls it instead of
  reading `Stats.Bytes`.
- **Rationale**: `sort.go:metric` is already the single source of the per-key
  value. Deriving the displayed number from the same function guarantees the
  column and the ordering can never diverge (the bug being fixed is exactly
  such a divergence). Co-location keeps the sort-key knowledge in one file
  (KD8/FR10 from task 12: sort is a pure render-layer concern).
- **Trade-offs**: One small helper added to `sort.go`. No new types.

### KD3 — No data-model or collection changes
- **Decision**: `Stats`, `ChangeSet`, the tree builder, and all non-interactive
  output are untouched. `Stats.Bytes` is retained (the stats pane still shows
  it as the live "Size:" line).
- **Rationale**: The required values (AddedBytes/ModifiedBytes/DeletedBytes and
  the counts) already aggregate onto every node from task 12. This is a display
  bug, not a data gap.

## System Design
### Component Overview
- **`sort.go` (new helper)**: `columnText(n *dcfh.Node, key sortKey) string` —
  the metric→display mapping. Reuses `metric()`; formats bytes via
  `dcfh.FormatHumanSize`, counts via decimal. `name` is mapped to
  `change_bytes` before formatting.
- **`render.go:drawRow`**: replaces `dcfh.FormatHumanSize(row.node.Stats.Bytes)`
  with `columnText(row.node, m.sortKey)`. `m.sortKey` is already in scope
  (`drawRow` is a `*model` method). Rename the locals `size` → `colVal` and
  `sizeX` → `colX` since the value is no longer always a byte size.
- **Unchanged**: stats pane (`drawStats`) still prints `Size:` from
  `Stats.Bytes` and the per-category count+byte lines.

### Data Flow
1. User toggles sort key → `m.sortKey` updated, `m.rebuildRows()`.
2. `draw` → `drawTree` → `drawRow(row, …)` for each visible node.
3. `drawRow` calls `columnText(row.node, m.sortKey)`.
4. `columnText` calls `metric(node, key)` (the same value the comparator
   ranked on) and formats it per unit.

## Interface Design
### New helper (package `tui`, in `sort.go`)
```
// columnText formats the right-aligned per-row value for the active sort
// key, reusing metric() so the number can never diverge from the order.
// Bytes keys format via FormatHumanSize; count keys as a decimal integer;
// name (no numeric key) falls back to change-volume bytes.
//
// Precondition: n is non-nil (rebuildRows never enqueues a nil node).
func columnText(n *dcfh.Node, key sortKey) string
```

**Ordered requirement (review F1 — do not collapse these two steps):**
`metric(n, sortName)` returns `0` (`sort.go:59-60`), so `columnText` MUST
remap the key *before* reading the value:
1. If `key == sortName`, set `key = sortChangeBytes` (the displayed change
   volume). Do **not** call `metric(n, sortName)`.
2. Take the value from `metric(n, key)` — the same call the comparator uses —
   never a hand-written `AddedBytes+ModifiedBytes+DeletedBytes` re-sum (that
   would reintroduce the column/order divergence KD2 exists to prevent).
3. Format: `sortChangeBytes` → `FormatHumanSize`; every other key → decimal.

Behaviour table (also the unit-test matrix — one assertion per row, plus the
`name` row asserting a non-`0 B` byte string):

| key            | value source                         | format            |
|----------------|--------------------------------------|-------------------|
| change_bytes   | metric() = A+M+D bytes               | FormatHumanSize   |
| name           | A+M+D bytes (mapped to change_bytes) | FormatHumanSize   |
| change_files   | metric() = A+M+D count               | decimal integer   |
| added          | metric() = Added count               | decimal integer   |
| modified       | metric() = Modified count            | decimal integer   |
| deleted        | metric() = Deleted count             | decimal integer   |

### Data Models
Unchanged. `dcfh.Stats` fields already carry every value the helper reads.

## Constraints
- Render-layer only; no change to `Stats`, `ChangeSet`, the builder, the
  on-disk index, or non-interactive status/update output.
- Reuse `sort.go:metric`; do not re-sum categories in `render.go`.
- Right-alignment uses `colX = width - len(colVal)`. `len()` is byte length,
  not display width — safe here only because the column is always ASCII
  (`FormatHumanSize` output or decimal digits), so byte length == rune count
  == column width. Do not feed a non-ASCII string into this column.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: >1 week? No (<0.5 day).
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No (one render-layer concern).
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Separable parts? No.

0 signals — no decomposition.

## Validation
- [ ] Design review completed (plan-review subagents)
- [ ] Column-rule decision (KD1) recorded and matches user choice
- [ ] Integration point verified: `m.sortKey` in scope at `drawRow`; `metric()`
      reusable from `render.go` (same package)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 13
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
KD1 (track active sort) and KD2 (single metric→display mapping reusing
`metric()`) implemented exactly as designed. The F1 ordered requirement
(remap `name`→`change_bytes` before calling `metric()`) was followed and
locked by a test. No design gaps surfaced during exec.

## Lessons Learned
Reusing the ranking function for the displayed value made column/order
divergence unrepresentable — the right structural answer to a "display
contradicts sort" bug.
