# size column shows disk size not change bytes - Plan
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Baseline Commit**: 597bb5f58af4985dd4f4ee7706184eca26f73afc
- **Template Version**: 2.1

## Goal
Make the interactive-tree viewer's right-aligned per-row number reflect the
active sort metric (change volume by default) instead of always showing the
subtree's on-disk footprint, so the byte-weighted `change_bytes` ordering is
self-explaining.

## Background
Task 12 added `change_bytes` (Added+Modified+Deleted bytes) as the default
sort, but `cmd/dcfh/internal/tui/render.go:201` still draws
`row.node.Stats.Bytes` — the live on-disk size — for every row. The ordering
is therefore correct (the comparator uses the proper metric via
`sort.go:metric`) but the visible number contradicts it: a directory with
~112 MB of churn renders its 51.9 GB disk size, so the descending order looks
wrong. The data already exists on `Stats` (AddedBytes/ModifiedBytes/
DeletedBytes); no new collection or on-disk format change is needed.

## Success Criteria
- [ ] Under the default `change_bytes` sort, each row's right-aligned value is
      the subtree's change volume (`AddedBytes+ModifiedBytes+DeletedBytes`),
      matching the descending order shown.
- [ ] The displayed value is consistent with whichever sort key is active per
      the rule decided in design (c-design-plan KD), with no row contradicting
      its position in the ordering.
- [ ] Render unit test asserts the column value equals the active-sort metric
      (not `Stats.Bytes`) for at least the `change_bytes` default and one
      count-based key.
- [ ] No change to non-interactive status/update output, the on-disk index, or
      the collected `Stats` fields — render-layer-only fix.
- [ ] `go test ./...` green; pre-commit gate (golangci-lint, race) clean.

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None (data already on `dcfh.Stats`; task 12 landed)

## Major Milestones
1. **Design decision**: Settle what the column shows per sort key — change
   bytes always, vs. track-the-active-metric (heterogeneous bytes/counts),
   vs. bytes for change sorts + disk size for `name`. Define the header/column
   semantics so units are never ambiguous.
2. **Render fix**: Replace the hardcoded `Stats.Bytes` read with the
   sort-aware value; reuse `sort.go:metric` rather than duplicating the
   category sums.
3. **Tests**: Update/extend render tests to lock the column to the metric.

## Risk Assessment
### Medium Priority Risks
- **Risk 1**: Heterogeneous column units (bytes for byte-sorts, integer counts
  for count-sorts) could confuse rather than clarify.
  - **Mitigation**: Resolve explicitly in design (KD); whichever rule is
    chosen, ensure the header sort indicator already names the active metric so
    the unit is discoverable. Prefer the simplest rule that satisfies the
    user's stated intent ("see the size of the changes").

### Low Priority Risks
- **Risk 2**: A render test currently asserts on `Stats.Bytes` and would need
  updating, risking a masked regression.
  - **Mitigation**: Read existing render_test.go assertions first; update them
    deliberately, not reflexively.

## Dependencies
- None external. Self-contained render-layer change atop task 12.

## Constraints
- Render-layer only: no new data collection, no `Stats`/`ChangeSet` field
  changes, no on-disk format impact, no change to non-interactive output.
- Reuse `sort.go:metric`; do not duplicate the category-sum logic in render.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (<0.5 day).
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No (single render-layer concern).
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Separable parts? No.

0 signals triggered — single focused bugfix, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan 13
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All success criteria met. The column now renders the active-sort metric;
`columnText` is 100% covered; non-interactive output and on-disk index
unchanged; full suite + race green; golangci-lint 0 issues. Completed in a
single session, within the <0.5 day estimate. 0 decomposition signals held.

## Lessons Learned
The decisive move was settling the column-display rule (AskUserQuestion) before
design, and letting the plan-review surface the `name`→0 trap up front — exec
then had zero deviations on the core change. See j-retrospective.md.
