# z key hides unchanged tree entries - Plan
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Baseline Commit**: 826fab4a7aed6914cdcf080b3fd6a9573f6cbb3e
- **Template Version**: 2.1

## Goal
Add a `z` key binding to the `--interactive-tree` viewer that toggles hiding
of unchanged entries (files and wholly-unchanged directories), so the user can
collapse the tree to only what changed.

## Success Criteria
- [ ] `z` toggles a "hide unchanged" mode on/off; the footer help line advertises it.
- [ ] When the mode is on, every visible row is a changed node — a node with
      `Added+Modified+Deleted > 0` (keyed on that sum, never on `Stats.Files`,
      which excludes deletions); directories whose whole subtree is unchanged are
      hidden, directories with any descendant change stay visible.
- [ ] Selection focus is preserved across the toggle (same node stays selected if
      still visible; graceful fallback if it was hidden) — matching the `r`/sort
      toggle behaviour.
- [ ] The mode composes with existing sort (`c/f/a/m/d/n`), reverse (`r`), and
      expand/collapse without regression; the full `tui` suite stays green.
- [ ] When the mode hides everything (no changes), the existing empty-state
      ("(no changes to display)") renders with no panic.

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None — confined to the existing `cmd/dcfh/internal/tui` package.

## Major Milestones
1. **Requirements + design**: pin the "unchanged" predicate and the
   directory-visibility rule; confirm `z` is unbound.
2. **Implementation**: add a `hideUnchanged` model flag, filter in `rebuildRows`,
   bind `z` in `handleRune`, update the footer help string.
3. **Tests + close-out**: table/simulation coverage for the toggle, empty-state,
   and composition with sort/reverse; full regression green.

## Risk Assessment
### High Priority Risks
- **Risk 1**: Keying "changed?" off `Stats.Files` (which excludes deletions)
  would wrongly hide deletion-only directories — the exact trap task 15 hit.
  - **Mitigation**: Predicate is `Added+Modified+Deleted > 0`; covered by a
    deletion-only fixture in the test plan (reuse task 15's `docs/` case).

### Medium Priority Risks
- **Risk 2**: A directory could be hidden even though a descendant changed (or
  shown though empty) if the filter inspects only direct children.
  - **Mitigation**: Filter on each node's already-aggregated subtree `Stats`, so
    subtree changes propagate up the existing aggregation — no new tree pass.

## Dependencies
- Builds on task 15's viewer (`render.go`, `tui.go`); no external dependencies.

## Constraints
- Read-only viewer: must not mutate the index or filesystem (package contract).
- Pure render-layer operation over already-aggregated `dcfh.Stats` — no re-read,
  no new data, no I/O (consistent with the sort/reverse toggles).
- `z` must remain unbound elsewhere; verify against `handleRune` before binding.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — single small change.
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one toggle + filter + help line.
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Can parts be worked on separately? No — one cohesive change.

**Result**: 0 signals — no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 5 success criteria met. Delivered on estimate (<0.5 day, Low) with no scope
change and no rework. Both identified risks (Risk 1 `Stats.Files` deletion trap;
Risk 2 directory-by-subtree) were designed against from the first line and
guarded by TC-U1/TC-3 and TC-4. See j-retrospective.md.

## Lessons Learned
Carrying task 15's `Stats.Files` deletion trap forward as an explicit design
constraint pre-empted the only real correctness risk. See j-retrospective.md.
