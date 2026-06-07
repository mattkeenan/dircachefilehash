# size column shows disk size not change bytes - Implementation Plan
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1

## Goal
Make `drawRow` render the active sort metric (via a new `columnText` helper)
instead of the hardcoded `Stats.Bytes`, per c-design-plan KD1/KD2.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes
- `cmd/dcfh/internal/tui/sort.go` — add `columnText(n *dcfh.Node, key sortKey)
  string`, co-located with `metric()`. Imports `strconv` (new) and the `dcfh`
  alias (already imported) for `FormatHumanSize`.
- `cmd/dcfh/internal/tui/render.go` — in `drawRow`, replace
  `dcfh.FormatHumanSize(row.node.Stats.Bytes)` with
  `columnText(row.node, m.sortKey)`; rename locals `size`→`colVal`,
  `sizeX`→`colX`.

### Supporting Changes (both files already exist — extend, do not add)
- `cmd/dcfh/internal/tui/sort_test.go` — add unit tests for `columnText` (the
  behaviour table from the design, incl. the `name`→change_bytes fallback
  returning a byte string not `0 B`). The existing `node()`/`nodeBytes()`
  helpers set counts XOR bytes; the mixed count+byte fixture needs a literal
  `dcfh.Node{Stats: dcfh.Stats{…}}` (as `treeForSim` does).
- `cmd/dcfh/internal/tui/render_test.go` — add a render test asserting the
  drawn column equals the active-sort metric for the `change_bytes` default
  and one count key (proves the wire-up, not just the helper).

## Implementation Steps

### Step 1: Helper (`sort.go`)
- [ ] Add `import "strconv"` to the existing import block.
- [ ] Add `columnText`, reusing `metric()` (do NOT re-sum categories):
  ```go
  // columnText formats the right-aligned per-row value for the active
  // sort key, reusing metric() so the displayed number can never diverge
  // from the ordering. The change_bytes value formats as a human size;
  // count keys format as a decimal integer. name has no numeric key, so
  // it falls back to the change_bytes value (change volume).
  //
  // Precondition: n is non-nil (rebuildRows never enqueues a nil node).
  func columnText(n *dcfh.Node, key sortKey) string {
      if key == sortName {
          key = sortChangeBytes // name → change volume (metric(name) is 0)
      }
      v := metric(n, key)
      if key == sortChangeBytes {
          return dcfh.FormatHumanSize(v)
      }
      return strconv.FormatInt(v, 10)
  }
  ```
  Note the ordered requirement (design F1): remap `name`→`change_bytes`
  **before** calling `metric()`; never call `metric(n, sortName)` (returns 0).

### Step 2: Wire-up (`render.go:drawRow`, lines ~200-205)
- [ ] Replace:
  ```go
  size := dcfh.FormatHumanSize(row.node.Stats.Bytes)
  sizeX := width - len(size)
  if sizeX > x+1 {
      drawText(s, sizeX, y, width-sizeX, base, size)
  }
  ```
  with:
  ```go
  colVal := columnText(row.node, m.sortKey)
  colX := width - len(colVal)
  if colX > x+1 {
      drawText(s, colX, y, width-colX, base, colVal)
  }
  ```
- [ ] Update the `// Right-aligned size …` comment to say the column shows the
  active-sort metric (size for change_bytes/name, count otherwise).

### Step 3: Tests
- [ ] `sort_test.go`: table test over every `sortKey`. Build a node with known
  Stats (e.g. Added 1/50B, Modified 1/200B, Deleted 1/900B → change_bytes
  1150B, change_files 3, added 1, …). Assert:
  - `change_bytes` → `dcfh.FormatHumanSize(1150)`
  - `name` → `dcfh.FormatHumanSize(1150)` (NOT `"0 B"`) — locks F1
  - `change_files` → `"3"`, `added` → `"1"`, `modified` → `"1"`,
    `deleted` → `"1"`
- [ ] `render_test.go`: extend the existing sim-screen test (reuse
  `treeForSim`/`screenText`/`findRow`). Under the default `change_bytes` sort,
  assert the rendered row for `docs` contains `FormatHumanSize(900)` and NOT
  its `Stats.Bytes` (0 for docs — pick `src`: change_bytes=250 vs Bytes=250
  collide, so use a node where they differ). After pressing `f` (change_files),
  assert the same row shows the count. (See note below on choosing a
  discriminating node.)

### Step 4: Validate
- [ ] `go build ./...`
- [ ] `go test ./cmd/... ./pkg/...`
- [ ] `golangci-lint run ./...` (gosec floor; new `strconv` use is benign)
- [ ] Manual: `./dcfh status --interactive-tree` on a dir with churn; confirm a
  directory's number is its change volume, and toggling `f`/`a`/`m`/`d` swaps
  the column to counts matching the header.

## Code Changes
### Before (`render.go:201-205`)
```go
// Right-aligned size for files / aggregate size for dirs.
size := dcfh.FormatHumanSize(row.node.Stats.Bytes)
sizeX := width - len(size)
if sizeX > x+1 {
    drawText(s, sizeX, y, width-sizeX, base, size)
}
```
### After
```go
// Right-aligned value tracks the active sort metric: change volume
// (human size) for change_bytes/name, else the integer count.
colVal := columnText(row.node, m.sortKey)
colX := width - len(colVal)
if colX > x+1 {
    drawText(s, colX, y, width-colX, base, colVal)
}
```

## Test-fixture note (discriminating node)
`treeForSim` currently has `src` with change_bytes 250 == Stats.Bytes 250, so a
test there can't distinguish the fix from the bug. Assert on a node where the
two differ: `docs/old.md` is deleted → `Stats.Bytes` 0 but change_bytes 900.
Under the old code the column showed `0 B`; under the fix it shows
`FormatHumanSize(900)`. That row is the regression guard.

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
Single render-layer change + tests; no deferral expected.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 13
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Both planned edits landed verbatim (helper in `sort.go`, wire-up in
`drawRow`). Two incidental test-file cleanups added (`strings.SplitSeq`;
dropped the dead `newSimModel` `h` param to clear a latent `unparam`).
Discriminating-node strategy worked: the `docs/` regression guard passes.

## Lessons Learned
The plan's explicit "do not re-sum categories; reuse `metric()`" instruction
and the discriminating-node note made implementation mechanical.
