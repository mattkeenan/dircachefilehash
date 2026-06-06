# Byte-weighted default sort for interactive-tree - Plan
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Baseline Commit**: 83f7440e27d72b70daa3584f13c51052c0ce50e8
- **Template Version**: 2.1

## Goal
Make the interactive-tree viewer sort by *volume of change* (bytes) by
default instead of *count of change* (files), so the busiest directories
by data surface first — and keep the count sort available on its own key.

## Success Criteria
- [ ] A new `change_bytes` sort metric (Added+Modified+Deleted **bytes**)
      exists and is the **default**; header reads `sort:change_bytes(desc)`.
- [ ] The existing count metric is renamed `change` → `change_files`
      everywhere it surfaces (sortKey, `label()`, header string, help text).
- [ ] Runtime key map: `c` → `change_bytes` (default), `f` → `change_files`;
      `a`/`m`/`d`/`n`/`r` unchanged.
- [ ] Byte sums are sourced without a second filesystem walk (KD2 single
      source preserved) and `status`/`update` non-interactive output and
      on-disk index bytes remain byte-identical (no regression).
- [ ] `go build ./...`, `go test ./...`, `golangci-lint run ./...` (gosec)
      and the `-race` pipeline tests stay green.

## Original Estimate
**Effort**: 0.5–1 day
**Complexity**: Medium
**Dependencies**: None external. Implements the existing BACKLOG item
"Byte-weighted sort option for interactive-tree viewer" (Low, identified
in task 11) — to be retired into CHANGELOG under task 12 at retrospective.

## Major Milestones
1. **Data layer**: per-category byte sums available on `dcfh.Stats`
   (`AddedBytes`/`ModifiedBytes`/`DeletedBytes`), aggregated up the tree,
   fed from the merged index (live) and the change-set (deleted).
2. **Sort layer**: add `sortChangeBytes`, rename `sortChange` →
   `sortChangeFiles`, set the new default, rebind keys (`c`/`f`).
3. **Verification**: unit tests for the new metric + comparators, render
   header/default assertions, and the non-interactive byte-identity
   regression; manual real-terminal confirmation that `c`/`f` toggle.

## Risk Assessment
### High Priority Risks
- **Deleted-byte attribution asymmetry**: `status` retains deleted sizes
  (in the cache/merged index and on `StatusResult.DeletedBytes`), but a
  full `update` discards deleted entries from the merged index, so their
  bytes are only knowable if the `changeCollector` captures them at
  `OnLeftOnly`/`OnMatch` (the backlog-noted plumbing). If the two paths
  attribute deleted bytes differently, the same deletion sorts
  differently under `status` vs `update`.
  - **Mitigation**: settle one rule in design (c-) that both paths honour;
    the leading candidate is to thread per-deleted-path sizes through
    `ChangeSet` uniformly so the pure builder has one code path. Decision
    is flagged as the #1 review item before exec.

### Medium Priority Risks
- **Modified-byte semantics**: "current size" vs "old size" vs "delta".
  - **Mitigation**: match the existing `diffComparisonSink` convention
    (current/new size, `ModifiedBytes += right size`); reject deltas as
    out-of-scope plumbing.
- **Key-binding collision / muscle memory**: `c` changes meaning (was
  count, now bytes).
  - **Mitigation**: documented in help text and the footer legend; `f`
    added for the old behaviour; called out in CHANGELOG.

## Dependencies
- Builds directly on task 11's data/sort/render layers (`pkg/treeview.go`,
  `cmd/dcfh/internal/tui/sort.go`, `render.go`).
- No new third-party dependencies.

## Constraints
- **No second filesystem walk** (KD2): byte sums must come from the
  already-loaded merged index plus the in-memory change-set.
- **Byte-identical non-interactive path**: the collector/size capture must
  not perturb serialisation or the atomic rename (task 11's TC-17 guard
  must still hold).
- British spelling in prose; per-line gosec rationale if any new suppress.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (~0.5–1 day).
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? Borderline (data + sort +
      cross-path consistency) but one cohesive change in one subsystem.
- [ ] **Risk**: High-risk components needing isolation? No — the one risk
      (deleted-byte attribution) is a design decision, not isolatable code.
- [ ] **Independence**: Can parts be worked separately? Not usefully — the
      rename, new metric and default flip are one atomic user-facing change
      (the structure decided with the user).
**Conclusion**: No decomposition. Single feature task.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered on estimate (one session). All five success criteria met:
`change_bytes` default + header, `change`→`change_files` rename, `c`/`f`
key map, no second walk + byte-identical non-interactive output, and
build/test/lint/-race green.

## Lessons Learned
The #1 flagged risk (deleted-byte attribution asymmetry) was resolved in
design (KD2 dual-source), so exec had zero deviations — front-loading the
hard decision paid off. See j-retrospective.md.
