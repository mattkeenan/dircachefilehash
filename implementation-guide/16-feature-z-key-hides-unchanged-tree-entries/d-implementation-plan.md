# z key hides unchanged tree entries - Implementation Plan
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Implement the `z` hide-unchanged toggle per the approved design: one model
field, one pure predicate, one in-`walk` filter, one key binding, one footer line.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes
- `cmd/dcfh/internal/tui/render.go` — add `hideUnchanged bool` to `model`; add
  the `hasChange` predicate; filter inside `rebuildRows.walk`; add `z` to the
  footer help string.
- `cmd/dcfh/internal/tui/tui.go` — add `case 'z':` to `handleRune` (toggle +
  rebuild + preserve selection), mirroring the `r` case.

### Supporting Changes
- `cmd/dcfh/internal/tui/render_test.go` — new tests + two small fixtures
  (`treeWithUnchangedDir`, `treeAllUnchanged`); see e-testing-plan.md.

## Implementation Steps

### Step 1: `hasChange` predicate (Decision 3, FR2)
- [ ] Add to `render.go` next to `nodeStyle`:
  ```go
  // hasChange reports whether n carries any change in its (subtree-aggregated)
  // Stats. Keys on Added+Modified+Deleted — never Stats.Files, which excludes
  // deletions, so a deletion-only node correctly counts as changed. This is the
  // exact complement of nodeStyle's unchanged arm (set == 0 → ' ').
  func hasChange(n *dcfh.Node) bool {
      return n.Stats.Added+n.Stats.Modified+n.Stats.Deleted > 0
  }
  ```

### Step 2: Model field (Decision 1, FR1)
- [ ] Add `hideUnchanged bool` to the `model` struct (alongside `reverse`).
  Zero value false → launch unchanged. No change to `newModel` needed.

### Step 3: Filter in `rebuildRows.walk` (Decision 2, FR3/FR4)
- [ ] **Before**:
  ```go
  walk = func(n *dcfh.Node, depth int) {
      for _, c := range sortNodes(n.Children, m.sortKey, m.reverse) {
          m.rows = append(m.rows, rowItem{node: c, depth: depth})
          if c.IsDir && m.expanded[c] {
              walk(c, depth+1)
          }
      }
  }
  ```
- [ ] **After**:
  ```go
  walk = func(n *dcfh.Node, depth int) {
      for _, c := range sortNodes(n.Children, m.sortKey, m.reverse) {
          if m.hideUnchanged && !hasChange(c) {
              continue // prune unchanged node + its whole subtree; no force-expand
          }
          m.rows = append(m.rows, rowItem{node: c, depth: depth})
          if c.IsDir && m.expanded[c] {
              walk(c, depth+1)
          }
      }
  }
  ```
  Filtering *before* append/recurse prunes a wholly-unchanged directory's subtree
  in one step; a directory with any changed descendant has `hasChange` true (Stats
  is subtree-aggregated) and stays visible (FR4/AC4b).

### Step 4: Bind `z` in `handleRune` (Decision 4, FR1/FR5)
- [ ] Add a `case 'z':` mirroring the existing `r` case:
  ```go
  case 'z':
      cur := m.current()
      m.hideUnchanged = !m.hideUnchanged
      m.rebuildRows()
      m.selectNode(cur)
  ```

### Step 5: Footer help advertises `z` (Decision 5, FR7)
- [ ] **Before**: `help := "↑/↓ move  →/← expand/collapse  c/f/a/m/d/n sort  r reverse  q quit"`
- [ ] **After**: `help := "↑/↓ move  →/← expand/collapse  c/f/a/m/d/n sort  r reverse  z hide  q quit"`
  (`drawText` clips on rune boundaries, so the longer line degrades safely at
  narrow widths.)

### Step 6: Tests (per e-testing-plan.md)
- [ ] Add `TestHasChange` (pure predicate table) and simulation tests for the
  toggle (leaf hide, deletion-only/collapsed dir stays, wholly-unchanged dir
  hidden, composition with sort/reverse, footer advertises `z`, all-hidden
  empty-state, selection preserved/clamped). Two new fixtures.
- [ ] **Robustness notes (from plan review):** the selection test must assert
      the *clamped* branch (select an unchanged node, press `z`, assert
      `m.sel` is in-bounds) — not only the preserved branch. The all-hidden test
      must also press a navigation key (`moveDown`/`expand`) while `m.rows` is
      empty to lock the no-panic contract (`current()==nil` bails are guarded
      today; the test pins that).

### Step 7: Build & regression
- [ ] `make build`; `go test ./cmd/dcfh/internal/tui/...` then `./cmd/... ./pkg/...`.
- [ ] `golangci-lint run ./cmd/dcfh/internal/tui/...` → 0 issues.
- [ ] Footer-string check **resolved at plan time**: grep of
      `render_test.go`/`sort_test.go` for the footer literal found no assertions
      pinning it (suite asserts only the header sort string), so appending
      ` z hide` breaks nothing. Re-confirm the grep stays empty at exec.

## Test Coverage
**See e-testing-plan.md for complete test plan.** Targets: `hasChange` 100%;
the new `rebuildRows` filter branch and the `z` `handleRune` case exercised by
simulation tests; full `tui` regression green.

## Validation Criteria
**See e-testing-plan.md.** Build clean, all ACs (AC1–AC8 + AC4b) covered, full
`go test ./...` + pre-commit `-race` green, golangci-lint clean, no existing test
disabled.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 7 steps executed as written; before/after code matched the source exactly.
Only deviation: added small `simModelFor`/`pressZ` test helpers (Step 6). Footer
grep re-confirmed empty at exec. Build/lint/race/vuln gates all green. See
f-implementation-exec.md.

## Lessons Learned
Pre-resolving the footer-string-pin risk at plan time (grep for assertions) meant
the footer edit broke nothing — worth doing for any hand-maintained literal.
